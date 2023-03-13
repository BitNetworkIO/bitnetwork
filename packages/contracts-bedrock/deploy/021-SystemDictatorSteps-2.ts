import assert from 'assert'

import { ethers } from 'ethers'
import { DeployFunction } from 'hardhat-deploy/dist/types'
import { awaitCondition } from '@mantleio/core-utils'
import '@mantleio/hardhat-deploy-config'
import 'hardhat-deploy'
import '@nomiclabs/hardhat-ethers'

import {
  assertContractVariable,
  getContractsFromArtifacts,
  jsonifyTransaction,
  isStep,
  doStep,
} from '../src/deploy-utils'

const deployFn: DeployFunction = async (hre) => {
  const { deployer } = await hre.getNamedAccounts()

  // Set up required contract references.
  const [
    SystemDictator,
    ProxyAdmin,
    AddressManager,
    L1CrossDomainMessenger,
    L1StandardBridgeProxy,
    L1StandardBridge,
    L2OutputOracle,
    MantlePortal,
    MantleMintableERC20Factory,
    L1ERC721Bridge,
  ] = await getContractsFromArtifacts(hre, [
    {
      name: 'SystemDictatorProxy',
      iface: 'SystemDictator',
      signerOrProvider: deployer,
    },
    {
      name: 'ProxyAdmin',
      signerOrProvider: deployer,
    },
    {
      name: 'Lib_AddressManager',
      signerOrProvider: deployer,
    },
    {
      name: 'Proxy__BVM_L1CrossDomainMessenger',
      iface: 'L1CrossDomainMessenger',
      signerOrProvider: deployer,
    },
    {
      name: 'Proxy__BVM_L1StandardBridge',
    },
    {
      name: 'Proxy__BVM_L1StandardBridge',
      iface: 'L1StandardBridge',
      signerOrProvider: deployer,
    },
    {
      name: 'L2OutputOracleProxy',
      iface: 'L2OutputOracle',
      signerOrProvider: deployer,
    },
    {
      name: 'MantlePortalProxy',
      iface: 'MantlePortal',
      signerOrProvider: deployer,
    },
    {
      name: 'MantleMintableERC20FactoryProxy',
      iface: 'MantleMintableERC20Factory',
      signerOrProvider: deployer,
    },
    {
      name: 'L1ERC721BridgeProxy',
      iface: 'L1ERC721Bridge',
      signerOrProvider: deployer,
    },
  ])

  // If we have the key for the controller then we don't need to wait for external txns.
  const isLiveDeployer =
    deployer.toLowerCase() === hre.deployConfig.controller.toLowerCase()

  // Step 3 clears out some state from the AddressManager.
  await doStep({
    isLiveDeployer,
    SystemDictator,
    step: 3,
    message: `
      Step 3 will clear out some legacy state from the AddressManager. Once you execute this step,
      you WILL NOT BE ABLE TO RESTART THE SYSTEM using exit1(). You should confirm that the L2
      system is entirely operational before executing this step.
    `,
    checks: async () => {
      const deads = [
        'BVM_CanonicalTransactionChain',
        'BVM_L2CrossDomainMessenger',
        'BVM_DecompressionPrecompileAddress',
        'BVM_Sequencer',
        'BVM_Proposer',
        'BVM_ChainStorageContainer-CTC-batches',
        'BVM_ChainStorageContainer-CTC-queue',
        'BVM_CanonicalTransactionChain',
        'BVM_StateCommitmentChain',
        'BVM_BondManager',
        'BVM_ExecutionManager',
        'BVM_FraudVerifier',
        'BVM_StateManagerFactory',
        'BVM_StateTransitionerFactory',
        'BVM_SafetyChecker',
        'BVM_L1MultiMessageRelayer',
        'BondManager',
      ]
      for (const dead of deads) {
        assert(
          (await AddressManager.getAddress(dead)) ===
            ethers.constants.AddressZero
        )
      }
    },
  })

  // Step 4 transfers ownership of the AddressManager and L1StandardBridge to the ProxyAdmin.
  await doStep({
    isLiveDeployer,
    SystemDictator,
    step: 4,
    message: `
      Step 4 will transfer ownership of the AddressManager and L1StandardBridge to the ProxyAdmin.
    `,
    checks: async () => {
      await assertContractVariable(AddressManager, 'owner', ProxyAdmin.address)

      assert(
        (await L1StandardBridgeProxy.callStatic.getOwner({
          from: ethers.constants.AddressZero,
        })) === ProxyAdmin.address
      )
    },
  })

  // Make sure the dynamic system configuration has been set.
  if (
    (await isStep(SystemDictator, 5)) &&
    !(await SystemDictator.dynamicConfigSet())
  ) {
    console.log(`
      You must now set the dynamic L2OutputOracle configuration by calling the function
      updateL2OutputOracleDynamicConfig. You will need to provide the
      l2OutputOracleStartingBlockNumber and the l2OutputOracleStartingTimestamp which can both be
      found by querying the last finalized block in the L2 node.
    `)

    if (isLiveDeployer) {
      console.log(`Updating dynamic oracle config...`)

      // Use default starting time if not provided
      let deployL2StartingTimestamp =
        hre.deployConfig.l2OutputOracleStartingTimestamp
      if (deployL2StartingTimestamp < 0) {
        const l1StartingBlock = await hre.ethers.provider.getBlock(
          hre.deployConfig.l1StartingBlockTag
        )
        if (l1StartingBlock === null) {
          throw new Error(
            `Cannot fetch block tag ${hre.deployConfig.l1StartingBlockTag}`
          )
        }
        deployL2StartingTimestamp = l1StartingBlock.timestamp
      }

      await SystemDictator.updateDynamicConfig(
        {
          l2OutputOracleStartingBlockNumber:
            hre.deployConfig.l2OutputOracleStartingBlockNumber,
          l2OutputOracleStartingTimestamp: deployL2StartingTimestamp,
        },
        false // do not pause the the MantlePortal when initializing
      )
    } else {
      const tx = await SystemDictator.populateTransaction.updateDynamicConfig(
        {
          l2OutputOracleStartingBlockNumber:
            hre.deployConfig.l2OutputOracleStartingBlockNumber,
          l2OutputOracleStartingTimestamp:
            hre.deployConfig.l2OutputOracleStartingTimestamp,
        },
        true
      )
      console.log(`Please update dynamic oracle config...`)
      console.log(`MSD address: ${SystemDictator.address}`)
      console.log(`JSON:`)
      console.log(jsonifyTransaction(tx))
    }

    await awaitCondition(
      async () => {
        return SystemDictator.dynamicConfigSet()
      },
      30000,
      1000
    )
  }

  // Step 5 initializes all contracts.
  await doStep({
    isLiveDeployer,
    SystemDictator,
    step: 5,
    message: `
      Step 5 will initialize all Bedrock contracts. After this step is executed, the MantlePortal
      will be open for deposits but withdrawals will be paused if deploying a production network.
      The Proposer will also be able to submit L2 outputs to the L2OutputOracle.
    `,
    checks: async () => {
      // Check L2OutputOracle was initialized properly.
      await assertContractVariable(
        L2OutputOracle,
        'latestBlockNumber',
        hre.deployConfig.l2OutputOracleStartingBlockNumber
      )

      // Check MantlePortal was initialized properly.
      await assertContractVariable(
        MantlePortal,
        'l2Sender',
        '0x000000000000000000000000000000000000dEaD'
      )
      const resourceParams = await MantlePortal.params()
      assert(
        resourceParams.prevBaseFee.eq(await MantlePortal.INITIAL_BASE_FEE()),
        `MantlePortal was not initialized with the correct initial base fee`
      )
      assert(
        resourceParams.prevBoughtGas.eq(0),
        `MantlePortal was not initialized with the correct initial bought gas`
      )
      assert(
        !resourceParams.prevBlockNum.eq(0),
        `MantlePortal was not initialized with the correct initial block number`
      )
      assert(
        (await hre.ethers.provider.getBalance(L1StandardBridge.address)).eq(0)
      )

      if (isLiveDeployer) {
        await assertContractVariable(MantlePortal, 'paused', false)
      } else {
        await assertContractVariable(MantlePortal, 'paused', true)
      }

      // Check L1CrossDomainMessenger was initialized properly.
      try {
        await L1CrossDomainMessenger.xDomainMessageSender()
        assert(false, `L1CrossDomainMessenger was not initialized properly`)
      } catch (err) {
        // Expected.
      }

      // Check L1StandardBridge was initialized properly.
      await assertContractVariable(
        L1StandardBridge,
        'messenger',
        L1CrossDomainMessenger.address
      )
      assert(
        (await hre.ethers.provider.getBalance(L1StandardBridge.address)).eq(0)
      )

      // Check MantleMintableERC20Factory was initialized properly.
      await assertContractVariable(
        MantleMintableERC20Factory,
        'BRIDGE',
        L1StandardBridge.address
      )

      // Check L1ERC721Bridge was initialized properly.
      await assertContractVariable(
        L1ERC721Bridge,
        'messenger',
        L1CrossDomainMessenger.address
      )
    },
  })

  // Step 6 unpauses the MantlePortal.
  if (await isStep(SystemDictator, 6)) {
    console.log(`
      Unpause the MantlePortal. The GUARDIAN account should be used. In practice
      this is the multisig. In test networks, the MantlePortal is initialized
      without being paused.
    `)

    if (isLiveDeployer) {
      console.log('WARNING: MantlePortal configured to not be paused')
      console.log('This should only happen for test environments')
      await assertContractVariable(MantlePortal, 'paused', false)
    } else {
      const tx = await MantlePortal.populateTransaction.unpause()
      console.log(`Please unpause the MantlePortal...`)
      console.log(`MantlePortal address: ${MantlePortal.address}`)
      console.log(`JSON:`)
      console.log(jsonifyTransaction(tx))
    }

    await awaitCondition(
      async () => {
        const paused = await MantlePortal.paused()
        return !paused
      },
      30000,
      1000
    )

    await assertContractVariable(MantlePortal, 'paused', false)

    console.log(`
      You must now finalize the upgrade by calling finalize() on the SystemDictator. This will
      transfer ownership of the ProxyAdmin to the final system owner as specified in the deployment
      configuration.
    `)

    if (isLiveDeployer) {
      console.log(`Finalizing deployment...`)
      await SystemDictator.finalize()
    } else {
      const tx = await SystemDictator.populateTransaction.finalize()
      console.log(`Please finalize deployment...`)
      console.log(`MSD address: ${SystemDictator.address}`)
      console.log(`JSON:`)
      console.log(jsonifyTransaction(tx))
    }

    await awaitCondition(
      async () => {
        return SystemDictator.finalized()
      },
      30000,
      1000
    )

    await assertContractVariable(
      ProxyAdmin,
      'owner',
      hre.deployConfig.finalSystemOwner
    )
  }
}

deployFn.tags = ['SystemDictatorSteps', 'phase2']

export default deployFn
