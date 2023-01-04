// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.9;

import "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts-upgradeable/security/ReentrancyGuardUpgradeable.sol";
import "@openzeppelin/contracts-upgradeable/utils/math/SafeMathUpgradeable.sol";
import "@openzeppelin/contracts-upgradeable/utils/AddressUpgradeable.sol";
import { DataLayrDisclosureLogic } from "../libraries/eigenda/DataLayrDisclosureLogic.sol";
import { IDataLayrServiceManager } from "../libraries/eigenda/lib/contracts/interfaces/IDataLayrServiceManager.sol";
import { BN254 } from "../libraries/eigenda/BN254.sol";
import { DataStoreUtils } from "../libraries/eigenda/lib/contracts/libraries/DataStoreUtils.sol";
import { Parser } from "../libraries/eigenda/Parse.sol";
import "hardhat/console.sol";


contract BVM_EigenDataLayrChain is OwnableUpgradeable, ReentrancyGuardUpgradeable, Parser {
    using SafeMathUpgradeable for uint256;
    using AddressUpgradeable for address;

    enum RollupStoreStatus {
        UNCOMMITTED,
        COMMITTED,
        REVERTED
    }

    struct DisclosureProofs {
        bytes header;
        uint32 firstChunkNumber;
        bytes[] polys;
        DataLayrDisclosureLogic.MultiRevealProof[] multiRevealProofs;
        BN254.G2Point polyEquivalenceProof;
    }

    address public sequencer;
    address public dataManageAddress;
    uint256 public BLOCK_STALE_MEASURE;
    uint256 public l2SubmittedBlockNumber;
    uint256 public fraudProofPeriod;

    bytes public constant FRAUD_STRING = '-_(` O `)_- -_(` o `)_- -_(` Q `)_- BITDAO JUST REKT YOU |_(` O `)_| - |_(` o `)_| - |_(` Q `)_|';

    struct RollupStore {
        uint32 dataStoreId;
        uint32 confirmAt;
        RollupStoreStatus status;
    }

    //mapping from the l2block's block number to datastore id
    mapping(uint256 => RollupStore) public l2BlockRollupStores;
    /**
     * @notice mapping used to track whether or not this contract initiated specific dataStores, as well as
     * to track how the link between dataStoreId and L2BlockNumber
     * @dev We use this so we don't create a subgraph temporarily
     */
    mapping(uint32 => uint256) public dataStoreIdToL2BlockNumber;

    //da fraud proof white list
    mapping(address=>bool) private fraudProofWhitelist;

    event RollupStoreInitialized(uint32 dataStoreId, uint256 l2BlockNumber);
    event RollupStoreConfirmed(uint32 dataStoreId, uint256 l2BlockNumber);
    event RollupStoreReverted(uint32 dataStoreId, uint256 l2BlockNumber);

    function initialize(address _sequencer, address _dataManageAddress, uint256 _block_stale_measure, uint256 _fraudProofPeriod, uint256 _l2SubmittedBlockNumber) public initializer {
        __Ownable_init();
        sequencer = _sequencer;
        dataManageAddress = _dataManageAddress;
        BLOCK_STALE_MEASURE = _block_stale_measure;
        fraudProofPeriod = _fraudProofPeriod;
        l2SubmittedBlockNumber = _l2SubmittedBlockNumber;
    }

    /**
     * @notice Returns the block number of the latest submitted L2.
     * If no submitted yet then this function will return the starting block number.
     *
     * @return Latest submitted L2 block number.
     */
    function getL2SubmitBlockNumber() public view returns (uint256) {
        return l2SubmittedBlockNumber;
    }

    /**
     * @notice Returns the rollup store by l2 block number
     * @return RollupStore.
     */
    function rollupStoreByL2Block(uint256 l2Block) public view returns (RollupStore memory) {
        return l2BlockRollupStores[l2Block];
    }

    /**
    * @notice set fraud proof address
    * @param _address for fraud proof
    */
    function setFraudProofAddress(address _address) external {
        require(msg.sender == sequencer, "Only the sequencer can set fraud proof address unavailable");
        fraudProofWhitelist[_address] = true;
    }

    /**
    * @notice unavailable fraud proof address
    * @param _address for fraud proof
    */
    function unavailableFraudProofAddress(address _address) external {
        require(msg.sender == sequencer, "Only the sequencer can remove fraud proof address");
        fraudProofWhitelist[_address] = false;
    }

    /**
    * @notice remove fraud proof address
    * @param _address for fraud proof
    */
    function removeFraudProofAddress(address _address) external {
        require(msg.sender == sequencer, "Only the sequencer can remove fraud proof address");
        delete fraudProofWhitelist[_address];
    }


    /**
    * @notice update fraud proof period
    * @param _fraudProofPeriod fraud proof period
    */
    function updateFraudProofPeriod(uint256 _fraudProofPeriod) external {
        require(msg.sender == sequencer, "Only the sequencer can update fraud proof period");
        fraudProofPeriod = _fraudProofPeriod;
    }

    /**
    * @notice update dlsm address
    * @param _dataManageAddress dlsm address
    */
    function updateDataLayrManagerAddress(address _dataManageAddress) external {
        require(msg.sender == sequencer, "Only the sequencer can update dlsm address");
        dataManageAddress = _dataManageAddress;
    }

    /**
    * @notice update l2 latest block number
    * @param _l2SubmittedBlockNumber l2 latest block number
    */
    function updateSubmittedL2BlockNumber(uint256 _l2SubmittedBlockNumber) external {
        require(msg.sender == sequencer, "Only the sequencer can set latest l2 block number");
        l2SubmittedBlockNumber = _l2SubmittedBlockNumber;
    }

    /**
    * @notice update sequencer address
    * @param _sequencer update sequencer address
    */
    function updateSequencerAddress(address _sequencer) external {
        require(msg.sender == sequencer, "Only the sequencer can update sequencer address");
        sequencer = _sequencer;
    }

    /**
     * @notice Called by the (staked) sequencer to pay for a datastore and post some metadata (in the `header` parameter) about it on chain.
     * Since the sequencer must encode the data before they post the header on chain, they must use a *snapshot* of the number and stakes of DataLayr operators
     * from a previous block number, specified by the `blockNumber` input.
     * @param header of data to be stored
     * @param duration is the duration to store the datastore for
     * @param blockNumber is the previous block number which was used to encode the data for storage
     * @param totalOperatorsIndex is index in the totalOperators array of DataLayr referring to what the total number of operators was at `blockNumber`
     * @dev The specified `blockNumber `must be less than `BLOCK_STALE_MEASURE` blocks in the past.
     */
    function storeData(
        bytes calldata header,
        uint8 duration,
        uint32 blockNumber,
        uint256 l2Block,
        uint32 totalOperatorsIndex
    ) external {
        require(msg.sender == sequencer, "Only the sequencer can store data");
        require(block.number - blockNumber < BLOCK_STALE_MEASURE, "stakes taken from too long ago");
        uint32 dataStoreId = IDataLayrServiceManager(dataManageAddress).taskNumber();
        //Initialize and pay for the datastore
        IDataLayrServiceManager(dataManageAddress).initDataStore(
            msg.sender,
            address(this),
            duration,
            blockNumber,
            totalOperatorsIndex,
            header
        );
        dataStoreIdToL2BlockNumber[dataStoreId] = l2Block;
        emit RollupStoreInitialized(dataStoreId, l2Block);
    }

    /**
     * @notice After the `storeData `transaction is included in a block and doesn’t revert, the sequencer will disperse the data to the DataLayr nodes off chain
     * and get their signatures that they have stored the data. Now, the sequencer has to post the signature on chain and get it verified.
     * @param data Input of the header information for a dataStore and signatures for confirming the dataStore -- used as input to the `confirmDataStore` function
     * of the DataLayrServiceManager -- see the DataLayr docs for more info on this.
     * @param searchData Data used to specify the dataStore being confirmed. Must be provided so other contracts can properly look up the dataStore.
     * @dev Only dataStores created through this contract can be confirmed by calling this function.
     */
    function confirmData(
        bytes calldata data,
        IDataLayrServiceManager.DataStoreSearchData memory searchData,
        uint256 l2Block
    ) external {
        require(msg.sender == sequencer, "Only the sequencer can store data");
        require(
            dataStoreIdToL2BlockNumber[searchData.metadata.globalDataStoreId] == l2Block,
            "Data store either was not initialized by the rollup contract, or is already confirmed"
        );
        IDataLayrServiceManager(dataManageAddress).confirmDataStore(data, searchData);
        //store the rollups view of the datastore
        l2BlockRollupStores[l2Block] = RollupStore({
            dataStoreId: searchData.metadata.globalDataStoreId,
            confirmAt: uint32(block.timestamp + fraudProofPeriod),
            status: RollupStoreStatus.COMMITTED
        });
        //store link between dataStoreId and rollupStoreNumber
        emit RollupStoreConfirmed(searchData.metadata.globalDataStoreId, l2Block);
    }

    /**
  * @notice Called by a challenger (this could be anyone -- "challenger" is not a permissioned role) to prove that fraud has occurred.
     * First, a subset of data included in a dataStore that was initiated by the sequencer is proven, and then the presence of fraud in the data is checked.
     * For the sake of this example, "fraud occurring" means that the sequencer included the forbidden `FRAUD_STRING` in a dataStore that they initiated.
     * In pratical use, "fraud occurring" might mean including data that specifies an invalid transaction or invalid state transition.
     * @param l2Block The rollup l2Block to prove fraud on
     * @param startIndex The index to begin reading the proven data from
     * @param searchData Data used to specify the dataStore being fraud-proven. Must be provided so other contracts can properly look up the dataStore.
     * @param disclosureProofs Non-interactive polynomial proofs that prove that the specific data of interest was part of the dataStore in question.
     * @dev This function is only callable if:
     * -the sequencer is staked,
     * -the dataStore in question has been confirmed, and
     * -the fraudproof period for the dataStore has not yet passed.
     */
    function proveFraud(
        uint256 l2Block,
        uint256 startIndex,
        IDataLayrServiceManager.DataStoreSearchData memory searchData,
        DisclosureProofs calldata disclosureProofs
    ) external {
        require(fraudProofWhitelist[msg.sender] == true, "Only fraud proof white list can challenge data");
        RollupStore memory rollupStore = l2BlockRollupStores[l2Block];
        require(rollupStore.status == RollupStoreStatus.COMMITTED && rollupStore.confirmAt > block.timestamp, "RollupStore must be committed and unconfirmed");
        //verify that the provided metadata is correct for the challenged data store
        require(
            IDataLayrServiceManager(dataManageAddress).getDataStoreHashesForDurationAtTimestamp(
                searchData.duration,
                searchData.timestamp,
                searchData.index
            ) == DataStoreUtils.computeDataStoreHash(searchData.metadata),
            "metadata preimage is incorrect"
        );
        //make sure search data, disclosure proof, and rollupstore are all consistent with each other
        require(searchData.metadata.globalDataStoreId == rollupStore.dataStoreId, "seachData's datastore id is not consistent with given rollup store");
        require(searchData.metadata.headerHash == keccak256(disclosureProofs.header), "disclosure proofs headerhash preimage is incorrect");
        //verify that all of the provided polynomials are in fact part of the data
        require(DataLayrDisclosureLogic.batchNonInteractivePolynomialProofs(
            disclosureProofs.header,
            disclosureProofs.firstChunkNumber,
            disclosureProofs.polys,
            disclosureProofs.multiRevealProofs,
            disclosureProofs.polyEquivalenceProof
        ), "disclosure proofs are invalid");
        // get the number of systematic symbols from the header
        uint32 numSys = DataLayrDisclosureLogic.getNumSysFromHeader(disclosureProofs.header);
        require(disclosureProofs.firstChunkNumber + disclosureProofs.polys.length <= numSys, "Can only prove data from the systematic chunks");
        //parse proven data
        bytes memory provenString = parse(disclosureProofs.polys, startIndex, FRAUD_STRING.length);
        //sanity check
        require(provenString.length == FRAUD_STRING.length, "Parsing error, proven string is different length than fraud string");
        //check whether provenString == FRAUD_STRING
        require(keccak256(provenString) == keccak256(FRAUD_STRING), "proven string != fraud string");
        //slash sequencer because fraud is proven
        l2BlockRollupStores[l2Block].status = RollupStoreStatus.REVERTED;
        emit RollupStoreReverted(searchData.metadata.globalDataStoreId, l2Block);
    }
}
