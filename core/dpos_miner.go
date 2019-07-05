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
	"context"
	"crypto/ecdsa"
	"fmt"
	"github.com/Aurorachain/go-aoa/accounts"
	aa "github.com/Aurorachain/go-aoa/accounts/walletType"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/common/hexutil"
	"github.com/Aurorachain/go-aoa/consensus"
	"github.com/Aurorachain/go-aoa/consensus/delegatestate"
	"github.com/Aurorachain/go-aoa/core/state"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/core/vm"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/params"
	"github.com/pkg/errors"
	"math/big"
	"strings"
	"sync"
	"time"
)

// Backend wraps all methods required for mining.
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
	extra                     []byte // maybe can be set by rpc
	aoa                       Backend
	config                    *params.ChainConfig
	current                   *worker
	engine                    consensus.Engine
	currentNewRoundHash       *types.ShuffleData // 本轮洗牌列表的Hex,洗牌的块高
	shuffleHashChan           chan *types.ShuffleData
	delegateInfoMap           map[string]*ecdsa.PrivateKey
	AddDelegateWalletCallback func(data *aa.DelegateWalletInfo)
}

type worker struct {
	config     *params.ChainConfig
	state      *state.StateDB // apply state changes here
	tcount     int            // tx count in cycle
	txs        []*types.Transaction
	receipts   []*types.Receipt
	createdAt  time.Time
	signer     types.Signer
	header     *types.Header
	Block      *types.Block // the new block
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
			log.Infof("dposMiner add delegate privateKey, address=%v", address)
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
		log.Infof("dpos|produceBlockCallback, shuffleHash=%s, shuffleBlockNumber=%d", shuffleData.ShuffleHash.Hex(), shuffleData.ShuffleBlockNumber)
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
		log.Infof("dpos|produceBlockCallback, blockNumber=%v, blockGasLimit=%v, beginTime=%v, currentTime=%v, coinbase=%v", header.Number.Uint64(), gasLimit, tstamp, time.Now().Unix(), header.Coinbase.Hex())

		if err := engine.Prepare(dposMiner.aoa.BlockChain(), header); err != nil {
			log.Error("dpos|Failed to prepare header", "err", err)
			return
		}

		if err := dposMiner.makeCurrent(parent, header); err != nil {
			log.Error("Failed to create dpos produce context, err=%s", err)
			return
		}

		//pending, err := dposMiner.aoa.TxPool().Pending()
		now := time.Now()
		pending, err := dposMiner.aoa.TxPool().PendingTxsByPrice()
		log.Infof("PendingTxsByPrice end, timestamp=%v", time.Now().Sub(now))

		if err != nil {
			log.Error("Failed to fetch pending transactions, err=%s", err)
			return
		}
		work := dposMiner.current

		txs := types.NewTransactionsByPriceAndNonce2(work.signer, pending)
		no := time.Now()
		dposMiner.commitTransactions(txs, header.Coinbase)
		log.Infof("commitTransactions end, timestamp=%v, whole Time=%s", time.Now().Sub(no), time.Now().Sub(now))
		if work.Block, err = engine.Finalize(dposMiner.aoa.BlockChain(), header, work.state, work.delegatedb, work.txs, work.receipts); err != nil {
			log.Error("Failed to finalize block for sealing, err=%s", err)
			return
		}

		if work.Block != nil {
			err := dposMiner.signBlockWithoutWallet(work.Block, header.Coinbase)
			if err != nil {
				log.Errorf("dpos|produceBlockCallback|fail, err=%s", err)
				return
			}
			log.Infof("dpos|produceBlockCallback push block to chan, blockNumber=%d, trxLen=%d", work.Block.NumberU64(), work.Block.Transactions().Len())
			go func() {
				dposMiner.blockChan <- work.Block
			}()
		}
	}

	dposMiner.produceBlockCallBack = produceBlockCallback
	go dposMiner.readNewShufflehash()
	return dposMiner
}

// sign block with coinbase,need to unlock wallet
func (d *DposMiner) signBlock(block *types.Block, coinbase common.Address) error {
	account := accounts.Account{Address: coinbase}
	wallet, err := d.aoa.AccountManager().Find(account)
	if err != nil {
		log.Error("Failed to find coinbase wallet, coinbaseAddress=%s, err=%s",coinbase.Hex(), err)
		return errors.New("sign error")
	}
	// b := sha3.Sum256(block.Hash().Bytes())
	signature, err := wallet.SignHash(account, block.Hash().Bytes())
	if err != nil {
		log.Error("Failed to sign block, coinbaseAddress=%s, err=%s", coinbase.Hex(), err)
		return errors.New("sign error")
	}
	block.Signature = signature
	return nil
}

// sign block with coinbase,pwd store in memory
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

// makeCurrent creates a new environment for the current cycle
func (d *DposMiner) makeCurrent(parent *types.Block, header *types.Header) error {
	statedb, err := d.aoa.BlockChain().StateAt(parent.Root())

	if err != nil {
		return err
	}
	delegatedb, err := d.aoa.BlockChain().DelegateStateAt(parent.DelegateRoot())

	if err != nil {
		log.Error("dposMiner|makeCurrent|delegatedb err, err=%s", err)
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
	//work := d.current
	//work.currentMu.Lock()
	//defer work.currentMu.Unlock()
	//return work.Block
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
		// If we don't have enough gas for any further transactions then we're done
		if gp.Gas() < params.TxGas {
			log.Debugf("Not enough gas for further transactions, gp=%v", gp)
			break
		}
		// Retrieve the next transaction and abort if all done
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

		// Error may be ignored here. The error has already been checked
		// during transaction acceptance is the transaction pool.
		//
		// We use the eip155 signer regardless of the current hf.
		//from, _ := types.Sender(env.signer, tx)
		// Check whether the tx is replay protected. If we're not in the EIP155 hf
		// phase, start ignoring the sender until we do.
		//if tx.Protected() {
		//	log.Debug("Ignoring reply protected transaction", "hash", tx.Hash())
		//	txs.Pop()
		//	continue
		//}
		// Start executing the transaction
		env.state.Prepare(tx.Hash(), common.Hash{}, env.tcount)

		//now := time.Now()
		err, gasUsed, logs := env.commitTransaction(tx, d.aoa.BlockChain(), coinbase, gp)
		if isContract {
			//now = time.Now()
			*(*uint64)(contractGasLimit) -= gasUsed - 20000
			//gas := math.Exp(0.03*float64(gasUsed)/10000.0+0.1) * float64(gasUsed)
			//if *(*uint64)(contractGasLimit) < uint64(gas) {
			//	*(*uint64)(contractGasLimit) = 0
			//} else {
			//	*(*uint64)(contractGasLimit) -= uint64(gas)
			//}
		}
		switch err {
		case ErrGasLimitReached:
			// Pop the current out-of-gas transaction without shifting in the next from the account

			//log.Debug("Gas limit exceeded for current block", "sender", from)

			log.Debugf("Gas limit exceeded for current block, hash=%v", tx.Hash().Hex())

			txs.Pop()

		case ErrNonceTooLow:
			// New head notification data race between the transaction pool and miner, shift

			//log.Debug("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())

			log.Debugf("Skipping transaction with low nonce, tx=%v, nonce=%v", tx.Hash().Hex(), tx.Nonce())

			txs.Shift()

		case ErrNonceTooHigh:
			// Reorg notification data race between the transaction pool and miner, skip account =

			//log.Debug("Skipping account with hight nonce", "sender", from, "nonce", tx.Nonce())

			log.Debugf("Skipping account with hight nonce, tx=%v, nonce=%v", tx.Hash(), tx.Nonce())
			txs.Pop()

		case nil:
			// Everything ok, collect the logs and shift in the next transaction from the same account
			coalescedLogs = append(coalescedLogs, logs...)
			env.tcount++
			txs.Shift()

		case ErrCancelAgent, ErrSubVote, ErrDuplicateRegisterAgent, ErrSubVoteNotEnough, ErrAddVote:
			log.Debugf("Skipping transaction with error vote action, tx=%v, nonce=%v", tx.Hash().Hex(), tx.Nonce())
			txs.Shift()

		default:

			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Infof("Transaction failed, account skipped, hash=%v, err=%v", tx.Hash(), err)
			txs.Shift()

		}

	}

	if len(coalescedLogs) > 0 || env.tcount > 0 {
		// make a copy, the state caches the logs and these logs get "upgraded" from pending to mined
		// logs by filling in the block hash when the block was mined by the local miner. This can
		// cause a race condition if a log was "upgraded" before the PendingLogsEvent is processed.
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
	//now := time.Now()
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
