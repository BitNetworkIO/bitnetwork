// SPDX-License-Identifier: MIT
pragma solidity ^0.8.9;

import {ITssRewardContract} from  "./iTssRewardContract.sol";
import {IBVM_GasPriceOracle} from "./iBVM_GasPriceOracle.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {Lib_PredeployAddresses} from "../../libraries/constants/Lib_PredeployAddresses.sol";

/* Library Imports */
import {SafeMath} from "@openzeppelin/contracts/utils/math/SafeMath.sol";

/* Interface Imports */

/* External Imports */

/**
 * @title TssRewardContract
 * @dev Release to batch roll up tss members.
 */
contract TssRewardContract is Ownable,ITssRewardContract {
    using SafeMath for uint256;

    mapping(uint256 => uint256) public ledger;
    address public bvmGasPriceOracleAddress;
    address public deadAddress;
    uint256 public dust;
    uint256 public bestBlockID;
    uint256 public totalAmount;
    uint256 public latsBatchTime;
    uint256 public sendAmountPerYear;
    address public l2Message;



    // set call address
    constructor(address _deadAddress, address _owner, uint256 _sendAmountPerYear, address _bvmGasPriceOracleAddress, address _l2Message)
    Ownable()
    {
        transferOwnership(_owner);
        deadAddress = _deadAddress;
        sendAmountPerYear = _sendAmountPerYear;
        bvmGasPriceOracleAddress = _bvmGasPriceOracleAddress;
        l2Message = _l2Message;
    }

    // slither-disable-next-line locked-ether
    receive() external payable {}

    /**
     * Enforces that the modified function is only callable by a specific null address.
     *  authenticated to call this function.
     */
    modifier onlyFromDeadAddress() {
        require(
            msg.sender == deadAddress,
            "tss reward call message unauthenticated"
        );
        _;
    }

    modifier onlyL2Message() {
        require(
            msg.sender == l2Message,
            "tss reward call message unauthenticated"
        );
        _;
    }


    modifier checkBalance() {
        require(
            address(this).balance >= totalAmount,
            "balance record and contract balance are not equal"
        );
        _;
    }

    function setSendAmountPerYear(uint256 _sendAmountPerYear) public onlyOwner {
        sendAmountPerYear = _sendAmountPerYear;
    }

    function querySendAmountPerSecond() public view returns (uint256){
        return (sendAmountPerYear * 10 ** 18).div(365 * 24 * 60 * 60);
    }

    /**
     * @dev return the total undistributed amount
     */
    function queryReward() external view checkBalance returns (uint256) {
        return address(this).balance;
    }

    /**
     * @dev claimReward distribute reward to tss member.
     * @param _batchTime Batch corresponds to L1 Block Timestamp
     * @param _length The distribute batch block number
     * @param _tssMembers The address array of tss group members
     */
    function claimReward(uint256 _blockStartHeight, uint32 _length, uint256 _batchTime, address[] calldata _tssMembers)
    external
    virtual
    onlyL2Message
    {
        if (IBVM_GasPriceOracle(bvmGasPriceOracleAddress).IsBurning() != true) {
            claimRewardByBlock(_blockStartHeight, _length, _tssMembers);
            return;
        }
        uint256 sendAmount = 0;
        uint256 batchAmount = 0;
        uint256 accu = 0;
        // sendAmount
        if (latsBatchTime == 0) {
            latsBatchTime = _batchTime;
            return;
        }
        batchAmount = (_batchTime - latsBatchTime) * querySendAmountPerSecond() + dust;
        dust = 0;
        sendAmount = batchAmount.div(_tssMembers.length);
        for (uint256 j = 0; j < _tssMembers.length; j++) {
            address payable addr = payable(_tssMembers[j]);
            accu = accu.add(sendAmount);
            addr.transfer(sendAmount);
        }
        uint256 reserved = batchAmount.sub(accu);
        if (reserved > 0) {
            dust = dust.add(reserved);
        }
        emit DistributeTssReward(
            _batchTime,
            _tssMembers
        );
    }

    /**
     * @dev claimReward distribute reward to tss member.
     * @param _blockStartHeight The block height at L2 which needs to distribute profits
     * @param _length The distribute batch block number
     * @param _tssMembers The address array of tss group members
     */
    function claimRewardByBlock(uint256 _blockStartHeight, uint32 _length, address[] calldata _tssMembers)
    internal
    {
        uint256 sendAmount = 0;
        uint256 batchAmount = 0;
        uint256 accu = 0;
        // release reward from _blockStartHeight to _blockStartHeight + _length - 1
        for (uint256 i = 0; i < _length; i++) {
            batchAmount = batchAmount.add(ledger[_blockStartHeight + i]);
            // delete distributed height
            delete ledger[_blockStartHeight + i];
        }
        if (batchAmount > 0) {
            batchAmount = batchAmount + dust;
            dust = 0;
            sendAmount = batchAmount.div(_tssMembers.length);
            for (uint256 j = 0; j < _tssMembers.length; j++) {
                address payable addr = payable(_tssMembers[j]);
                accu = accu.add(sendAmount);
                totalAmount = totalAmount.sub(sendAmount);
                addr.transfer(sendAmount);
            }
            uint256 reserved = batchAmount.sub(accu);
            if (reserved > 0) {
                dust = dust.add(reserved);
            }
        }
        emit DistributeTssRewardByBlock(
            _blockStartHeight,
            _length,
            _tssMembers
        );
    }

    /**
     * @dev update tss member gas reward by every block.
     * @param _blockID The block height at L2 which needs to distribute profits
     * @return _tssMembers Address array of tss group members
     */
    function updateReward(uint256 _blockID, uint256 _amount)
    external
    onlyFromDeadAddress
    checkBalance
    returns (bool)
    {
        // check update block ID
        require(_blockID == bestBlockID + 1, "block id update illegal");
        // iter address to update balance
        bestBlockID = _blockID;
        totalAmount = totalAmount.add(_amount);
        ledger[_blockID] = _amount;
        return true;
    }

    /**
     * @dev withdraw div dust
     */
    function withdrawDust() external onlyOwner checkBalance {
        uint256 amount = dust;
        totalAmount = totalAmount.sub(dust);
        dust = 0;
        if (amount > 0) {
        payable(owner()).transfer(dust);
        }
    }

    /**
     * @dev clear balance
     */
    function withdraw() external onlyOwner checkBalance {
        totalAmount = 0;
        if (address(this).balance > 0) {
            payable(owner()).transfer(address(this).balance);
        }
    }
}
