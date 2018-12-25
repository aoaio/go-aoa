package core

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"github.com/Aurorachain/go-Aurora/accounts"
	aa "github.com/Aurorachain/go-Aurora/accounts/walletType"
	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/hexutil"
	"github.com/Aurorachain/go-Aurora/consensus"
	"github.com/Aurorachain/go-Aurora/consensus/delegatestate"
	"github.com/Aurorachain/go-Aurora/core/state"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/core/vm"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/pkg/errors"
	"math/big"
	"strings"
	"sync"
	"time"
)

type Backend interface {
	AccountManager() *accounts.Manager
	BlockChain() *BlockChain
	TxPool() *TxPool
	ChainDb() aoadb.Database
	WatcherDb() aoadb.Database
}

type DposMiner struct {
	produceBlockCallBack      func(ctx context.Context)
	blockChan                 chan *types.Block
	mu                        sync.Mutex
	extra                     []byte
	aoa                       Backend
	config                    *params.ChainConfig
	current                   *worker
	engine                    consensus.Engine
	currentNewRoundHash       *types.ShuffleData
	shuffleHashChan           chan *types.ShuffleData
	delegateInfoMap           map[string]*ecdsa.PrivateKey
	AddDelegateWalletCallback func(data *aa.DelegateWalletInfo)
}

type worker struct {
	config     *params.ChainConfig
	state      *state.StateDB
	tcount     int
	txs        []*types.Transaction
	receipts   []*types.Receipt
	createdAt  time.Time
	signer     types.Signer
	header     *types.Header
	Block      *types.Block
	delegatedb *delegatestate.DelegateDB
	currentMu  sync.Mutex
}

func NewDposMiner(config *params.ChainConfig, aoa Backend, engine consensus.Engine) *DposMiner {

	dposMiner := &DposMiner{
		blockChan:       make(chan *types.Block),
		aoa:             aoa,
		config:          config,
		engine:          engine,
		shuffleHashChan: make(chan *types.ShuffleData),
		delegateInfoMap: make(map[string]*ecdsa.PrivateKey, 0),
	}

	addDelegateWalletCallback := func(data *aa.DelegateWalletInfo) {
		if data == nil {
			return
		}
		address := strings.ToLower(data.Address)
		if _, ok := dposMiner.delegateInfoMap[address]; !ok {
			log.Info("dposMiner add delegate privateKey", "address", address)
			dposMiner.delegateInfoMap[address] = data.PrivateKey
		}
	}
	dposMiner.AddDelegateWalletCallback = addDelegateWalletCallback
	produceBlockCallback := func(ctx context.Context) {
		value := ctx.Value(types.DelegatePrefix)
		candidate, ok := value.(types.ProduceDelegate)
		if !ok {
			log.Error("dposMiner| convert error,stop produce block")
			return
		}
		dposMiner.mu.Lock()
		defer dposMiner.mu.Unlock()

		tstamp := candidate.WorkTime
		blockTime := big.NewInt(int64(tstamp))
		parent := dposMiner.aoa.BlockChain().CurrentBlock()

		lastBlockNumber := parent.Number()
		gasLimit := CalcGasLimit(parent)

		if gasLimit > params.MaxGasLimit {
			gasLimit = params.MaxGasLimit
		}

		encodeBytes := hexutil.Encode([]byte(candidate.NickName))
		agentName := hexutil.MustDecode(encodeBytes)
		shuffleData := dposMiner.currentNewRoundHash
		log.Info("dpos|produceBlockCallback", "shuffleHash", shuffleData.ShuffleHash.Hex(), "shuffleBlockNumber", shuffleData.ShuffleBlockNumber)
		header := &types.Header{
			ParentHash:         parent.Hash(),
			Number:             lastBlockNumber.Add(lastBlockNumber, common.Big1),
			GasLimit:           gasLimit,
			Extra:              dposMiner.extra,
			Time:               blockTime,
			Coinbase:           common.HexToAddress(candidate.Address),
			AgentName:          agentName,
			ShuffleHash:        *shuffleData.ShuffleHash,
			ShuffleBlockNumber: shuffleData.ShuffleBlockNumber,
		}
		log.Info("dpos|produceBlockCallback", "blockNumber", header.Number.Uint64(), "blockGasLimit", gasLimit, "beginTime", tstamp, "currentTime", time.Now().Unix(), "coinbase", header.Coinbase.Hex())

		if err := engine.Prepare(dposMiner.aoa.BlockChain(), header); err != nil {
			log.Error("dpos|Failed to prepare header", "err", err)
			return
		}

		if err := dposMiner.makeCurrent(parent, header); err != nil {
			log.Error("Failed to create dpos produce context", "err", err)
			return
		}

		now := time.Now()
		pending, err := dposMiner.aoa.TxPool().PendingTxsByPrice()
		log.Info("PendingTxsByPrice end", "timestamp", time.Now().Sub(now))

		if err != nil {
			log.Error("Failed to fetch pending transactions", "err", err)
			return
		}
		work := dposMiner.current

		txs := types.NewTransactionsByPriceAndNonce2(work.signer, pending)
		no := time.Now()
		dposMiner.commitTransactions(txs, header.Coinbase)
		log.Info("commitTransactions end", "timestamp", time.Now().Sub(no), "whole Time", time.Now().Sub(now))
		if work.Block, err = engine.Finalize(dposMiner.aoa.BlockChain(), header, work.state, work.delegatedb, work.txs, work.receipts); err != nil {
			log.Error("Failed to finalize block for sealing", "err", err)
			return
		}

		if work.Block != nil {
			err := dposMiner.signBlockWithoutWallet(work.Block, header.Coinbase)
			if err != nil {
				log.Error("dpos|produceBlockCallback|fail", "err", err)
				return
			}
			log.Info("dpos|produceBlockCallback push block to chan", "blockNumber", work.Block.NumberU64(), "trxLen", work.Block.Transactions().Len())
			go func() {
				dposMiner.blockChan <- work.Block
			}()
		}
	}

	dposMiner.produceBlockCallBack = produceBlockCallback
	go dposMiner.readNewShufflehash()
	return dposMiner
}

func (d *DposMiner) signBlock(block *types.Block, coinbase common.Address) error {
	account := accounts.Account{Address: coinbase}
	wallet, err := d.aoa.AccountManager().Find(account)
	if err != nil {
		log.Error("Failed to find coinbase wallet", "coinbaseAddress", coinbase.Hex(), "err", err)
		return errors.New("sign error")
	}

	signature, err := wallet.SignHash(account, block.Hash().Bytes())
	if err != nil {
		log.Error("Failed to sign block", "coinbaseAddress", coinbase.Hex(), "err", err)
		return errors.New("sign error")
	}
	block.Signature = signature
	return nil
}

func (d *DposMiner) signBlockWithoutWallet(block *types.Block, coinbase common.Address) error {
	address := strings.ToLower(coinbase.Hex())
	if _, ok := d.delegateInfoMap[address]; !ok {
		errMsg := fmt.Sprintf("sign block fail because can not find pwd in memory address:%s lenMap:%d", coinbase.Hex(), len(d.delegateInfoMap))
		return errors.New(errMsg)
	}
	privateKey := d.delegateInfoMap[address]
	signature, err := crypto.Sign(block.Hash().Bytes()[:32], privateKey)
	if err != nil {
		log.Error("Failed to sign block", "coinbaseAddress", coinbase.Hex(), "err", err)
		return errors.New("sign error")
	}
	block.Signature = signature
	return nil
}

func (d *DposMiner) GetCurrentNewRoundHash() *types.ShuffleData {
	return d.currentNewRoundHash
}

func (d *DposMiner) GetShuffleHashChan() chan *types.ShuffleData {
	return d.shuffleHashChan
}

func (d *DposMiner) GetProduceBlockChan() chan *types.Block {
	return d.blockChan
}

func (d *DposMiner) GetDelegateWallets() map[string]*ecdsa.PrivateKey {
	return d.delegateInfoMap
}

func (d *DposMiner) readNewShufflehash() {
	for {
		select {
		case shufflehash := <-d.shuffleHashChan:
			d.currentNewRoundHash = shufflehash
		}

	}
}

func (d *DposMiner) makeCurrent(parent *types.Block, header *types.Header) error {
	statedb, err := d.aoa.BlockChain().StateAt(parent.Root())

	if err != nil {
		return err
	}
	delegatedb, err := d.aoa.BlockChain().DelegateStateAt(parent.DelegateRoot())

	if err != nil {
		log.Error("dposMiner|makeCurrent|delegatedb err", "err", err)
		return err
	}
	work := &worker{
		config:     d.config,
		state:      statedb,
		createdAt:  time.Now(),
		tcount:     0,
		signer:     types.NewAuroraSigner(d.config.ChainId),
		header:     header,
		delegatedb: delegatedb,
	}
	d.current = work
	return nil

}

func (d *DposMiner) Pending() (*types.Block, *state.StateDB) {
	work := d.current
	work.currentMu.Lock()
	defer work.currentMu.Unlock()
	return work.Block, work.state.Copy()
}

func (d *DposMiner) PendingBlock() *types.Block {
	return d.aoa.BlockChain().CurrentBlock()

}

func (d *DposMiner) GetProduceCallback() func(ctx context.Context) {
	return d.produceBlockCallBack
}

func (d *DposMiner) commitTransactions(txs *types.TransactionsByPriceAndNonce, coinbase common.Address) {
	env := d.current
	gp := new(GasPool).AddGas(env.header.GasLimit)
	contractGasLimit := new(GasPool).AddGas(params.MaxContractGasLimit)

	var coalescedLogs []*types.Log

	for {

		if gp.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "gp", gp)
			break
		}

		tx := txs.Peek()
		if tx == nil {
			break
		}

		var isContract bool
		contract := tx.GetIsContract()
		if contract == nil {
			isContract = IsContractTransaction(tx, env.state)
		} else {
			isContract = contract.(bool)
		}
		if isContract && contractGasLimit.Gas() < tx.Gas() {
			txs.Pop()
			continue
		}

		env.state.Prepare(tx.Hash(), common.Hash{}, env.tcount)

		err, gasUsed, logs := env.commitTransaction(tx, d.aoa.BlockChain(), coinbase, gp)
		if isContract {

			*(*uint64)(contractGasLimit) -= gasUsed - 20000

		}

		switch err {
		case ErrGasLimitReached:

			log.Trace("Gas limit exceeded for current block", "tx", tx.Hash())

			txs.Pop()

		case ErrNonceTooLow:

			log.Trace("Skipping transaction with low nonce", "tx", tx.Hash(), "nonce", tx.Nonce())

			txs.Shift()

		case ErrNonceTooHigh:

			log.Trace("Skipping account with hight nonce", "tx", tx.Hash(), "nonce", tx.Nonce())
			txs.Pop()

		case nil:

			coalescedLogs = append(coalescedLogs, logs...)
			env.tcount++
			txs.Shift()

		case ErrCancelAgent, ErrSubVote, ErrDuplicateRegisterAgent, ErrSubVoteNotEnough, ErrAddVote:
			log.Trace("Skipping transaction with error vote action", "tx", tx.Hash(), "nonce", tx.Nonce())
			txs.Shift()

		default:

			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			txs.Shift()

		}

	}

	if len(coalescedLogs) > 0 || env.tcount > 0 {

		cpy := make([]*types.Log, len(coalescedLogs))
		for i, l := range coalescedLogs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
	}
}

func (env *worker) commitTransaction(tx *types.Transaction, bc *BlockChain, coinbase common.Address, gp *GasPool) (error, uint64, []*types.Log) {
	snap := env.state.Snapshot()
	delegateSnap := env.delegatedb.Snapshot()

	receipt, gasUsed, err := ApplyTransaction(env.config, bc, &coinbase, gp, env.state, env.header, tx, &env.header.GasUsed, vm.Config{}, env.delegatedb, env.header.Time.Uint64(), false)

	if err != nil {
		env.state.RevertToSnapshot(snap, env.config.IsEpiphron(env.header.Number))
		env.delegatedb.RevertToSnapshot(delegateSnap)
		return err, gasUsed, nil
	}
	env.txs = append(env.txs, tx)
	env.receipts = append(env.receipts, receipt)
	return nil, gasUsed, receipt.Logs
}
