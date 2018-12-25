package core

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"sync"
	"time"

	"container/heap"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/consensus/delegatestate"
	"github.com/Aurorachain/go-Aurora/core/state"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/event"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/metrics"
	"github.com/Aurorachain/go-Aurora/params"
	"gopkg.in/karalabe/cookiejar.v2/collections/prque"
	"strings"
)

const (

	chainHeadChanSize = 10

	rmTxChanSize   = 10
	maxTrxType     = 1000
	maxContractNum = 5000
)

var (

	ErrInvalidSender = errors.New("invalid sender")

	ErrFullPending = errors.New("transaction pool is full")

	ErrVoteList = errors.New("Vote member not in delegate poll.")

	ErrNonceTooLow = errors.New("nonce too low")

	ErrNickName = errors.New("Deligate nickname error")

	ErrRegister = errors.New("already register delegate.")

	ErrUnderpriced = errors.New("transaction underpriced")

	ErrReplaceUnderpriced = errors.New("replacement transaction underpriced")

	ErrInsufficientFunds = errors.New("insufficient funds of AOA")

	ErrInsufficientAssetFunds = errors.New("insufficient funds of asset for transfer")

	ErrIntrinsicGas = errors.New("intrinsic gas too low")

	ErrGasLimit = errors.New("exceeds block gas limit")

	ErrNegativeValue = errors.New("negative value")

	ErrOversizedData = errors.New("oversized data")
)

var (
	evictionInterval    = time.Minute
	statsReportInterval = 8 * time.Second
)

var (

	pendingDiscardCounter   = metrics.NewCounter("txpool/pending/discard")
	pendingReplaceCounter   = metrics.NewCounter("txpool/pending/replace")
	pendingRateLimitCounter = metrics.NewCounter("txpool/pending/ratelimit")
	pendingNofundsCounter   = metrics.NewCounter("txpool/pending/nofunds")

	queuedDiscardCounter   = metrics.NewCounter("txpool/queued/discard")
	queuedReplaceCounter   = metrics.NewCounter("txpool/queued/replace")
	queuedRateLimitCounter = metrics.NewCounter("txpool/queued/ratelimit")
	queuedNofundsCounter   = metrics.NewCounter("txpool/queued/nofunds")

	invalidTxCounter     = metrics.NewCounter("txpool/invalid")
	underpricedTxCounter = metrics.NewCounter("txpool/underpriced")

)

type TxStatus uint

const (
	TxStatusUnknown  TxStatus = iota
	TxStatusQueued
	TxStatusPending
	TxStatusIncluded
)

type blockChain interface {
	CurrentBlock() *types.Block
	GetBlock(hash common.Hash, number uint64) *types.Block
	StateAt(root common.Hash) (*state.StateDB, error)
	DelegateStateAt(root common.Hash) (*delegatestate.DelegateDB, error)
	GetDelegatePoll() (*map[common.Address]types.Candidate, error)

	SubscribeChainHeadEvent(ch chan<- ChainHeadEvent) event.Subscription
}

type TxPoolConfig struct {
	NoLocals  bool
	Journal   string
	Rejournal time.Duration

	PriceLimit uint64
	PriceBump  uint64

	AccountSlots uint64
	GlobalSlots  uint64
	AccountQueue uint64
	GlobalQueue  uint64

	Lifetime time.Duration
}

var DefaultTxPoolConfig = TxPoolConfig{
	Journal:   "transactions.rlp",
	Rejournal: time.Hour,

	PriceLimit: 1,
	PriceBump:  10,

	AccountSlots: 256,
	GlobalSlots:  20000,
	AccountQueue: 1024,
	GlobalQueue:  10000,

	Lifetime: 30 * time.Minute,
}

func (config *TxPoolConfig) sanitize() TxPoolConfig {
	conf := *config
	if conf.Rejournal < time.Second {
		log.Warn("Sanitizing invalid txpool journal time", "provided", conf.Rejournal, "updated", time.Second)
		conf.Rejournal = time.Second
	}
	if conf.PriceLimit < 1 {
		log.Warn("Sanitizing invalid txpool price limit", "provided", conf.PriceLimit, "updated", DefaultTxPoolConfig.PriceLimit)
		conf.PriceLimit = DefaultTxPoolConfig.PriceLimit
	}
	if conf.PriceBump < 1 {
		log.Warn("Sanitizing invalid txpool price bump", "provided", conf.PriceBump, "updated", DefaultTxPoolConfig.PriceBump)
		conf.PriceBump = DefaultTxPoolConfig.PriceBump
	}
	return conf
}

type TxPool struct {
	config       TxPoolConfig
	chainconfig  *params.ChainConfig
	chain        blockChain
	gasPrice     *big.Int
	txFeed       event.Feed
	scope        event.SubscriptionScope
	chainHeadCh  chan ChainHeadEvent
	chainHeadSub event.Subscription
	signer       types.Signer
	mu           sync.RWMutex

	currentState  *state.StateDB
	pendingState  *state.ManagedState
	currentMaxGas uint64

	locals  *accountSet
	journal *txJournal

	pending map[common.Address]*txList
	queue   map[common.Address]*txList
	beats   map[common.Address]time.Time
	all     map[common.Hash]*types.Transaction
	priced  *txTypeList

	wg sync.WaitGroup

	homestead bool
}

var maxElectDelegate int64

func NewTxPool(config TxPoolConfig, chainconfig *params.ChainConfig, chain blockChain) *TxPool {

	config = (&config).sanitize()
	maxElectDelegate = chainconfig.MaxElectDelegate.Int64()

	pool := &TxPool{
		config:      config,
		chainconfig: chainconfig,
		chain:       chain,
		signer:      types.NewAuroraSigner(chainconfig.ChainId),
		pending:     make(map[common.Address]*txList),
		queue:       make(map[common.Address]*txList),
		beats:       make(map[common.Address]time.Time),
		all:         make(map[common.Hash]*types.Transaction),
		chainHeadCh: make(chan ChainHeadEvent, chainHeadChanSize),
		gasPrice:    new(big.Int).SetUint64(config.PriceLimit),
	}
	pool.locals = newAccountSet(pool.signer)
	pool.priced = newTxTypeList(&pool.all)
	pool.reset(nil, chain.CurrentBlock().Header())

	if !config.NoLocals && config.Journal != "" {
		pool.journal = newTxJournal(config.Journal)

		if err := pool.journal.load(pool.AddLocal); err != nil {
			log.Warn("Failed to load transaction journal", "err", err)
		}
		if err := pool.journal.rotate(pool.local()); err != nil {
			log.Warn("Failed to rotate transaction journal", "err", err)
		}
	}

	pool.chainHeadSub = pool.chain.SubscribeChainHeadEvent(pool.chainHeadCh)

	pool.wg.Add(1)
	go pool.loop()

	return pool
}

func (pool *TxPool) loop() {
	defer pool.wg.Done()

	var prevPending, prevQueued int

	report := time.NewTicker(statsReportInterval)
	defer report.Stop()

	evict := time.NewTicker(evictionInterval)
	defer evict.Stop()

	journal := time.NewTicker(pool.config.Rejournal)
	defer journal.Stop()

	head := pool.chain.CurrentBlock()

	for {
		select {

		case ev := <-pool.chainHeadCh:
			if ev.Block != nil {
				pool.mu.Lock()
				pool.reset(head.Header(), ev.Block.Header())
				head = ev.Block

				pool.mu.Unlock()
			}

		case <-pool.chainHeadSub.Err():
			return

		case <-report.C:
			pool.mu.RLock()
			pending, queued := pool.stats()

			pool.mu.RUnlock()

			if pending != prevPending || queued != prevQueued {
				log.Debug("Transaction pool status report", "executable", pending, "queued", queued)
				prevPending, prevQueued = pending, queued
			}

		case <-evict.C:
			pool.mu.Lock()

			for addr := range pool.pending {

				if time.Since(pool.beats[addr]) > pool.config.Lifetime {
					for _, tx := range pool.pending[addr].Flatten() {
						pool.removeTx(tx.Hash())
					}
				}
			}
			for addr := range pool.queue {
				if time.Since(pool.beats[addr]) > pool.config.Lifetime {
					for _, tx := range pool.queue[addr].Flatten() {
						pool.removeTx(tx.Hash())
					}
				}
			}
			if pool.priced.items.TxNum() != len(*pool.priced.all) {
				itemTxList := pool.priced.items.GetTransactionList()
				for _, tx := range itemTxList {
					if _, ok := pool.all[tx.Hash()]; !ok {
						pool.priced.RemoveByHash(tx.GetTransactionType(), tx.Hash())
					}
				}
			}
			pool.mu.Unlock()

		case <-journal.C:
			if pool.journal != nil {
				pool.mu.Lock()
				if err := pool.journal.rotate(pool.local()); err != nil {
					log.Warn("Failed to rotate local tx journal", "err", err)
				}
				pool.mu.Unlock()
			}
		}
	}
}

func (pool *TxPool) lockedReset(oldHead, newHead *types.Header) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.reset(oldHead, newHead)
}

func (pool *TxPool) reset(oldHead, newHead *types.Header) {

	var reinject types.Transactions

	if oldHead != nil && oldHead.Hash() != newHead.ParentHash {

		oldNum := oldHead.Number.Uint64()
		newNum := newHead.Number.Uint64()

		if depth := uint64(math.Abs(float64(oldNum) - float64(newNum))); depth > 64 {
			log.Debug("Skipping deep transaction reorg", "depth", depth)
		} else {

			var discarded, included types.Transactions

			var (
				rem = pool.chain.GetBlock(oldHead.Hash(), oldHead.Number.Uint64())
				add = pool.chain.GetBlock(newHead.Hash(), newHead.Number.Uint64())
			)
			for rem.NumberU64() > add.NumberU64() {
				discarded = append(discarded, rem.Transactions()...)
				if rem = pool.chain.GetBlock(rem.ParentHash(), rem.NumberU64()-1); rem == nil {
					log.Error("Unrooted old chain seen by tx pool", "block", oldHead.Number, "hash", oldHead.Hash())
					return
				}
			}
			for add.NumberU64() > rem.NumberU64() {
				included = append(included, add.Transactions()...)
				if add = pool.chain.GetBlock(add.ParentHash(), add.NumberU64()-1); add == nil {
					log.Error("Unrooted new chain seen by tx pool", "block", newHead.Number, "hash", newHead.Hash())
					return
				}
			}
			for rem.Hash() != add.Hash() {
				discarded = append(discarded, rem.Transactions()...)
				if rem = pool.chain.GetBlock(rem.ParentHash(), rem.NumberU64()-1); rem == nil {
					log.Error("Unrooted old chain seen by tx pool", "block", oldHead.Number, "hash", oldHead.Hash())
					return
				}
				included = append(included, add.Transactions()...)
				if add = pool.chain.GetBlock(add.ParentHash(), add.NumberU64()-1); add == nil {
					log.Error("Unrooted new chain seen by tx pool", "block", newHead.Number, "hash", newHead.Hash())
					return
				}
			}
			reinject = types.TxDifference(discarded, included)
		}
	}

	if newHead == nil {
		newHead = pool.chain.CurrentBlock().Header()
	}
	statedb, err := pool.chain.StateAt(newHead.Root)
	if err != nil {
		log.Error("Failed to reset txpool state", "err", err)
		return
	}
	pool.currentState = statedb
	pool.pendingState = state.ManageState(statedb)
	pool.currentMaxGas = newHead.GasLimit

	log.Debug("Reinjecting stale transactions", "count", len(reinject))
	pool.addTxsLocked(reinject, false)

	pool.demoteUnexecutables()

	for addr, list := range pool.pending {
		txs := list.Flatten()
		pool.pendingState.SetNonce(addr, txs[len(txs)-1].Nonce()+1)
	}

	pool.promoteExecutables(nil)
}

func (pool *TxPool) Stop() {

	pool.scope.Close()

	pool.chainHeadSub.Unsubscribe()
	pool.wg.Wait()

	if pool.journal != nil {
		pool.journal.close()
	}
	log.Info("Transaction pool stopped")
}

func (pool *TxPool) SubscribeTxPreEvent(ch chan<- TxPreEvent) event.Subscription {
	return pool.scope.Track(pool.txFeed.Subscribe(ch))
}

func (pool *TxPool) GasPrice() *big.Int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return new(big.Int).Set(pool.gasPrice)
}

func (pool *TxPool) SetGasPrice(price *big.Int) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.gasPrice = price
	for _, tx := range pool.priced.Cap(price, pool.locals, pool.currentState) {
		pool.removeTx(tx.Hash())
	}
	log.Info("Transaction pool price threshold updated", "price", price)
}

func (pool *TxPool) State() *state.ManagedState {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return pool.pendingState
}

func (pool *TxPool) Stats() (int, int) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return pool.stats()
}

func (pool *TxPool) stats() (int, int) {
	pending := 0
	for _, list := range pool.pending {
		pending += list.Len()
	}
	queued := 0
	for _, list := range pool.queue {
		queued += list.Len()
	}
	return pending, queued
}

func (pool *TxPool) txKindNum() (map[string]int) {
	res := make(map[string]int)
	for _, kv := range *pool.priced.items {
		res[kv.GetKey()] = kv.GetValueLen()
	}
	return res
}

func (pool *TxPool) TxKindNum() (map[string]int) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return pool.txKindNum()
}

func (pool *TxPool) Content() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pending := make(map[common.Address]types.Transactions)
	for addr, list := range pool.pending {
		pending[addr] = list.Flatten()
	}
	queued := make(map[common.Address]types.Transactions)
	for addr, list := range pool.queue {
		queued[addr] = list.Flatten()
	}
	return pending, queued
}

func (pool *TxPool) Pending() (map[common.Address]types.Transactions, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pending := make(map[common.Address]types.Transactions)
	for addr, list := range pool.pending {
		pending[addr] = list.Flatten()
	}
	return pending, nil
}

func (pool *TxPool) PendingTxsByPrice() (types.TxByPrice, error) {

	pool.mu.Lock()
	defer pool.mu.Unlock()

	var wg sync.WaitGroup

	wholeTxNum := len(pool.all)
	estimateTxNum := pool.currentMaxGas / params.TxGas
	price := make(types.TxByPrice, 0, estimateTxNum)
	contractNum := pool.priced.items.ContractNum()

	wg.Add(len(*pool.priced.items))
	for index, txTypeList := range *pool.priced.items {
		txGasList := *txTypeList.value
		txTypeWholeNum := int64(txGasList.Len())

		if txTypeWholeNum == 0 {
			pool.priced.items.Remove(index)
			wg.Done()
			continue
		}
		conNum := uint64(contractNum)
		if conNum > maxContractNum {
			conNum = maxContractNum
		}
		es := uint64(int64(-1.8*float64(conNum)) + 10000)
		wholeTrxNum := wholeTxNum
		if txTypeList.contract {
			es = conNum
			wholeTrxNum = contractNum
		} else {
			wholeTrxNum = wholeTxNum - contractNum
		}

		go func(txGasList types.TxByPrice, txLen int64, wholeTxNum int, estimateTxNum uint64, kv *KV) {
			defer wg.Done()

			txsLen := new(big.Float).SetInt64(txLen)
			wTxNum := new(big.Float).SetFloat64(float64(1) / float64(wholeTxNum))
			esTxNum := new(big.Float).SetInt64(int64(estimateTxNum))
			txSuggestNum := big.NewFloat(0).Mul(txsLen.Mul(txsLen, wTxNum), esTxNum)
			txTypeSuggestNum, _ := txSuggestNum.Int64()

			if txTypeSuggestNum == 0 {
				txTypeSuggestNum = 1
			}
			sortTxs := types.SortByPriceAndNonce(pool.signer, txGasList)
			if txTypeSuggestNum > int64(len(sortTxs)) {
				txTypeSuggestNum = int64(len(sortTxs))
			}

			price = append(price, sortTxs[:txTypeSuggestNum]...)
		}(txGasList, txTypeWholeNum, wholeTrxNum, es, txTypeList)
	}
	wg.Wait()

	heap.Init(&price)

	return price, nil
}

func (pool *TxPool) PoolSigner() types.Signer {
	return pool.signer
}

func (pool *TxPool) local() map[common.Address]types.Transactions {
	txs := make(map[common.Address]types.Transactions)
	for addr := range pool.locals.accounts {
		if pending := pool.pending[addr]; pending != nil {
			txs[addr] = append(txs[addr], pending.Flatten()...)
		}
		if queued := pool.queue[addr]; queued != nil {
			txs[addr] = append(txs[addr], queued.Flatten()...)
		}
	}
	return txs
}

func (pool *TxPool) validateTx(tx *types.Transaction, local bool) error {

	if tx.TxDataAction() > types.ActionCallContract {
		return fmt.Errorf("Illegal action: %d", tx.TxDataAction())
	}

	if tx.Size() > 32*1024 {
		return ErrOversizedData
	}

	if tx.Value().Sign() < 0 {
		return ErrNegativeValue
	}

	if pool.currentMaxGas < tx.Gas() {
		return ErrGasLimit
	}
	if params.MaxOneContractGasLimit < tx.Gas() {
		return errors.New("Gas over Limit!")
	}

	from, err := types.Sender(pool.signer, tx)
	if err != nil {
		return ErrInvalidSender
	}

	local = local || pool.locals.contains(from)
	if !local && pool.gasPrice.Cmp(tx.GasPrice()) > 0 {
		return ErrUnderpriced
	}

	if pool.currentState.GetNonce(from) > tx.Nonce() {
		return ErrNonceTooLow
	}

	cost := tx.AoaCost()
	delegates, err := pool.chain.GetDelegatePoll()
	if err != nil {
		return err
	}
	delegateList := *delegates
	switch tx.TxDataAction() {
	case types.ActionRegister:
		if string(tx.Nickname()) == "" {
			return ErrNickName
		}
		if _, ok := delegateList[from]; ok {
			return ErrRegister
		}
	case types.ActionAddVote, types.ActionSubVote:
		var voteCost *big.Int
		if tx.TxDataAction() == types.ActionAddVote {
			voteCost = tx.Value()
		} else {
			voteCost = big.NewInt(-tx.Value().Int64())
		}
		votes, err := types.BytesToVote(tx.Vote())
		if err != nil {
			return err
		}
		if len(votes) == 0 {
			return errors.New("empty vote list")
		}
		err = validateVote(pool.currentState.GetVoteList(from), votes, delegateList)
		if err != nil {
			return err
		}
		lockBalance := pool.currentState.GetLockBalance(from)

		if voteCost.Add(voteCost, lockBalance).Cmp(new(big.Int).Mul(big.NewInt(params.Aoa), big.NewInt(maxElectDelegate))) > 0 {
			return errors.New(fmt.Sprintf("vote exceeds %d delegate", maxElectDelegate))
		}
	case types.ActionPublishAsset:
		err = pool.currentState.ValidateAsset(*tx.AssetInfo())
		if err != nil {
			log.Error("ValidateAsset failed.", "err", err)
			return err
		}
	case types.ActionCreateContract:
		if len(tx.Data()) == 0 {
			return errors.New("Create contract but data is nil")
		}
	}
	a := tx.Asset()
	if a != nil && (*a != common.Address{}) {
		if pool.currentState.GetAssetBalance(from, *a).Cmp(tx.Value()) < 0 {
			return ErrInsufficientAssetFunds
		}
	}
	if pool.currentState.GetBalance(from).Cmp(cost) < 0 {
		return ErrInsufficientFunds
	}
	intrGas, err := IntrinsicGas(tx.Data(), tx.TxDataAction())
	if err != nil {
		return err
	}
	if tx.Gas() < intrGas {
		return ErrIntrinsicGas
	}

	return nil
}

func validateVote(prevVoteList []common.Address, curVoteList []types.Vote, delegateList map[common.Address]types.Candidate) error {
	voteChange := prevVoteList
	for _, vote := range curVoteList {
		switch vote.Operation {
		case 0:
			if _, contain := sliceContains(*vote.Candidate, voteChange); !contain {
				if _, ok := delegateList[*vote.Candidate]; !ok {
					return ErrVoteList
				}
				voteChange = append(voteChange, *vote.Candidate)
			} else {
				return errors.New("You have already vote candidate " + strings.ToLower(vote.Candidate.Hex()))
			}
		case 1:
			if i, contain := sliceContains(*vote.Candidate, voteChange); contain {
				voteChange = append(voteChange[:i], voteChange[i+1:]...)
			} else {
				return errors.New("You haven't vote candidate " + strings.ToLower(vote.Candidate.Hex()) + " yet")
			}
		default:
			return errors.New("Vote candidate " + vote.Candidate.Hex() + " Operation error!")
		}
	}
	return nil
}

func (pool *TxPool) add(tx *types.Transaction, local bool) (bool, error) {
	wholeTransactionNumber := pool.config.GlobalQueue + pool.config.GlobalSlots
	log.Info("Tx_Pool|add|start", "tx_to", tx.To(), "type", tx.GetTransactionType().Hex())

	hash := tx.Hash()
	txType := tx.GetTransactionType()
	if pool.all[hash] != nil {
		log.Trace("Discarding already known transaction", "hash", hash)
		return false, fmt.Errorf("known transaction: %x", hash)
	}

	if err := pool.validateTx(tx, local); err != nil {
		log.Trace("Discarding invalid transaction", "hash", hash, "err", err)
		invalidTxCounter.Inc(1)
		return false, err
	}

	if uint64(len(pool.all)) >= wholeTransactionNumber {

		index, txList := pool.priced.Get(txType)
		var removeTxHash common.Hash

		if -1 == index {
			if len(*pool.priced.items) > maxTrxType {
				log.Trace("Transaction pool is full, discarding transaction", "hash", hash)
				return false, ErrFullPending
			}
			removeTxHash = pool.priced.RemoveTx()
		} else {
			if pool.priced.Underpriced(tx) {
				log.Trace("Discarding underpriced transaction", "hash", hash, "price", tx.GasPrice())
				underpricedTxCounter.Inc(1)
				return false, ErrUnderpriced
			}
			sort.Sort(txList)
			removeTx := (*txList)[len(*txList)-1]
			removeTxHash = removeTx.Hash()
		}
		pool.removeTx(removeTxHash)

	}

	from, _ := types.Sender(pool.signer, tx)
	if list := pool.pending[from]; list != nil && list.Overlaps(tx) {

		inserted, old := list.Add(tx, pool.config.PriceBump)
		if !inserted {
			pendingDiscardCounter.Inc(1)
			return false, ErrReplaceUnderpriced
		}

		if old != nil {
			delete(pool.all, old.Hash())
			pool.priced.RemoveByHash(old.GetTransactionType(), old.Hash())
			pendingReplaceCounter.Inc(1)
		}
		pool.all[tx.Hash()] = tx
		pool.priced.Put(tx, pool.currentState)
		pool.journalTx(from, tx)

		log.Trace("Pooled new executable transaction", "hash", hash, "from", from, "to", tx.To())

		go pool.txFeed.Send(TxPreEvent{tx})

		return old != nil, nil
	}

	replace, err := pool.enqueueTx(hash, tx)
	if err != nil {
		return false, err
	}

	if local {
		pool.locals.add(from)
	}
	pool.journalTx(from, tx)
	if !replace {

	}

	log.Trace("Pooled new future transaction", "hash", hash, "from", from, "to", tx.To())
	return replace, nil
}

func IsContractTransaction(tx *types.Transaction, db *state.StateDB) bool {
	switch tx.TxDataAction() {
	case types.ActionRegister, types.ActionAddVote, types.ActionSubVote, types.ActionPublishAsset:
		return false
	case types.ActionCreateContract, types.ActionCallContract:
		return true
	default:
		if tx.To() == nil {
			return len(tx.Data()) > 0
		} else {
			codeSize := db.GetCodeSize(*tx.To())
			return codeSize > 0
		}
	}
}

func (pool *TxPool) enqueueTx(hash common.Hash, tx *types.Transaction) (bool, error) {

	from, _ := types.Sender(pool.signer, tx)
	if pool.queue[from] == nil {
		pool.queue[from] = newTxList(false)
	}
	inserted, old := pool.queue[from].Add(tx, pool.config.PriceBump)
	if !inserted {

		queuedDiscardCounter.Inc(1)
		return false, ErrReplaceUnderpriced
	}

	if old != nil {
		delete(pool.all, old.Hash())
		pool.priced.RemoveByHash(old.GetTransactionType(), old.Hash())
		queuedReplaceCounter.Inc(1)
	}
	pool.all[hash] = tx
	pool.priced.Put(tx, pool.currentState)
	return old != nil, nil
}

func (pool *TxPool) journalTx(from common.Address, tx *types.Transaction) {

	if pool.journal == nil || !pool.locals.contains(from) {
		return
	}
	if err := pool.journal.insert(tx); err != nil {
		log.Warn("Failed to journal local transaction", "err", err)
	}
}

func (pool *TxPool) promoteTx(addr common.Address, hash common.Hash, tx *types.Transaction) {

	if pool.pending[addr] == nil {
		pool.pending[addr] = newTxList(true)
	}
	list := pool.pending[addr]

	inserted, old := list.Add(tx, pool.config.PriceBump)
	if !inserted {

		delete(pool.all, hash)
		pool.priced.RemoveByHash(tx.GetTransactionType(), hash)

		pendingDiscardCounter.Inc(1)
		return
	}

	if old != nil {
		delete(pool.all, old.Hash())
		pool.priced.RemoveByHash(old.GetTransactionType(), old.Hash())

		pendingReplaceCounter.Inc(1)
	}

	if pool.all[hash] == nil {
		pool.all[hash] = tx
		pool.priced.Put(tx, pool.currentState)
	}

	pool.beats[addr] = time.Now()
	pool.pendingState.SetNonce(addr, tx.Nonce()+1)

	go pool.txFeed.Send(TxPreEvent{tx})
}

func (pool *TxPool) AddLocal(tx *types.Transaction) error {
	return pool.addTx(tx, !pool.config.NoLocals)
}

func (pool *TxPool) AddRemote(tx *types.Transaction) error {
	return pool.addTx(tx, false)
}

func (pool *TxPool) AddLocals(txs []*types.Transaction) []error {
	return pool.addTxs(txs, !pool.config.NoLocals)
}

func (pool *TxPool) AddRemotes(txs []*types.Transaction) []error {
	return pool.addTxs(txs, false)
}

func (pool *TxPool) addTx(tx *types.Transaction, local bool) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	replace, err := pool.add(tx, local)
	if err != nil {
		return err
	}

	if !replace {
		from, _ := types.Sender(pool.signer, tx)
		pool.promoteExecutables([]common.Address{from})
	}
	return nil
}

func (pool *TxPool) addTxs(txs []*types.Transaction, local bool) []error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	return pool.addTxsLocked(txs, local)
}

func (pool *TxPool) addTxsLocked(txs []*types.Transaction, local bool) []error {

	dirty := make(map[common.Address]struct{})
	errs := make([]error, len(txs))

	for i, tx := range txs {
		var replace bool
		if replace, errs[i] = pool.add(tx, local); errs[i] == nil {
			if !replace {
				from, _ := types.Sender(pool.signer, tx)
				dirty[from] = struct{}{}
			}
		}
	}

	if len(dirty) > 0 {
		addrs := make([]common.Address, 0, len(dirty))
		for addr := range dirty {
			addrs = append(addrs, addr)
		}
		pool.promoteExecutables(addrs)
	}
	return errs
}

func (pool *TxPool) Status(hashes []common.Hash) []TxStatus {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	status := make([]TxStatus, len(hashes))
	for i, hash := range hashes {
		if tx := pool.all[hash]; tx != nil {
			from, _ := types.Sender(pool.signer, tx)
			if pool.pending[from] != nil && pool.pending[from].txs.items[tx.Nonce()] != nil {
				status[i] = TxStatusPending
			} else {
				status[i] = TxStatusQueued
			}
		}
	}
	return status
}

func (pool *TxPool) Get(hash common.Hash) *types.Transaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return pool.all[hash]
}

func (pool *TxPool) removeTx(hash common.Hash) {

	tx, ok := pool.all[hash]
	if !ok {
		return
	}
	addr, _ := types.Sender(pool.signer, tx)

	delete(pool.all, hash)
	_, x := pool.priced.items.Get(tx.GetTransactionType())
	log.Debug("Before Remove Trx", "trxNum", x.Len(), "type", tx.GetTransactionType().Hex())
	pool.priced.RemoveByHash(tx.GetTransactionType(), hash)
	log.Debug("After Remove Trx", "trxNum", x.Len(), "type", tx.GetTransactionType().Hex())

	if pending := pool.pending[addr]; pending != nil {
		if removed, invalids := pending.Remove(tx); removed {

			if pending.Empty() {
				delete(pool.pending, addr)
				delete(pool.beats, addr)
			} else {

				for _, tx := range invalids {
					pool.enqueueTx(tx.Hash(), tx)
				}
			}

			if nonce := tx.Nonce(); pool.pendingState.GetNonce(addr) > nonce {
				pool.pendingState.SetNonce(addr, nonce)
			}
			return
		}
	}

	if future := pool.queue[addr]; future != nil {
		future.Remove(tx)
		if future.Empty() {
			delete(pool.queue, addr)
		}
	}
}

func (pool *TxPool) promoteExecutables(accounts []common.Address) {

	if accounts == nil {
		accounts = make([]common.Address, 0, len(pool.queue))
		for addr := range pool.queue {
			accounts = append(accounts, addr)
		}
	}

	for _, addr := range accounts {
		list := pool.queue[addr]
		if list == nil {
			continue
		}

		for _, tx := range list.Forward(pool.currentState.GetNonce(addr)) {
			hash := tx.Hash()
			txType := tx.GetTransactionType()
			log.Trace("Removed old queued transaction", "hash", hash)
			delete(pool.all, hash)
			pool.priced.RemoveByHash(txType, hash)

		}

		drops, _ := list.Filter(pool.currentState.GetBalance(addr), pool.currentMaxGas)
		for _, tx := range drops {
			hash := tx.Hash()
			txType := tx.GetTransactionType()
			log.Trace("Removed unpayable queued transaction", "hash", hash)
			delete(pool.all, hash)
			pool.priced.RemoveByHash(txType, hash)
			queuedNofundsCounter.Inc(1)

		}

		for _, tx := range list.Ready(pool.pendingState.GetNonce(addr)) {
			hash := tx.Hash()
			log.Trace("Promoting queued transaction", "hash", hash)
			pool.promoteTx(addr, hash, tx)
		}

		if !pool.locals.contains(addr) {
			for _, tx := range list.Cap(int(pool.config.AccountQueue)) {
				hash := tx.Hash()
				txType := tx.GetTransactionType()
				delete(pool.all, hash)
				pool.priced.RemoveByHash(txType, hash)
				queuedRateLimitCounter.Inc(1)
				log.Trace("Removed cap-exceeding queued transaction", "hash", hash)

			}
		}

		if list.Empty() {
			delete(pool.queue, addr)
		}
	}

	pending := uint64(0)
	for _, list := range pool.pending {
		pending += uint64(list.Len())
	}
	if pending > pool.config.GlobalSlots {
		pendingBeforeCap := pending

		spammers := prque.New()
		for addr, list := range pool.pending {

			if !pool.locals.contains(addr) && uint64(list.Len()) > pool.config.AccountSlots {
				spammers.Push(addr, float32(list.Len()))
			}
		}

		offenders := []common.Address{}
		for pending > pool.config.GlobalSlots && !spammers.Empty() {

			offender, _ := spammers.Pop()
			offenders = append(offenders, offender.(common.Address))

			if len(offenders) > 1 {

				threshold := pool.pending[offender.(common.Address)].Len()

				for pending > pool.config.GlobalSlots && pool.pending[offenders[len(offenders)-2]].Len() > threshold {
					for i := 0; i < len(offenders)-1; i++ {
						list := pool.pending[offenders[i]]
						for _, tx := range list.Cap(list.Len() - 1) {

							hash := tx.Hash()
							txType := tx.GetTransactionType()
							delete(pool.all, hash)
							pool.priced.RemoveByHash(txType, hash)

							if nonce := tx.Nonce(); pool.pendingState.GetNonce(offenders[i]) > nonce {
								pool.pendingState.SetNonce(offenders[i], nonce)
							}
							log.Trace("Removed fairness-exceeding pending transaction", "hash", hash)
						}
						pending--
					}
				}
			}
		}

		if pending > pool.config.GlobalSlots && len(offenders) > 0 {
			for pending > pool.config.GlobalSlots && uint64(pool.pending[offenders[len(offenders)-1]].Len()) > pool.config.AccountSlots {
				for _, addr := range offenders {
					list := pool.pending[addr]
					for _, tx := range list.Cap(list.Len() - 1) {

						hash := tx.Hash()
						txType := tx.GetTransactionType()
						delete(pool.all, hash)
						pool.priced.RemoveByHash(txType, hash)

						if nonce := tx.Nonce(); pool.pendingState.GetNonce(addr) > nonce {
							pool.pendingState.SetNonce(addr, nonce)
						}
						log.Trace("Removed fairness-exceeding pending transaction", "hash", hash)
					}
					pending--
				}
			}
		}
		pendingRateLimitCounter.Inc(int64(pendingBeforeCap - pending))
	}

	queued := uint64(0)
	for _, list := range pool.queue {
		queued += uint64(list.Len())
	}
	if queued > pool.config.GlobalQueue {

		addresses := make(addresssByHeartbeat, 0, len(pool.queue))
		for addr := range pool.queue {
			if !pool.locals.contains(addr) {
				addresses = append(addresses, addressByHeartbeat{addr, pool.beats[addr]})
			}
		}
		sort.Sort(addresses)

		for drop := queued - pool.config.GlobalQueue; drop > 0 && len(addresses) > 0; {
			addr := addresses[len(addresses)-1]
			list := pool.queue[addr.address]

			addresses = addresses[:len(addresses)-1]

			if size := uint64(list.Len()); size <= drop {
				for _, tx := range list.Flatten() {
					pool.removeTx(tx.Hash())

				}
				drop -= size
				queuedRateLimitCounter.Inc(int64(size))
				continue
			}

			txs := list.Flatten()
			for i := len(txs) - 1; i >= 0 && drop > 0; i-- {
				pool.removeTx(txs[i].Hash())
				drop--
				queuedRateLimitCounter.Inc(1)
			}
		}
	}

}

func (pool *TxPool) demoteUnexecutables() {

	for addr, list := range pool.pending {
		nonce := pool.currentState.GetNonce(addr)

		for _, tx := range list.Forward(nonce) {
			hash := tx.Hash()
			txType := tx.GetTransactionType()
			log.Trace("Removed old pending transaction", "hash", hash)
			delete(pool.all, hash)
			pool.priced.RemoveByHash(txType, hash)

		}

		drops, invalids := list.Filter(pool.currentState.GetBalance(addr), pool.currentMaxGas)
		for _, tx := range drops {
			hash := tx.Hash()
			txType := tx.GetTransactionType()
			log.Trace("Removed unpayable pending transaction", "hash", hash)
			delete(pool.all, hash)
			pool.priced.RemoveByHash(txType, hash)
			pendingNofundsCounter.Inc(1)

		}

		for _, tx := range invalids {
			hash := tx.Hash()
			log.Trace("Demoting pending transaction", "hash", hash)
			pool.enqueueTx(hash, tx)
		}

		if list.Len() > 0 && list.txs.Get(nonce) == nil {
			for _, tx := range list.Cap(0) {
				hash := tx.Hash()
				log.Error("Demoting invalidated transaction", "hash", hash)
				pool.enqueueTx(hash, tx)
			}
		}

		if list.Empty() {
			delete(pool.pending, addr)
			delete(pool.beats, addr)
		}
	}
}

type addressByHeartbeat struct {
	address   common.Address
	heartbeat time.Time
}

type addresssByHeartbeat []addressByHeartbeat

func (a addresssByHeartbeat) Len() int           { return len(a) }
func (a addresssByHeartbeat) Less(i, j int) bool { return a[i].heartbeat.Before(a[j].heartbeat) }
func (a addresssByHeartbeat) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

type accountSet struct {
	accounts map[common.Address]struct{}
	signer   types.Signer
}

func newAccountSet(signer types.Signer) *accountSet {
	return &accountSet{
		accounts: make(map[common.Address]struct{}),
		signer:   signer,
	}
}

func (as *accountSet) contains(addr common.Address) bool {
	_, exist := as.accounts[addr]
	return exist
}

func (as *accountSet) containsTx(tx *types.Transaction) bool {
	if addr, err := types.Sender(as.signer, tx); err == nil {
		return as.contains(addr)
	}
	return false
}

func (as *accountSet) add(addr common.Address) {
	as.accounts[addr] = struct{}{}
}
