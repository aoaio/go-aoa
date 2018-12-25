package core

import (
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/consensus"

	"github.com/Aurorachain/go-Aurora/consensus/delegatestate"
	"github.com/Aurorachain/go-Aurora/core/state"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/core/vm"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/params"
	"math/big"
	"github.com/Aurorachain/go-Aurora/log"
	"strings"
)

type StateProcessor struct {
	config *params.ChainConfig 
	bc     *BlockChain         
	engine consensus.Engine    
}

func NewStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *StateProcessor {
	return &StateProcessor{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config, db *delegatestate.DelegateDB) (types.Receipts, []*types.Log, uint64, error) {

	var (
		receipts types.Receipts
		usedGas  = new(uint64)
		header   = block.Header()
		allLogs  []*types.Log
		gp       = new(GasPool).AddGas(block.GasLimit())
	)

	for i, tx := range block.Transactions() {
		statedb.Prepare(tx.Hash(), block.Hash(), i)
		db.Prepare(tx.Hash(), block.Hash(), i)
		receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, statedb, header, tx, usedGas, cfg, db, block.Time().Uint64(),true)
		if err != nil {
			return nil, nil, 0, err
		}

		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}

	p.engine.Finalize(p.bc, header, statedb, db, block.Transactions(), receipts)

	return receipts, allLogs, *usedGas, nil
}

func ApplyTransaction(config *params.ChainConfig, bc *BlockChain, author *common.Address, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config, db *delegatestate.DelegateDB, blockTime uint64, watchInnerTx bool) (*types.Receipt, uint64, error) {
	msg, err := tx.AsMessage(types.MakeSigner(config, header.Number))
	if err != nil {
		return nil, 0, err
	}

	context := NewEVMContext(msg, header, bc, author)

	vmenv := vm.NewEVM(context, statedb, config, cfg)
	vmenv.WatchInnerTx = watchInnerTx

	_, gas, failed, err := ApplyMessage(vmenv, msg, gp)
	if err != nil {
		return nil, 0, err
	}
	err = voteChangeToDelegateState(msg.From(), tx, statedb, db, blockTime, header.Number.Int64())

	if err != nil {
		return nil, 0, err
	}

	var root []byte

	if config.IsAres(header.Number) {
		statedb.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(false).Bytes()
	}
	*usedGas += gas

	receipt := types.NewReceipt(root, failed, *usedGas)
	receipt.Action = tx.TxDataAction()
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = gas

	if msg.Action() == types.ActionCreateContract || msg.Action() == types.ActionPublishAsset {
		receipt.ContractAddress = crypto.CreateAddress(vmenv.Context.Origin, tx.Nonce())
	}

	receipt.Logs = statedb.GetLogs(tx.Hash())
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})

	if len(vmenv.InnerTxs) > 0 {
		itxerr := bc.innerTxDb.Set(tx.Hash(),vmenv.InnerTxs)
		if nil!=itxerr {
			log.Warn("save inner transactions error","err",itxerr)
		}
	}

	return receipt, gas, err
}

func voteChangeToDelegateState(from common.Address, tx *types.Transaction, statedb *state.StateDB, db *delegatestate.DelegateDB, blockTime uint64, blockNumber int64) error {

	address := strings.ToLower(from.Hex())
	candidates, err := CountTrxVote(address, tx, statedb, db)

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

	log.Info("voteChangeToDelegateState|end", "blockNumber", blockNumber)
	return err
}

func voteCount(db *delegatestate.DelegateDB, v types.VoteCandidate, blockTime uint64) error {
	address := common.HexToAddress(v.Address)
	switch v.Action {
	case register:
		if db.Exist(address) {

			return ErrDuplicateRegisterAgent
		}
		db.GetOrNewStateObject(address, v.Nickname, blockTime)
	case addVote:
		if !db.Exist(address) {

			return ErrAddVote
		}
		db.AddVote(address, big.NewInt(int64(v.Vote)))
	case subVote:
		if !db.SubExist(address) {

			return ErrSubVote
		}
		vote := db.GetVote(address)
		subVoteNumber := big.NewInt(int64(v.Vote))
		if vote.Int64() < subVoteNumber.Int64() {

			return ErrSubVoteNotEnough
		}
		db.SubVote(address, subVoteNumber)
	case cancel:
		if !db.Exist(address) {

			return ErrCancelAgent
		}
		db.Suicide(address)
	}
	return nil
}
