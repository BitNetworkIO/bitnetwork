import { HardhatUserConfig } from 'hardhat/types'
import 'solidity-coverage'
import * as dotenv from 'dotenv'
import { ethers } from 'ethers'

// Hardhat plugins
import '@openzeppelin/hardhat-upgrades'
import '@mantleio/hardhat-deploy-config'
import '@nomiclabs/hardhat-ethers'
import '@nomiclabs/hardhat-waffle'
import '@nomiclabs/hardhat-etherscan'
// import '@primitivefi/hardhat-dodoc'
import '@typechain/hardhat'
import 'hardhat-deploy'
import 'hardhat-gas-reporter'
import 'hardhat-output-validator'
import 'hardhat-abi-exporter'

// Hardhat tasks
import './tasks'

// Load environment variables from .env
dotenv.config()

const enableGasReport = !!process.env.ENABLE_GAS_REPORT
const privateKey = process.env.PRIVATE_KEY || '0x' + '11'.repeat(32) // this is to avoid hardhat error
const deploy = process.env.DEPLOY_DIRECTORY || 'deploy'

import { copySync, remove } from 'fs-extra'
import { subtask } from 'hardhat/config'
import {
  TASK_COMPILE_SOLIDITY_GET_SOURCE_PATHS,
  TASK_COMPILE_SOLIDITY_LOG_COMPILATION_RESULT,
  TASK_COMPILE_SOLIDITY_LOG_NOTHING_TO_COMPILE,
} from 'hardhat/builtin-tasks/task-names'

import { spawnSync } from 'child_process'

subtask(TASK_COMPILE_SOLIDITY_GET_SOURCE_PATHS).setAction(
  async (_, __, runSuper) => {
    console.log('running task')
    // copySync(
    //   '../../datalayr/contracts/eignlayr-contracts/src',
    //   './contracts/libraries/eigenda/lib'
    // )
    const paths = await runSuper()
    const filteredPaths = paths.filter(function (p) {
      return !p.includes('eigenda')
    })
    console.log('end task')
    return filteredPaths
  }
)

subtask(TASK_COMPILE_SOLIDITY_LOG_COMPILATION_RESULT).setAction(
  async (_, __, runSuper) => {
    console.log('running TASK_COMPILE_SOLIDITY_LOG_COMPILATION_RESULT')

    // delete
    // await remove('./contracts/libraries/eigenda')

    runSuper()
  }
)

subtask(TASK_COMPILE_SOLIDITY_LOG_NOTHING_TO_COMPILE).setAction(
  async (_, __, runSuper) => {
    console.log('running TASK_COMPILE_SOLIDITY_LOG_NOTHING_TO_COMPILE')

    // delete
    // await remove('./contracts/libraries/eigenda')
    runSuper()
  }
)

// @ts-ignore
// @ts-ignore
// @ts-ignore
const config: HardhatUserConfig = {
  networks: {
    hardhat: {
      live: false,
      saveDeployments: false,
      tags: ['local'],
    },
    local: {
      chainId: 31337,
      url: 'http://127.0.0.1:9545',
      accounts: [privateKey],
    },
    dev: {
      chainId: 31337,
      url: 'https://mantle-l1chain.dev.davionlabs.com',
      accounts: [
        'dbda1821b80551c9d65939329250298aa3472ba22feea921c0cf5d620ea67b97',
        'ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80',
      ],
    },
    mantle: {
      url: 'http://127.0.0.1:8545',
      saveDeployments: false,
    },
    'mantle-kovan': {
      chainId: 69,
      url: 'https://kovan.mantle.io',
      deploy,
      accounts: [privateKey],
    },
    'mantle-mainnet': {
      chainId: 10,
      url: 'https://mainnet.mantle.io',
      deploy,
      accounts: [privateKey],
    },
    'mainnet-trial': {
      chainId: 42069,
      url: 'http://127.0.0.1:8545',
      accounts: [privateKey],
    },
    goerlibn: {
      chainId: 5,
      url: 'https://eth-goerli.g.alchemy.com/v2/821_LFssCCQnEG3mHnP7tSrc87IQKsUp',
      deploy,
      accounts: [privateKey],
    },
    'goerli-qa': {
      chainId: 5,
      url: 'https://goerli.infura.io/v3/d6167662f2104fbc8d5a947e59dbaa28',
      deploy,
      accounts: [privateKey],
    },
    'goerli-testnet': {
      chainId: 5,
      //url: 'https://goerli.infura.io/v3/d6167662f2104fbc8d5a947e59dbaa28',
      url: 'https://goerli.davionlabs.com',
      deploy,
      accounts: [privateKey],
      gas: 'auto',
      gasPrice: 88496120920,
gasMultiplier: 5,
    },
    kovan: {
      chainId: 42,
      url: process.env.CONTRACTS_RPC_URL || '',
      deploy,
      accounts: [privateKey],
    },
    mainnet: {
      chainId: 1,
      url: process.env.CONTRACTS_RPC_URL || '',
      deploy,
      accounts: [privateKey],
    },
  },
  mocha: {
    timeout: 50000,
  },
  solidity: {
    compilers: [
      {
        version: '0.8.9',
        settings: {
          optimizer: { enabled: true, runs: 200 },
        },
      },
      {
        version: '0.5.17', // Required for WETH9
        settings: {
          optimizer: { enabled: true, runs: 200 },
        },
      },
    ],
    settings: {
      metadata: {
        bytecodeHash: 'none',
      },
      outputSelection: {
        '*': {
          '*': ['metadata', 'storageLayout'],
        },
      },
    },
  },
  typechain: {
    outDir: 'dist/types',
    target: 'ethers-v5',
  },
  paths: {
    deploy: './deploy',
    deployments: './deployments',
    deployConfig: './deploy-config',
  },
  abiExporter: {
    path: './abi',
    runOnCompile: true,
    clear: true,
  },
  namedAccounts: {
    deployer: {
      default: 0,
    },
  },
  gasReporter: {
    enabled: enableGasReport,
    currency: 'USD',
    gasPrice: 100,
    outputFile: process.env.CI ? 'gas-report.txt' : undefined,
  },
  etherscan: {
    apiKey: {
      mainnet: process.env.ETHERSCAN_API_KEY,
      goerli: process.env.ETHERSCAN_API_KEY,
      sepolia: process.env.ETHERSCAN_API_KEY,
    },
    customChains: [
    {
      network: "sepolia",
      chainId: 11155111,
      urls: {
        apiURL: "https://api-sepolia.etherscan.io/api",
        browserURL: "https://sepolia.etherscan.io"
      }
    }
    ]
  },
  dodoc: {
    runOnCompile: true,
    exclude: [
      'Helper_GasMeasurer',
      'Helper_SimpleProxy',
      'TestERC20',
      'TestLib_CrossDomainUtils',
      'TestLib_BVMCodec',
      'TestLib_RLPReader',
      'TestLib_RLPWriter',
      'TestLib_AddressAliasHelper',
      'TestLib_MerkleTrie',
      'TestLib_SecureMerkleTrie',
      'TestLib_Buffer',
      'TestLib_Bytes32Utils',
      'TestLib_BytesUtils',
      'TestLib_MerkleTree',
    ],
  },
  outputValidator: {
    runOnCompile: true,
    errorMode: false,
    checks: {
      events: false,
      variables: false,
    },
    exclude: ['contracts/test-helpers', 'contracts/test-libraries'],
  },
  deployConfigSpec: {
    allowUnlimitedContractSize: {
      type: 'boolean',
      default: true,
    },
    isForkedNetwork: {
      type: 'boolean',
      default: false,
    },
    numDeployConfirmations: {
      type: 'number',
      default: 0,
    },
    gasPrice: {
      type: 'number',
      default: undefined,
    },
    l1BlockTimeSeconds: {
      type: 'number',
    },
    l2BlockGasLimit: {
      type: 'number',
    },
    l2ChainId: {
      type: 'number',
    },
    ctcL2GasDiscountDivisor: {
      type: 'number',
    },
    ctcEnqueueGasCost: {
      type: 'number',
    },
    sccFaultProofWindowSeconds: {
      type: 'number',
    },
    sccSequencerPublishWindowSeconds: {
      type: 'number',
    },
    blockStaleMeasure: {
      type: 'number',
    },
    daFraudProofPeriod: {
      type: 'number',
    },
    l2SubmittedBlockNumber: {
      type: 'number',
    },
    bvmSequencerAddress: {
      type: 'address',
    },
    bvmProposerAddress: {
      type: 'address',
    },
    bvmBlockSignerAddress: {
      type: 'address',
    },
    bvmFeeWalletAddress: {
      type: 'address',
    },
    bvmAddressManagerOwner: {
      type: 'address',
    },
    bvmGasPriceOracleOwner: {
      type: 'address',
    },
    bvmFeeWalletOwner: {
      type: 'address',
    },
    bvmWhitelistOwner: {
      type: 'address',
      default: ethers.constants.AddressZero,
    },
    dataManagerAddress: {
      type: 'address',
    },
    bvmEigenSequencerAddress: {
      type: 'address',
    },
    sccAddress: {
      type: 'address',
      default: 0,
    },
    gasPriceOracleOverhead: {
      type: 'number',
      default: 2750,
    },
    gasPriceOracleScalar: {
      type: 'number',
      default: 1_500_000,
    },
    gasPriceOracleDecimals: {
      type: 'number',
      default: 6,
    },
    gasPriceOracleIsBurning: {
      type: 'number',
      default: 0,
    },
    gasPriceOracleCharge: {
      type: 'number',
      default: 1,
    },
    gasPriceOracleL1BaseFee: {
      type: 'number',
      default: 1,
    },
    gasPriceOracleL2GasPrice: {
      type: 'number',
      default: 50_000_000,
    },
    hfBerlinBlock: {
      type: 'number',
      default: 0,
    },
    contractsDeployerKey: {
      type: 'string',
    },
    contractsRpcUrl: {
      type: 'string',
    },
    tssRewardSendAmountPerYear: {
      type: 'number',
    },
    tssRewardWaitingTime: {
      type: 'number',
    },
    tssDelegationManagerMinStakeAmount: {
      type: 'string',
    },
    tssManagerAddress: {
      type: 'string',
    },
  },
}

if (
  process.env.CONTRACTS_TARGET_NETWORK &&
  process.env.CONTRACTS_DEPLOYER_KEY &&
  process.env.CONTRACTS_RPC_URL
) {
  config.networks[process.env.CONTRACTS_TARGET_NETWORK] = {
    accounts: [process.env.CONTRACTS_DEPLOYER_KEY],
    url: process.env.CONTRACTS_RPC_URL,
    live: true,
    saveDeployments: true,
    gas: 'auto',
    gasPrice: 88496120920,
gasMultiplier: 5,
    tags: [process.env.CONTRACTS_TARGET_NETWORK],
  }
}

export default config
