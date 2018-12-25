package light

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/consensus"
	"github.com/Aurorachain/go-Aurora/core"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/event"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/rlp"
	"github.com/hashicorp/golang-lru"
)

var (
	bodyCacheLimit  = 256
	blockCacheLimit = 256
)

type LightChain struct {
	hc            *core.HeaderChain
	chainDb       aoadb.Database
	odr           OdrBackend
	chainFeed     event.Feed
	chainSideFeed event.Feed
	chainHeadFeed event.Feed
	scope         event.SubscriptionScope
	genesisBlock  *types.Block

	mu      sync.RWMutex
	chainmu sync.RWMutex

	bodyCache    *lru.Cache
	bodyRLPCache *lru.Cache
	blockCache   *lru.Cache

	quit    chan struct{}
	running int32

	procInterrupt int32
	wg            sync.WaitGroup

	engine consensus.Engine
	config *params.ChainConfig
}

func NewLightChain(odr OdrBackend, config *params.ChainConfig, engine consensus.Engine) (*LightChain, error) {
	bodyCache, _ := lru.New(bodyCacheLimit)
	bodyRLPCache, _ := lru.New(bodyCacheLimit)
	blockCache, _ := lru.New(blockCacheLimit)

	bc := &LightChain{
		chainDb:      odr.Database(),
		odr:          odr,
		quit:         make(chan struct{}),
		bodyCache:    bodyCache,
		bodyRLPCache: bodyRLPCache,
		blockCache:   blockCache,
		engine:       engine,
		config:       config,
	}
	var err error
	bc.hc, err = core.NewHeaderChain(odr.Database(), config, bc.engine, bc.getProcInterrupt)
	if err != nil {
		return nil, err
	}
	bc.genesisBlock, _ = bc.GetBlockByNumber(NoOdr, 0)
	if bc.genesisBlock == nil {
		return nil, core.ErrNoGenesis
	}
	if cp, ok := trustedCheckpoints[bc.genesisBlock.Hash()]; ok {
		bc.addTrustedCheckpoint(cp)
	}

	if err := bc.loadLastState(); err != nil {
		return nil, err
	}

	for hash := range core.BadHashes {
		if header := bc.GetHeaderByHash(hash); header != nil {
			log.Error("Found bad hash, rewinding chain", "number", header.Number, "hash", header.ParentHash)
			bc.SetHead(header.Number.Uint64() - 1)
			log.Error("Chain rewind was successful, resuming normal operation")
		}
	}
	return bc, nil
}

func (self *LightChain) addTrustedCheckpoint(cp trustedCheckpoint) {
	if self.odr.ChtIndexer() != nil {
		StoreChtRoot(self.chainDb, cp.sectionIdx, cp.sectionHead, cp.chtRoot)
		self.odr.ChtIndexer().AddKnownSectionHead(cp.sectionIdx, cp.sectionHead)
	}
	if self.odr.BloomTrieIndexer() != nil {
		StoreBloomTrieRoot(self.chainDb, cp.sectionIdx, cp.sectionHead, cp.bloomTrieRoot)
		self.odr.BloomTrieIndexer().AddKnownSectionHead(cp.sectionIdx, cp.sectionHead)
	}
	if self.odr.BloomIndexer() != nil {
		self.odr.BloomIndexer().AddKnownSectionHead(cp.sectionIdx, cp.sectionHead)
	}
	log.Info("Added trusted checkpoint", "chain name", cp.name)
}

func (self *LightChain) getProcInterrupt() bool {
	return atomic.LoadInt32(&self.procInterrupt) == 1
}

func (self *LightChain) Odr() OdrBackend {
	return self.odr
}

func (self *LightChain) loadLastState() error {
	if head := core.GetHeadHeaderHash(self.chainDb); head == (common.Hash{}) {

		self.Reset()
	} else {
		if header := self.GetHeaderByHash(head); header != nil {
			self.hc.SetCurrentHeader(header)
		}
	}

	header := self.hc.CurrentHeader()
	headerTd := self.GetTd(header.Hash(), header.Number.Uint64())
	log.Info("Loaded most recent local header", "number", header.Number, "hash", header.Hash(), "td", headerTd)

	return nil
}

func (bc *LightChain) SetHead(head uint64) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.hc.SetHead(head, nil)
	bc.loadLastState()
}

func (self *LightChain) GasLimit() uint64 {
	self.mu.RLock()
	defer self.mu.RUnlock()

	return self.hc.CurrentHeader().GasLimit
}

func (self *LightChain) LastBlockHash() common.Hash {
	self.mu.RLock()
	defer self.mu.RUnlock()

	return self.hc.CurrentHeader().Hash()
}

func (self *LightChain) Status() (td *big.Int, currentBlock common.Hash, genesisBlock common.Hash) {
	self.mu.RLock()
	defer self.mu.RUnlock()

	header := self.hc.CurrentHeader()
	hash := header.Hash()
	return self.GetTd(hash, header.Number.Uint64()), hash, self.genesisBlock.Hash()
}

func (self *LightChain) GetGenesisConfig() *params.ChainConfig {
	return self.config
}

func (bc *LightChain) Reset() {
	bc.ResetWithGenesisBlock(bc.genesisBlock)
}

func (bc *LightChain) ResetWithGenesisBlock(genesis *types.Block) {

	bc.SetHead(0)

	bc.mu.Lock()
	defer bc.mu.Unlock()

	if err := core.WriteTd(bc.chainDb, genesis.Hash(), genesis.NumberU64(), types.BlockDifficult); err != nil {
		log.Crit("Failed to write genesis block TD", "err", err)
	}
	if err := core.WriteBlock(bc.chainDb, genesis); err != nil {
		log.Crit("Failed to write genesis block", "err", err)
	}
	bc.genesisBlock = genesis
	bc.hc.SetGenesis(bc.genesisBlock.Header())
	bc.hc.SetCurrentHeader(bc.genesisBlock.Header())
}

func (bc *LightChain) Engine() consensus.Engine { return bc.engine }

func (bc *LightChain) Genesis() *types.Block {
	return bc.genesisBlock
}

func (bc *LightChain) GetDelegatePoll() (*map[common.Address]types.Candidate, error) {
	return nil, nil
}

func (self *LightChain) GetBody(ctx context.Context, hash common.Hash) (*types.Body, error) {

	if cached, ok := self.bodyCache.Get(hash); ok {
		body := cached.(*types.Body)
		return body, nil
	}
	body, err := GetBody(ctx, self.odr, hash, self.hc.GetBlockNumber(hash))
	if err != nil {
		return nil, err
	}

	self.bodyCache.Add(hash, body)
	return body, nil
}

func (self *LightChain) GetBodyRLP(ctx context.Context, hash common.Hash) (rlp.RawValue, error) {

	if cached, ok := self.bodyRLPCache.Get(hash); ok {
		return cached.(rlp.RawValue), nil
	}
	body, err := GetBodyRLP(ctx, self.odr, hash, self.hc.GetBlockNumber(hash))
	if err != nil {
		return nil, err
	}

	self.bodyRLPCache.Add(hash, body)
	return body, nil
}

func (bc *LightChain) HasBlock(hash common.Hash, number uint64) bool {
	blk, _ := bc.GetBlock(NoOdr, hash, number)
	return blk != nil
}

func (self *LightChain) GetBlock(ctx context.Context, hash common.Hash, number uint64) (*types.Block, error) {

	if block, ok := self.blockCache.Get(hash); ok {
		return block.(*types.Block), nil
	}
	block, err := GetBlock(ctx, self.odr, hash, number)
	if err != nil {
		return nil, err
	}

	self.blockCache.Add(block.Hash(), block)
	return block, nil
}

func (self *LightChain) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return self.GetBlock(ctx, hash, self.hc.GetBlockNumber(hash))
}

func (self *LightChain) GetBlockByNumber(ctx context.Context, number uint64) (*types.Block, error) {
	hash, err := GetCanonicalHash(ctx, self.odr, number)
	if hash == (common.Hash{}) || err != nil {
		return nil, err
	}
	return self.GetBlock(ctx, hash, number)
}

func (bc *LightChain) Stop() {
	if !atomic.CompareAndSwapInt32(&bc.running, 0, 1) {
		return
	}
	close(bc.quit)
	atomic.StoreInt32(&bc.procInterrupt, 1)

	bc.wg.Wait()
	log.Info("Blockchain manager stopped")
}

func (self *LightChain) Rollback(chain []common.Hash) {
	self.mu.Lock()
	defer self.mu.Unlock()

	for i := len(chain) - 1; i >= 0; i-- {
		hash := chain[i]

		if head := self.hc.CurrentHeader(); head.Hash() == hash {
			self.hc.SetCurrentHeader(self.GetHeader(head.ParentHash, head.Number.Uint64()-1))
		}
	}
}

func (self *LightChain) postChainEvents(events []interface{}) {
	for _, event := range events {
		switch ev := event.(type) {
		case core.ChainEvent:
			if self.LastBlockHash() == ev.Hash {
				self.chainHeadFeed.Send(core.ChainHeadEvent{Block: ev.Block})
			}
			self.chainFeed.Send(ev)
		case core.ChainSideEvent:
			self.chainSideFeed.Send(ev)
		}
	}
}

func (self *LightChain) InsertHeaderChain(chain []*types.Header, checkFreq int) (int, error) {
	start := time.Now()
	if i, err := self.hc.ValidateHeaderChain(chain, checkFreq); err != nil {
		return i, err
	}

	self.chainmu.Lock()
	defer func() {
		self.chainmu.Unlock()
		time.Sleep(time.Millisecond * 10)
	}()

	self.wg.Add(1)
	defer self.wg.Done()

	var events []interface{}
	whFunc := func(header *types.Header) error {
		self.mu.Lock()
		defer self.mu.Unlock()

		status, err := self.hc.WriteHeader(header)

		switch status {
		case core.CanonStatTy:
			log.Debug("Inserted new header", "number", header.Number, "hash", header.Hash())
			events = append(events, core.ChainEvent{Block: types.NewBlockWithHeader(header), Hash: header.Hash()})

		case core.SideStatTy:
			log.Debug("Inserted forked header", "number", header.Number, "hash", header.Hash())
			events = append(events, core.ChainSideEvent{Block: types.NewBlockWithHeader(header)})
		}
		return err
	}
	i, err := self.hc.InsertHeaderChain(chain, whFunc, start)
	self.postChainEvents(events)
	return i, err
}

func (self *LightChain) CurrentHeader() *types.Header {
	self.mu.RLock()
	defer self.mu.RUnlock()

	return self.hc.CurrentHeader()
}

func (self *LightChain) GetTd(hash common.Hash, number uint64) *big.Int {
	return self.hc.GetTd(hash, number)
}

func (self *LightChain) GetTdByHash(hash common.Hash) *big.Int {
	return self.hc.GetTdByHash(hash)
}

func (self *LightChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	return self.hc.GetHeader(hash, number)
}

func (self *LightChain) GetHeaderByHash(hash common.Hash) *types.Header {
	return self.hc.GetHeaderByHash(hash)
}

func (bc *LightChain) HasHeader(hash common.Hash, number uint64) bool {
	return bc.hc.HasHeader(hash, number)
}

func (self *LightChain) GetBlockHashesFromHash(hash common.Hash, max uint64) []common.Hash {
	return self.hc.GetBlockHashesFromHash(hash, max)
}

func (self *LightChain) GetHeaderByNumber(number uint64) *types.Header {
	return self.hc.GetHeaderByNumber(number)
}

func (self *LightChain) GetHeaderByNumberOdr(ctx context.Context, number uint64) (*types.Header, error) {
	if header := self.hc.GetHeaderByNumber(number); header != nil {
		return header, nil
	}
	return GetHeaderByNumber(ctx, self.odr, number)
}

func (self *LightChain) Config() *params.ChainConfig { return self.hc.Config() }

func (self *LightChain) SyncCht(ctx context.Context) bool {
	if self.odr.ChtIndexer() == nil {
		return false
	}
	headNum := self.CurrentHeader().Number.Uint64()
	chtCount, _, _ := self.odr.ChtIndexer().Sections()
	if headNum+1 < chtCount*ChtFrequency {
		num := chtCount*ChtFrequency - 1
		header, err := GetHeaderByNumber(ctx, self.odr, num)
		if header != nil && err == nil {
			self.mu.Lock()
			if self.hc.CurrentHeader().Number.Uint64() < header.Number.Uint64() {
				self.hc.SetCurrentHeader(header)
			}
			self.mu.Unlock()
			return true
		}
	}
	return false
}

func (self *LightChain) LockChain() {
	self.chainmu.RLock()
}

func (self *LightChain) UnlockChain() {
	self.chainmu.RUnlock()
}

func (self *LightChain) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return self.scope.Track(self.chainFeed.Subscribe(ch))
}

func (self *LightChain) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return self.scope.Track(self.chainHeadFeed.Subscribe(ch))
}

func (self *LightChain) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return self.scope.Track(self.chainSideFeed.Subscribe(ch))
}

func (self *LightChain) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return self.scope.Track(new(event.Feed).Subscribe(ch))
}

func (self *LightChain) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return self.scope.Track(new(event.Feed).Subscribe(ch))
}
