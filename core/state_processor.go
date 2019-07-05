// Copyright 2018 The go-aurora Authors
// This file is part of the go-aurora library.
//
// The go-aurora library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-aurora library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-aurora library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/consensus"
	"github.com/Aurorachain/go-aoa/consensus/delegatestate"
	"github.com/Aurorachain/go-aoa/core/state"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/core/vm"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/params"
	"math/big"
	"strings"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *StateProcessor {
	return &StateProcessor{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Process processes the state changes according to the Aurora rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config, db *delegatestate.DelegateDB) (types.Receipts, []*types.Log, uint64, error) {
	// TODO ADD vote changes to delegate state
	var (
		receipts types.Receipts
		usedGas  = new(uint64)
		header   = block.Header()
		allLogs  []*types.Log
		gp       = new(GasPool).AddGas(block.GasLimit())
	)
	//// Mutate the the block and state according to any hard-fork specs
	//if p.config.DAOForkSupport && p.config.DAOForkBlock != nil && p.config.DAOForkBlock.Cmp(block.Number()) == 0 {
	//	misc.ApplyDAOHardFork(statedb)
	//}
	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		statedb.Prepare(tx.Hash(), block.Hash(), i)
		db.Prepare(tx.Hash(), block.Hash(), i)
		receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, statedb, header, tx, usedGas, cfg, db, block.Time().Uint64(), true)
		if err != nil {
			return nil, nil, 0, err
		}
		// AOA upgrade start by rt
		monitorUpgrade(*tx, receipt)
		// AOA upgrade end by rt

		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.engine.Finalize(p.bc, header, statedb, db, block.Transactions(), receipts)

	return receipts, allLogs, *usedGas, nil
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(config *params.ChainConfig, bc *BlockChain, author *common.Address, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config, db *delegatestate.DelegateDB, blockTime uint64, watchInnerTx bool) (*types.Receipt, uint64, error) {
	msg, err := tx.AsMessage(types.MakeSigner(config, header.Number))
	if err != nil {
		return nil, 0, err
	}
	// Create a new context to be used in the EVM environment
	context := NewEVMContext(msg, header, bc, author)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	vmenv := vm.NewEVM(context, statedb, config, cfg)
	vmenv.WatchInnerTx = watchInnerTx
	// Apply the transaction to the current state (included in the env)
	_, gas, failed, err := ApplyMessage(vmenv, msg, gp)
	if err != nil {
		return nil, 0, err
	}
	err = voteChangeToDelegateState(msg.From(), tx, statedb, db, blockTime, header.Number.Int64())
	// log.Info("applyTransaction|voteChangeToDelegateState cost", "timestamp", time.Now().Sub(now4))
	if err != nil {
		return nil, 0, err
	}
	// Update the state with pending changes
	var root []byte
	//从AresBlock块开始，不用root
	if config.IsAres(header.Number) {
		statedb.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(false).Bytes()
	}
	*usedGas += gas

	// Create a new receipt for the transaction, storing the gas used by the tx.
	receipt := types.NewReceipt(root, failed, *usedGas)
	receipt.Action = tx.TxDataAction()
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = gas
	// if the transaction created a contract, store the creation address in the receipt.
	// 基于交易结构体增加Action属性的扩展进行判断
	if msg.Action() == types.ActionCreateContract || msg.Action() == types.ActionPublishAsset {
		receipt.ContractAddress = crypto.CreateAddress(vmenv.Context.Origin, tx.Nonce())
	}
	// Set the receipt logs and create a bloom for filtering
	receipt.Logs = statedb.GetLogs(tx.Hash())
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
	//now4 := time.Now()

	//inner transactions
	if len(vmenv.InnerTxs) > 0 {
		itxerr := bc.innerTxDb.Set(tx.Hash(), vmenv.InnerTxs)
		if nil != itxerr {
			log.Warn("save inner transactions error", "err", itxerr)
		}
	}

	return receipt, gas, err
}

// trx vote change to delegate state
func voteChangeToDelegateState(from common.Address, tx *types.Transaction, statedb *state.StateDB, db *delegatestate.DelegateDB, blockTime uint64, blockNumber int64) error {
	// beginDelegateRoot := db.IntermediateRoot(false)
	address := strings.ToLower(from.Hex())
	candidates, err := CountTrxVote(address, tx, statedb, db)

	//if len(candidates) > 0 {
	//	log.Info("voteChangeToDelegateState|", "blockNumber", blockNumber, "beginDelegateRoot", len(candidates), "err", err)
	//	for _, v := range candidates {
	//		log.Info("voteChangeToDelegateState|foreach", "candidate", v)
	//	}
	//}
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	for _, v := range candidates {
		err := voteCount(db, v, blockTime)
		if err != nil {
			log.Error("voteChangeToDelegateState|fail", "err", err)
			return err
		}
	}
	//db.Finalise(false)
	//endDelegateRoot := db.IntermediateRoot(false)
	log.Info("voteChangeToDelegateState|end", "blockNumber", blockNumber)
	return err
}

func voteCount(db *delegatestate.DelegateDB, v types.VoteCandidate, blockTime uint64) error {
	address := common.HexToAddress(v.Address)
	switch v.Action {
	case register:
		if db.Exist(address) {
			// vote := db.GetVote(address)
			// return errors.New(fmt.Sprintf("duplicate register vote address:%s vote:%d", address.Hex(), vote))
			return ErrDuplicateRegisterAgent
		}
		db.GetOrNewStateObject(address, v.Nickname, blockTime)
	case addVote:
		if !db.Exist(address) {
			// return errors.New(fmt.Sprintf("delegate not exist when add vote address:%s", address.Hex()))
			return ErrAddVote
		}
		db.AddVote(address, big.NewInt(int64(v.Vote)))
	case subVote:
		if !db.SubExist(address) {
			// return errors.New(fmt.Sprintf("delegate not exist when sub vote address:%s", address.Hex()))
			return ErrSubVote
		}
		vote := db.GetVote(address)
		subVoteNumber := big.NewInt(int64(v.Vote))
		if vote.Int64() < subVoteNumber.Int64() {
			// return errors.New(fmt.Sprintf("delegate sub error,vote not enough address:%s subVote:%d currentVote:%d", address.Hex(), subVoteNumber.Int64(), vote.Int64()))
			return ErrSubVoteNotEnough
		}
		db.SubVote(address, subVoteNumber)
	case cancel:
		if !db.Exist(address) {
			// return errors.New(fmt.Sprintf("delegate not exist when cancel address:%s", address.Hex()))
			return ErrCancelAgent
		}
		db.Suicide(address)
	}
	return nil
}
