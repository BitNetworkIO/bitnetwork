/* Imports: External */
import { DeployFunction } from 'hardhat-deploy/dist/types'

// @ts-ignore
import { names } from '../src/address-names'
import {
  deployAndVerifyAndThen,
  getContractFromArtifact,
} from '../src/deploy-utils'
import {awaitCondition, hexStringEquals} from "@mantleio/core-utils";

// eslint-disable-next-line @typescript-eslint/no-var-requires
const { ethers, upgrades } = require("hardhat");
const Web3 = require("web3")

const deployFn: DeployFunction = async (hre) => {
  // @ts-ignore
  const { deployer } = await hre.getNamedAccounts()

  const Lib_AddressManager = await getContractFromArtifact(
    hre,
    names.unmanaged.Lib_AddressManager
  )
  // @ts-ignore
  const owner = hre.deployConfig.bvmAddressManagerOwner
  // @ts-ignore
  const l1BitAddress = hre.deployConfig.l1BitAddress

  // deploy assertionMap impl
  await deployAndVerifyAndThen({
    hre,
    name: names.managed.fraud_proof.AssertionMap,
    contract: 'AssertionMap',
    args: [],
  })
  const Impl__AssertionMap = await getContractFromArtifact(
    hre,
    names.managed.fraud_proof.AssertionMap,
    {
      iface: 'AssertionMap',
      signerOrProvider: deployer,
    }
  )
  console.log('AssertionMap Implementation Address', Impl__AssertionMap.address)
  console.log('deploy fraud proof assertionMap success')

  // deploy verifier impl
  await deployAndVerifyAndThen({
    hre,
    name: names.managed.fraud_proof.VerifierEntry,
    contract: 'VerifierEntry',
    args: [],
  })
  const Impl__Verifier = await getContractFromArtifact(
    hre,
    names.managed.fraud_proof.VerifierEntry,
    {
      iface: 'VerifierEntry',
      signerOrProvider: deployer,
    }
  )
  console.log('Verifier Implementation Address', Impl__Verifier.address)
  console.log('deploy fraud proof verifier success')

  // deploy rollup impl
  await deployAndVerifyAndThen({
    hre,
    name: names.managed.fraud_proof.Rollup,
    contract: 'Rollup',
    args: [],
  })
  const Impl__Rollup = await getContractFromArtifact(
    hre,
    names.managed.fraud_proof.Rollup,
    {
      iface: 'Rollup',
      signerOrProvider: deployer,
    }
  )
  console.log('Rollup Implementation Address', Impl__Rollup.address)
  console.log('deploy fraud proof assertion rollup success')

  // deploy assertionMap proxy
  let callData = Impl__Verifier.interface.encodeFunctionData('initialize', [])
  await deployAndVerifyAndThen({
    hre,
    name: names.managed.fraud_proof.Proxy__AssertionMap,
    contract: 'TransparentUpgradeableProxy',
    iface: 'AssertionMap',
    args: [Impl__AssertionMap.address, owner, callData],
  })
  console.log('deploy fraud proof assertionMap proxy success')
  const Proxy__AssertionMap = await getContractFromArtifact(
    hre,
    names.managed.fraud_proof.Proxy__AssertionMap,
    {
      iface: 'AssertionMap',
      signerOrProvider: deployer,
    }
  )
  console.log('Proxy__AssertionMap Address', Proxy__AssertionMap.address)
  console.log('deploy fraud proof Proxy__AssertionMap success')

  // deploy verifier proxy
  callData = Impl__Verifier.interface.encodeFunctionData('initialize', [])
  await deployAndVerifyAndThen({
    hre,
    name: names.managed.fraud_proof.Proxy__Verifier,
    contract: 'TransparentUpgradeableProxy',
    iface: 'VerifierEntry',
    args: [Impl__Verifier.address, owner, callData],
  })
  console.log('deploy fraud proof verifier proxy success')

  // deploy rollup proxy
  const rollupArgs = [
    owner, // address _owner
    Impl__Verifier.address, // address _verifier,
    l1BitAddress, // address _stakeToken,
    Lib_AddressManager.address, // address _libAddressManager,
    Proxy__AssertionMap.address, // address _assertionMap,
    5, // uint256 _confirmationPeriod,
    0, // uint256 _challengePeriod,
    0, // uint256 _minimumAssertionPeriod,
    // 1000000000000, // uint256 _maxGasPerAssertion,
    0, // uint256 _baseStakeAmount
    '0x365a761a909633fc9742c81c90b3fc71cebfd64cc1ae1ed1f36b3c5e956e0f28', // bytes32 _initialVMhash //TODO-FIXME
  ]
  callData = Impl__Rollup.interface.encodeFunctionData('initialize', rollupArgs)
  await deployAndVerifyAndThen({
    hre,
    name: names.managed.fraud_proof.Proxy__Rollup,
    contract: 'TransparentUpgradeableProxy',
    iface: 'Rollup',
    args: [Impl__Rollup.address, owner, callData],
    postDeployAction: async (contract) => {
      // Theoretically it's not necessary to initialize this contract since it sits behind
      // a proxy. However, it's best practice to initialize it anyway just in case there's
      // some unknown security hole. It also prevents another user from appearing like an
      // official address because it managed to call the initialization function.
      // console.log(`Initializing fraud-proof Rollup (implementation)...`)
      // await contract.initialize(...rollupArgs)

      console.log(`Checking that contract was correctly initialized...`)
      await awaitCondition(
        async () => {
          return hexStringEquals(
            await contract.libAddressManager(),
            Lib_AddressManager.address
          )
        },
        5000,
        100
      )
      console.log('>>>>', contract.assertions())
      await awaitCondition(
        async () => {
          return hexStringEquals(
            await contract.assertions(),
            Proxy__AssertionMap.address
          )
        },
        5000,
        100
      )
    },
  })
  console.log('deploy fraud proof rollup proxy success')
  const Proxy__Rollup = await getContractFromArtifact(
    hre,
    names.managed.fraud_proof.Proxy__Rollup,
    {
      iface: 'Rollup',
      signerOrProvider: deployer,
    }
  )
  console.log('Proxy__Rollup Address', Proxy__Rollup.address)
  console.log('deploy fraud proof Proxy__Rollup success')
}

// This is kept during an upgrade. So no upgrade tag.
deployFn.tags = ['FraudProof', 'upgrade']

export default deployFn
