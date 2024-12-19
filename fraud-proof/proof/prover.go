// Copyright 2022, Specular contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proof

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/mantlenetworkio/mantle/fraud-proof/proof/proof"
	"github.com/mantlenetworkio/mantle/fraud-proof/proof/prover"
	proofState "github.com/mantlenetworkio/mantle/fraud-proof/proof/state"
	"github.com/mantlenetworkio/mantle/l2geth/common"
	"github.com/mantlenetworkio/mantle/l2geth/core"
	"github.com/mantlenetworkio/mantle/l2geth/core/state"
	"github.com/mantlenetworkio/mantle/l2geth/core/types"
	"github.com/mantlenetworkio/mantle/l2geth/core/vm"
	"github.com/mantlenetworkio/mantle/l2geth/log"
	"github.com/mantlenetworkio/mantle/l2geth/rpc"
)

const (
	// defaultProveReexec is the number of blocks the prover is willing to go back
	// and reexecute to produce missing historical state necessary to run a specific
	// trace.
	defaultProveReexec = uint64(128)
)

type ProverConfig struct {
	Reexec *uint64
}

type ExecutionState struct {
	VMHash         common.Hash
	BlockGasUsed   *big.Int
	StateType      proofState.StateType
	Block          *types.Block
	TransactionIdx uint64
	StepIdx        uint64
}

func (s *ExecutionState) MarshalJson() ([]byte, error) {
	return json.Marshal(&struct {
		VMHash         common.Hash `json:"vmHash"`
		BlockGasUsed   *big.Int    `json:"blockGasUsed"`
		StateType      string      `json:"stateType"`
		BlockHash      common.Hash `json:"blockHash"`
		TransactionIdx uint64      `json:"txnIdx"`
		StepIdx        uint64      `json:"stepIdx"`
	}{
		VMHash:         s.VMHash,
		BlockGasUsed:   s.BlockGasUsed,
		StateType:      string(s.StateType),
		BlockHash:      s.Block.Hash(),
		TransactionIdx: s.TransactionIdx,
		StepIdx:        s.StepIdx,
	})
}

func (s *ExecutionState) Hash() common.Hash {
	return s.VMHash
}

// [GenerateStates] generates execution states across blocks [startNum, endNum)
//
// For example there are 2 blocks: block A with transactions A1, A2; block B without transactions.
//
// The states are (in order):
//  1. the BlockState of the block right before A
//  2. the InterState before A1
//  3. the IntraStates of A1 if A1 is not an EOA transfer
//  4. the InterState before A2 (after A1)
//  5. the IntraStates of A2 if A2 is not an EOA transfer
//  6. the InterState after A2
//  7. the BlockState of the block A
//  8. a dummy InterState
//  9. the BlockState of the block B
func GenerateStates(backend Backend, ctx context.Context, startNum, endNum uint64, config *ProverConfig) ([]*ExecutionState, error) {
	var states []*ExecutionState
	var msg types.Message
	var block *types.Block
	var err error

	chainCtx := createChainContext(backend, ctx)
	startParent, err := backend.BlockByNumber(ctx, rpc.BlockNumber(startNum-1))
	if err != nil {
		return nil, err
	}
	bs, statedb, err := generateStartBlockState(backend, ctx, startParent, config)
	if err != nil {
		return nil, err
	}

	//states = append(states, &ExecutionState{
	//	VMHash:         bs.Hash(),
	//	BlockGasUsed:   common.Big0,
	//	StateType:      proofState.BlockStateType,
	//	Block:          startParent,
	//	TransactionIdx: 0,
	//	StepIdx:        0,
	//})
	// append start vmHash
	startHeader, err := backend.HeaderByNumber(ctx, rpc.BlockNumber(startNum))
	if err != nil {
		return nil, err
	}
	states = append(states, &ExecutionState{
		VMHash:         startHeader.Root,
		BlockGasUsed:   common.Big0,
		StateType:      proofState.BlockStateType,
		Block:          startParent,
		TransactionIdx: 0,
		StepIdx:        0,
	})
	log.Info("Get start state", "startNum", startNum, "VMHash", startHeader.Root.String())

	for num := startNum; num < endNum; num++ {
		// Preparation of block context
		if block, err = backend.BlockByNumber(ctx, rpc.BlockNumber(num)); err != nil {
			return nil, err
		}
		if block == nil {
			return nil, fmt.Errorf("block #%d not found", num)
		}
		blockCtx := core.NewEVMBlockContext(block.Header(), chainCtx, nil)
		blockHashTree, err := proofState.BlockHashTreeFromBlockContext(&blockCtx)
		if err != nil {
			return nil, err
		}

		transactions := block.Transactions()
		receipts, _ := backend.GetReceipts(ctx, block.Hash())

		blockGasUsed := new(big.Int).SetUint64(block.GasUsed())
		localBlockGasUsed := new(big.Int)
		// Trace all the transactions contained within
		for i, tx := range transactions {
			// Call Prepare to clear out the statedb access list
			statedb.Prepare(tx.Hash(), block.Hash(), i)
			// Push the interstate before transaction i
			its := proofState.InterStateFromCaptured(
				block.NumberU64(),
				uint64(i),
				statedb,
				blockGasUsed,
				transactions,
				receipts,
				blockHashTree,
			)
			states = append(states, &ExecutionState{
				VMHash:         its.Hash(),
				BlockGasUsed:   blockGasUsed,
				StateType:      proofState.InterStateType,
				Block:          block,
				TransactionIdx: uint64(i),
				StepIdx:        0,
			})

			// Execute transaction i with intra state generator enabled.
			stateGenerator := prover.NewIntraStateGenerator(block.NumberU64(), uint64(i), statedb, *its, blockHashTree)
			txCtx, err := generateTxCtx(backend, ctx, block, tx)
			if err != nil {
				return nil, err
			}
			vmenv := vm.NewEVM(*txCtx, statedb, backend.ChainConfig(), vm.Config{Debug: true, Tracer: stateGenerator})
			signer := types.MakeSigner(backend.ChainConfig(), block.Number())
			if msg, err = tx.AsMessage(signer); err != nil {
				return nil, err
			}
			_, usedGas, _, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(msg.Gas()))
			if err != nil {
				return nil, fmt.Errorf("tracing failed: %w", err)
			}
			generatedStates, err := stateGenerator.GetGeneratedStates()
			if err != nil {
				return nil, fmt.Errorf("tracing failed: %w", err)
			}
			for idx, s := range generatedStates {
				states = append(states, &ExecutionState{
					VMHash:         s.VMHash,
					BlockGasUsed:   new(big.Int).Add(blockGasUsed, new(big.Int).SetUint64(tx.Gas()-s.Gas)),
					StateType:      proofState.IntraStateType,
					Block:          block,
					TransactionIdx: uint64(i),
					StepIdx:        uint64(idx + 1),
				})
			}
			// Include refund
			localBlockGasUsed.Add(localBlockGasUsed, new(big.Int).SetUint64(usedGas))
		}

		// Push the inter state after all transactions
		its := proofState.InterStateFromCaptured(
			block.NumberU64(),
			uint64(len(transactions)),
			statedb,
			localBlockGasUsed,
			transactions,
			receipts,
			blockHashTree,
		)
		states = append(states, &ExecutionState{
			VMHash:         its.Hash(),
			BlockGasUsed:   localBlockGasUsed,
			StateType:      proofState.InterStateType,
			Block:          block,
			TransactionIdx: uint64(len(block.Transactions())),
			StepIdx:        0,
		})

		// Get next statedb to skip simulating block finalization
		// Here the statedb is the state at the end of the current block
		// (i.e. start of the new block)
		if bs, statedb, err = generateStartBlockState(backend, ctx, block, config); err != nil {
			return nil, err
		}
		states = append(states, &ExecutionState{
			VMHash:         bs.Hash(),
			BlockGasUsed:   common.Big0,
			StateType:      proofState.BlockStateType,
			Block:          block,
			TransactionIdx: 0,
			StepIdx:        0,
		})
	}

	// append end vmHash
	endParent, err := backend.BlockByNumber(ctx, rpc.BlockNumber(endNum-1))
	if err != nil {
		return nil, err
	}
	endHeader, err := backend.HeaderByNumber(ctx, rpc.BlockNumber(endNum))
	if err != nil {
		return nil, err
	}

	states = append(states, &ExecutionState{
		VMHash:         endHeader.Root,
		BlockGasUsed:   common.Big0,
		StateType:      proofState.BlockStateType,
		Block:          endParent,
		TransactionIdx: 0,
		StepIdx:        0,
	})

	log.Info("Get end state", "endNum", endNum, "VMHash", endHeader.Root.String())
	return states, nil
}

// [GenerateProof] serves as an entrypoint for one-step proof generation.
// There are 6 types of one-step proofs:
//  1. BlockState -> InterState: block initiation
//  2. InterState -> IntraState: transaction initiation (contract call or creation)
//  3. InterState -> InterState: EOA transfer transaction
//  4. IntraState -> IntraState: one-step EVM execution (require tracing)
//  5. IntraState -> InterState: transaction finalization (require tracing)
//  6. InterState -> BlockState: block finalization
func GenerateProof(ctx context.Context, backend Backend, startState *ExecutionState, config *ProverConfig) (*proof.OneStepProof, error) {
	if startState.Block == nil {
		return nil, fmt.Errorf("bad start state")
	}
	transactions := startState.Block.Transactions()
	if startState.TransactionIdx > uint64(len(transactions)) {
		return nil, fmt.Errorf("bad start state")
	}

	reexec := defaultProveReexec
	if config != nil && config.Reexec != nil {
		reexec = *config.Reexec
	}

	// Type 1: block initiation or Type 6: block finalization
	if startState.StateType == proofState.BlockStateType || (startState.StateType == proofState.InterStateType && startState.TransactionIdx == uint64(len(transactions))) {
		statedb, err := backend.StateAtBlock(ctx, startState.Block, reexec, nil, true, false)
		if err != nil {
			return nil, err
		}
		chainCtx := createChainContext(backend, ctx)
		vmctx := core.NewEVMBlockContext(startState.Block.Header(), chainCtx, nil)
		blockHashTree, err := proofState.BlockHashTreeFromBlockContext(&vmctx)
		if err != nil {
			return nil, err
		}
		if startState.StateType == proofState.BlockStateType {
			// Type 1: block initiation
			bs, err := proofState.BlockStateFromBlock(startState.Block.NumberU64(), statedb, blockHashTree)
			if err != nil {
				return nil, err
			}
			return proof.GetBlockInitiationProof(bs)
		} else {
			// Type 6: block finalization
			receipts, _ := backend.GetReceipts(ctx, startState.Block.Hash())
			its := proofState.InterStateFromCaptured(
				startState.Block.NumberU64(),
				startState.TransactionIdx,
				statedb,
				startState.BlockGasUsed,
				transactions,
				receipts,
				blockHashTree,
			)
			return proof.GetBlockFinalizationProof(its)
		}
	}

	// Prepare block and transaction context
	msg, txCtx, statedb, err := backend.StateAtTransaction(ctx, startState.Block, int(startState.TransactionIdx), reexec)
	if err != nil {
		return nil, err
	}
	chainCtx := createChainContext(backend, ctx)
	vmctx := core.NewEVMBlockContext(startState.Block.Header(), chainCtx, nil)
	receipts, err := backend.GetReceipts(ctx, startState.Block.Hash())
	if err != nil {
		return nil, err
	}
	blockHashTree, err := proofState.BlockHashTreeFromBlockContext(&vmctx)
	if err != nil {
		return nil, err
	}

	// Prepare the inter state before transaction for the prover
	its := proofState.InterStateFromCaptured(
		startState.Block.NumberU64(),
		startState.TransactionIdx,
		statedb,
		startState.BlockGasUsed,
		transactions,
		receipts,
		blockHashTree,
	)

	transaction := transactions[startState.TransactionIdx]

	if startState.StateType == proofState.InterStateType {
		// Type 2: transaction initiation or Type 3: EOA transfer transaction
		return proof.GetTransactionInitaitionProof(backend.ChainConfig(), &vmctx, transaction, &txCtx, its, statedb)
	}
	// Type 4: one-step EVM execution or Type 5: transaction finalization. Both require tracing.

	// Set up the prover
	prover := prover.NewProver(
		startState.VMHash,
		startState.StepIdx,
		backend.ChainConfig().Rules(vmctx.BlockNumber),
		startState.Block.NumberU64(),
		startState.TransactionIdx,
		statedb,
		*its,
		blockHashTree,
		transaction,
		receipts[startState.TransactionIdx],
	)
	// Run the transaction with prover enabled.
	vmenv := vm.NewEVM(txCtx, statedb, backend.ChainConfig(), vm.Config{Debug: true, Tracer: prover})
	// Call Prepare to clear out the statedb access list
	txHash := transactions[startState.TransactionIdx].Hash()
	statedb.Prepare(txHash, startState.Block.Hash(), int(startState.TransactionIdx))
	_, _, _, err = core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(msg.Gas()))
	if err != nil {
		return nil, fmt.Errorf("tracing failed: %w", err)
	}
	return prover.GetProof()
}

func generateTxCtx(backend Backend, ctx context.Context, block *types.Block, tx *types.Transaction) (*vm.Context, error) {
	signer := types.MakeSigner(backend.ChainConfig(), block.Number())
	msg, err := tx.AsMessage(signer)
	if err != nil {
		return nil, err
	}
	chainCtx := createChainContext(backend, ctx)
	blockCtx := core.NewEVMContext(msg, block.Header(), chainCtx, nil)
	return &blockCtx, nil
}

func generateStartBlockState(backend Backend, ctx context.Context, startBlock *types.Block, config *ProverConfig) (*proofState.BlockState, *state.StateDB, error) {
	reexec := defaultProveReexec
	if config != nil && config.Reexec != nil {
		reexec = *config.Reexec
	}
	// The statedb here is the state at the end of the parent block
	statedb, err := backend.StateAtBlock(ctx, startBlock, reexec, nil, true, false)
	if err != nil {
		return nil, nil, err
	}
	chainCtx := createChainContext(backend, ctx)

	parentBlockCtx := core.NewEVMBlockContext(startBlock.Header(), chainCtx, nil)
	blockHashTree, err := proofState.BlockHashTreeFromBlockContext(&parentBlockCtx)
	if err != nil {
		return nil, nil, err
	}
	bs, err := proofState.BlockStateFromBlock(parentBlockCtx.BlockNumber.Uint64(), statedb, blockHashTree)
	if err != nil {
		return nil, nil, err
	}
	return bs, statedb, nil
}

func transactionToMessage(tx types.Transaction, backend Backend, startBlockNum *big.Int) (core.Message, error) {
	signer := types.MakeSigner(backend.ChainConfig(), startBlockNum)
	msg, err := tx.AsMessage(signer)
	if err != nil {
		return nil, err
	}
	return msg, nil
}
