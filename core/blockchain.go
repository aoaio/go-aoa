package core

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/mclock"
	"github.com/Aurorachain/go-Aurora/consensus"
	"github.com/Aurorachain/go-Aurora/core/state"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/core/vm"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/event"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/metrics"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/rlp"
	"github.com/Aurorachain/go-Aurora/trie"
	"github.com/hashicorp/golang-lru"

	"github.com/Aurorachain/go-Aurora/consensus/delegatestate"
	"github.com/Aurorachain/go-Aurora/core/watch"
)

var (
	blockInsertTimer = metrics.NewTimer("chain/inserts")

	ErrNoGenesis = errors.New("Genesis not found in chain")
)

const (
	bodyCacheLimit      = 256
	blockCacheLimit     = 256
	maxFutureBlocks     = 256
	maxTimeFutureBlocks = 30
	badBlockLimit       = 10

	BlockChainVersion = 3
)

type BlockChain struct {
	config *params.ChainConfig

	hc            *HeaderChain
	chainDb       aoadb.Database
	rmLogsFeed    event.Feed
	chainFeed     event.Feed
	chainSideFeed event.Feed
	chainHeadFeed event.Feed
	logsFeed      event.Feed
	scope         event.SubscriptionScope
	genesisBlock  *types.Block

	mu      sync.RWMutex
	chainmu sync.RWMutex
	procmu  sync.RWMutex

	checkpoint       int
	currentBlock     *types.Block
	currentFastBlock *types.Block

	stateCache    state.Database
	delegateCache delegatestate.Database
	bodyCache     *lru.Cache
	bodyRLPCache  *lru.Cache
	blockCache    *lru.Cache
	futureBlocks  *lru.Cache
	innerTxDb     watch.InnerTxDb

	quit    chan struct{}
	running int32

	procInterrupt int32
	wg            sync.WaitGroup

	aoaEngine consensus.Engine
	processor Processor
	validator Validator
	vmConfig  vm.Config

	badBlocks            *lru.Cache
	candidateWrapperChan chan *types.CandidateWrapper
	delegateList         *map[string]types.Candidate
}

func NewBlockChain(chainDb aoadb.Database, config *params.ChainConfig, aoaEngine consensus.Engine, vmConfig vm.Config, itxDb aoadb.Database) (*BlockChain, error) {
	bodyCache, _ := lru.New(bodyCacheLimit)
	bodyRLPCache, _ := lru.New(bodyCacheLimit)
	blockCache, _ := lru.New(blockCacheLimit)
	futureBlocks, _ := lru.New(maxFutureBlocks)
	badBlocks, _ := lru.New(badBlockLimit)

	bc := &BlockChain{
		config:               config,
		chainDb:              chainDb,
		stateCache:           state.NewDatabase(chainDb),
		quit:                 make(chan struct{}),
		bodyCache:            bodyCache,
		bodyRLPCache:         bodyRLPCache,
		blockCache:           blockCache,
		futureBlocks:         futureBlocks,
		vmConfig:             vmConfig,
		badBlocks:            badBlocks,
		candidateWrapperChan: make(chan *types.CandidateWrapper),
		delegateCache:        delegatestate.NewDatabase(chainDb),
		aoaEngine:            aoaEngine,
		innerTxDb:            watch.NewInnerTxDb(itxDb),
	}
	bc.SetValidator(NewBlockValidator(config, bc, aoaEngine))
	bc.SetProcessor(NewStateProcessor(config, bc, aoaEngine))

	var err error
	bc.hc, err = NewHeaderChain(chainDb, config, aoaEngine, bc.getProcInterrupt)
	if err != nil {
		return nil, err
	}
	bc.genesisBlock = bc.GetBlockByNumber(0)
	if bc.genesisBlock == nil {
		return nil, ErrNoGenesis
	}
	if err := bc.loadLastState(); err != nil {
		return nil, err
	}

	for hash := range BadHashes {
		if header := bc.GetHeaderByHash(hash); header != nil {

			headerByNumber := bc.GetHeaderByNumber(header.Number.Uint64())

			if headerByNumber != nil && headerByNumber.Hash() == header.Hash() {
				log.Error("Found bad hash, rewinding chain", "number", header.Number, "hash", header.ParentHash)
				bc.SetHead(header.Number.Uint64() - 1)
				log.Error("Chain rewind was successful, resuming normal operation")
			}
		}
	}

	go bc.update()
	return bc, nil
}

func (bc *BlockChain) RollbackBFT(block *types.Block) {
	blockHash := block.Hash()
	log.Error("BlockChain|rollbackBFT start", "blockNumber", block.NumberU64(), "blockHash", blockHash)
	if header := bc.GetHeaderByHash(blockHash); header != nil {
		headerByNumber := bc.GetHeaderByNumber(header.Number.Uint64())
		if headerByNumber != nil && headerByNumber.Hash() == header.Hash() {
			bc.SetHead(header.Number.Uint64() - 1)
			log.Error("BlockChain|rollbackBFT Chain rewind was successful, resuming normal operation")
		}
	}
}

func (bc *BlockChain) getProcInterrupt() bool {
	return atomic.LoadInt32(&bc.procInterrupt) == 1
}

func (bc *BlockChain) GetDelegateDB() *delegatestate.Database {
	return &bc.delegateCache
}

func (bc *BlockChain) loadLastState() error {

	head := GetHeadBlockHash(bc.chainDb)
	if head == (common.Hash{}) {

		log.Warn("Empty database, resetting chain")
		return bc.Reset()
	}

	currentBlock := bc.GetBlockByHash(head)
	if currentBlock == nil {

		log.Warn("Head block missing, resetting chain", "hash", head)
		return bc.Reset()
	}

	if _, err := state.New(currentBlock.Root(), bc.stateCache); err != nil {

		log.Warn("Head state missing, resetting chain", "number", currentBlock.Number(), "hash", currentBlock.Hash())
		return bc.Reset()
	}

	if _, err := delegatestate.New(currentBlock.DelegateRoot(), bc.delegateCache); err != nil {
		log.Warn("Head delegate state missing, resetting chain", "number", currentBlock.Number(), "hash", currentBlock.Hash())
		return bc.Reset()
	}

	bc.currentBlock = currentBlock

	currentHeader := bc.currentBlock.Header()
	if head := GetHeadHeaderHash(bc.chainDb); head != (common.Hash{}) {
		if header := bc.GetHeaderByHash(head); header != nil {
			currentHeader = header
		}
	}
	bc.hc.SetCurrentHeader(currentHeader)

	bc.currentFastBlock = bc.currentBlock
	if head := GetHeadFastBlockHash(bc.chainDb); head != (common.Hash{}) {
		if block := bc.GetBlockByHash(head); block != nil {
			bc.currentFastBlock = block
		}
	}

	headerTd := bc.GetTd(currentHeader.Hash(), currentHeader.Number.Uint64())
	blockTd := bc.GetTd(bc.currentBlock.Hash(), bc.currentBlock.NumberU64())
	fastTd := bc.GetTd(bc.currentFastBlock.Hash(), bc.currentFastBlock.NumberU64())

	log.Info("Loaded most recent local header", "number", currentHeader.Number, "hash", currentHeader.Hash(), "td", headerTd)
	log.Info("Loaded most recent local full block", "number", bc.currentBlock.Number(), "hash", bc.currentBlock.Hash(), "td", blockTd)
	log.Info("Loaded most recent local fast block", "number", bc.currentFastBlock.Number(), "hash", bc.currentFastBlock.Hash(), "td", fastTd)

	return nil
}

func (bc *BlockChain) SetHead(head uint64) error {
	log.Warn("Rewinding blockchain", "target", head)

	bc.mu.Lock()
	defer bc.mu.Unlock()

	delFn := func(hash common.Hash, num uint64) {
		DeleteBody(bc.chainDb, hash, num)
	}
	bc.hc.SetHead(head, delFn)
	currentHeader := bc.hc.CurrentHeader()

	bc.bodyCache.Purge()
	bc.bodyRLPCache.Purge()
	bc.blockCache.Purge()
	bc.futureBlocks.Purge()

	if bc.currentBlock != nil && currentHeader.Number.Uint64() < bc.currentBlock.NumberU64() {
		bc.currentBlock = bc.GetBlock(currentHeader.Hash(), currentHeader.Number.Uint64())
	}
	if bc.currentBlock != nil {
		if _, err := state.New(bc.currentBlock.Root(), bc.stateCache); err != nil {

			bc.currentBlock = nil
		}
		if _, err := delegatestate.New(bc.CurrentBlock().DelegateRoot(), bc.delegateCache); err != nil {
			bc.currentBlock = nil
		}
	}

	if bc.currentFastBlock != nil && currentHeader.Number.Uint64() < bc.currentFastBlock.NumberU64() {
		bc.currentFastBlock = bc.GetBlock(currentHeader.Hash(), currentHeader.Number.Uint64())
	}

	if bc.currentBlock == nil {
		bc.currentBlock = bc.genesisBlock
	}
	if bc.currentFastBlock == nil {
		bc.currentFastBlock = bc.genesisBlock
	}
	if err := WriteHeadBlockHash(bc.chainDb, bc.currentBlock.Hash()); err != nil {
		log.Crit("Failed to reset head full block", "err", err)
	}
	if err := WriteHeadFastBlockHash(bc.chainDb, bc.currentFastBlock.Hash()); err != nil {
		log.Crit("Failed to reset head fast block", "err", err)
	}
	return bc.loadLastState()
}

func (bc *BlockChain) FastSyncCommitHead(hash common.Hash) error {

	block := bc.GetBlockByHash(hash)
	if block == nil {
		return fmt.Errorf("non existent block [%x…]", hash[:4])
	}
	if _, err := trie.NewSecure(block.Root(), bc.chainDb, 0); err != nil {
		return err
	}

	bc.mu.Lock()
	bc.currentBlock = block
	bc.mu.Unlock()

	log.Info("Committed new head block", "number", block.Number(), "hash", hash)
	return nil
}

func (bc *BlockChain) GasLimit() uint64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return bc.currentBlock.GasLimit()
}

func (bc *BlockChain) LastBlockHash() common.Hash {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return bc.currentBlock.Hash()
}

func (bc *BlockChain) CurrentBlock() *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return bc.currentBlock
}

func (bc *BlockChain) CurrentFastBlock() *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return bc.currentFastBlock
}

func (bc *BlockChain) Status() (td *big.Int, currentBlock common.Hash, genesisBlock common.Hash) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return bc.GetTd(bc.currentBlock.Hash(), bc.currentBlock.NumberU64()), bc.currentBlock.Hash(), bc.genesisBlock.Hash()
}

func (bc *BlockChain) SetProcessor(processor Processor) {
	bc.procmu.Lock()
	defer bc.procmu.Unlock()
	bc.processor = processor
}

func (bc *BlockChain) SetValidator(validator Validator) {
	bc.procmu.Lock()
	defer bc.procmu.Unlock()
	bc.validator = validator
}

func (bc *BlockChain) Validator() Validator {
	bc.procmu.RLock()
	defer bc.procmu.RUnlock()
	return bc.validator
}

func (bc *BlockChain) Processor() Processor {
	bc.procmu.RLock()
	defer bc.procmu.RUnlock()
	return bc.processor
}

func (bc *BlockChain) State() (*state.StateDB, error) {
	return bc.StateAt(bc.CurrentBlock().Root())
}

func (bc *BlockChain) StateAt(root common.Hash) (*state.StateDB, error) {
	return state.New(root, bc.stateCache)
}

func (bc *BlockChain) Reset() error {
	return bc.ResetWithGenesisBlock(bc.genesisBlock)
}

func (bc *BlockChain) DelegateState() (*delegatestate.DelegateDB, error) {
	return bc.DelegateStateAt(bc.CurrentBlock().DelegateRoot())
}

func (bc *BlockChain) DelegateStateAt(root common.Hash) (*delegatestate.DelegateDB, error) {
	return delegatestate.New(root, bc.delegateCache)
}

func (bc *BlockChain) ResetWithGenesisBlock(genesis *types.Block) error {

	if err := bc.SetHead(0); err != nil {
		return err
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if err := bc.hc.WriteTd(genesis.Hash(), genesis.NumberU64(), types.BlockDifficult); err != nil {
		log.Crit("Failed to write genesis block TD", "err", err)
	}
	if err := WriteBlock(bc.chainDb, genesis); err != nil {
		log.Crit("Failed to write genesis block", "err", err)
	}
	bc.genesisBlock = genesis
	bc.insert(bc.genesisBlock)
	bc.currentBlock = bc.genesisBlock
	bc.hc.SetGenesis(bc.genesisBlock.Header())
	bc.hc.SetCurrentHeader(bc.genesisBlock.Header())
	bc.currentFastBlock = bc.genesisBlock

	return nil
}

func (bc *BlockChain) Export(w io.Writer) error {
	return bc.ExportN(w, uint64(0), bc.currentBlock.NumberU64())
}

func (bc *BlockChain) ExportN(w io.Writer, first uint64, last uint64) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	if first > last {
		return fmt.Errorf("export failed: first (%d) is greater than last (%d)", first, last)
	}
	log.Info("Exporting batch of blocks", "count", last-first+1)

	for nr := first; nr <= last; nr++ {
		block := bc.GetBlockByNumber(nr)
		if block == nil {
			return fmt.Errorf("export failed on #%d: not found", nr)
		}

		if err := block.EncodeRLP(w); err != nil {
			return err
		}
	}

	return nil
}

func (bc *BlockChain) insert(block *types.Block) {

	updateHeads := GetCanonicalHash(bc.chainDb, block.NumberU64()) != block.Hash()

	if err := WriteCanonicalHash(bc.chainDb, block.Hash(), block.NumberU64()); err != nil {
		log.Crit("Failed to insert block number", "err", err)
	}
	if err := WriteHeadBlockHash(bc.chainDb, block.Hash()); err != nil {
		log.Crit("Failed to insert head block hash", "err", err)
	}
	bc.currentBlock = block

	if updateHeads {
		bc.hc.SetCurrentHeader(block.Header())

		if err := WriteHeadFastBlockHash(bc.chainDb, block.Hash()); err != nil {
			log.Crit("Failed to insert head fast block hash", "err", err)
		}
		bc.currentFastBlock = block
	}
}

func (bc *BlockChain) Genesis() *types.Block {
	return bc.genesisBlock
}

func (bc *BlockChain) GetBody(hash common.Hash) *types.Body {

	if cached, ok := bc.bodyCache.Get(hash); ok {
		body := cached.(*types.Body)
		return body
	}
	body := GetBody(bc.chainDb, hash, bc.hc.GetBlockNumber(hash))
	if body == nil {
		return nil
	}

	bc.bodyCache.Add(hash, body)
	return body
}

func (bc *BlockChain) GetBodyRLP(hash common.Hash) rlp.RawValue {

	if cached, ok := bc.bodyRLPCache.Get(hash); ok {
		return cached.(rlp.RawValue)
	}
	body := GetBodyRLP(bc.chainDb, hash, bc.hc.GetBlockNumber(hash))
	if len(body) == 0 {
		return nil
	}

	bc.bodyRLPCache.Add(hash, body)
	return body
}

func (bc *BlockChain) HasBlock(hash common.Hash, number uint64) bool {
	if bc.blockCache.Contains(hash) {
		return true
	}
	ok, _ := bc.chainDb.Has(blockBodyKey(hash, number))
	return ok
}

func (bc *BlockChain) HasBlockAndState(hash common.Hash) bool {

	block := bc.GetBlockByHash(hash)
	if block == nil {
		return false
	}

	_, err := bc.stateCache.OpenTrie(block.Root())
	return err == nil
}

func (bc *BlockChain) GetBlock(hash common.Hash, number uint64) *types.Block {

	if block, ok := bc.blockCache.Get(hash); ok {
		return block.(*types.Block)
	}
	block := GetBlock(bc.chainDb, hash, number)
	if block == nil {
		return nil
	}

	bc.blockCache.Add(block.Hash(), block)
	return block
}

func (bc *BlockChain) GetBlockByHash(hash common.Hash) *types.Block {
	return bc.GetBlock(hash, bc.hc.GetBlockNumber(hash))
}

func (bc *BlockChain) GetBlockByNumber(number uint64) *types.Block {
	hash := GetCanonicalHash(bc.chainDb, number)
	if hash == (common.Hash{}) {
		return nil
	}
	return bc.GetBlock(hash, number)
}

func (bc *BlockChain) GetBlocksFromHash(hash common.Hash, n int) (blocks []*types.Block) {
	number := bc.hc.GetBlockNumber(hash)
	for i := 0; i < n; i++ {
		block := bc.GetBlock(hash, number)
		if block == nil {
			break
		}
		blocks = append(blocks, block)
		hash = block.ParentHash()
		number--
	}
	return
}

func (bc *BlockChain) GetUnclesInChain(block *types.Block, length int) []*types.Header {
	var uncles []*types.Header
	for i := 0; block != nil && i < length; i++ {
		block = bc.GetBlock(block.ParentHash(), block.NumberU64()-1)
	}
	return uncles
}

func (bc *BlockChain) Stop() {
	if !atomic.CompareAndSwapInt32(&bc.running, 0, 1) {
		return
	}

	bc.scope.Close()
	close(bc.quit)
	atomic.StoreInt32(&bc.procInterrupt, 1)

	bc.wg.Wait()
	log.Info("Blockchain manager stopped")
}

func (bc *BlockChain) procFutureBlocks() {
	blocks := make([]*types.Block, 0, bc.futureBlocks.Len())
	for _, hash := range bc.futureBlocks.Keys() {
		if block, exist := bc.futureBlocks.Peek(hash); exist {
			blocks = append(blocks, block.(*types.Block))
		}
	}
	if len(blocks) > 0 {
		types.BlockBy(types.Number).Sort(blocks)

		for i := range blocks {
			bc.InsertChain(blocks[i : i+1])
		}
	}
}

type WriteStatus byte

const (
	NonStatTy   WriteStatus = iota
	CanonStatTy 
	SideStatTy  
)

func (bc *BlockChain) Rollback(chain []common.Hash) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	for i := len(chain) - 1; i >= 0; i-- {
		hash := chain[i]

		currentHeader := bc.hc.CurrentHeader()
		if currentHeader.Hash() == hash {
			bc.hc.SetCurrentHeader(bc.GetHeader(currentHeader.ParentHash, currentHeader.Number.Uint64()-1))
		}
		if bc.currentFastBlock.Hash() == hash {
			bc.currentFastBlock = bc.GetBlock(bc.currentFastBlock.ParentHash(), bc.currentFastBlock.NumberU64()-1)
			WriteHeadFastBlockHash(bc.chainDb, bc.currentFastBlock.Hash())
		}
		if bc.currentBlock.Hash() == hash {
			bc.currentBlock = bc.GetBlock(bc.currentBlock.ParentHash(), bc.currentBlock.NumberU64()-1)
			WriteHeadBlockHash(bc.chainDb, bc.currentBlock.Hash())
		}
	}
}

func SetReceiptsData(config *params.ChainConfig, block *types.Block, receipts types.Receipts) {
	signer := types.MakeSigner(config, block.Number())

	transactions, logIndex := block.Transactions(), uint(0)

	for j := 0; j < len(receipts); j++ {

		receipts[j].TxHash = transactions[j].Hash()

		if transactions[j].To() == nil {

			from, _ := types.Sender(signer, transactions[j])
			receipts[j].ContractAddress = crypto.CreateAddress(from, transactions[j].Nonce())
		}

		if j == 0 {
			receipts[j].GasUsed = receipts[j].CumulativeGasUsed
		} else {
			receipts[j].GasUsed = receipts[j].CumulativeGasUsed - receipts[j-1].CumulativeGasUsed
		}

		for k := 0; k < len(receipts[j].Logs); k++ {
			receipts[j].Logs[k].BlockNumber = block.NumberU64()
			receipts[j].Logs[k].BlockHash = block.Hash()
			receipts[j].Logs[k].TxHash = receipts[j].TxHash
			receipts[j].Logs[k].TxIndex = uint(j)
			receipts[j].Logs[k].Index = logIndex
			logIndex++
		}
	}
}

func (bc *BlockChain) InsertReceiptChain(blockChain types.Blocks, receiptChain []types.Receipts) (int, error) {
	bc.wg.Add(1)
	defer bc.wg.Done()

	for i := 1; i < len(blockChain); i++ {
		if blockChain[i].NumberU64() != blockChain[i-1].NumberU64()+1 || blockChain[i].ParentHash() != blockChain[i-1].Hash() {
			log.Error("Non contiguous receipt insert", "number", blockChain[i].Number(), "hash", blockChain[i].Hash(), "parent", blockChain[i].ParentHash(),
				"prevnumber", blockChain[i-1].Number(), "prevhash", blockChain[i-1].Hash())
			return 0, fmt.Errorf("non contiguous insert: item %d is #%d [%x…], item %d is #%d [%x…] (parent [%x…])", i-1, blockChain[i-1].NumberU64(),
				blockChain[i-1].Hash().Bytes()[:4], i, blockChain[i].NumberU64(), blockChain[i].Hash().Bytes()[:4], blockChain[i].ParentHash().Bytes()[:4])
		}
	}

	var (
		stats = struct{ processed, ignored int32 }{}
		start = time.Now()
		bytes = 0
		batch = bc.chainDb.NewBatch()
	)
	for i, block := range blockChain {
		receipts := receiptChain[i]

		if atomic.LoadInt32(&bc.procInterrupt) == 1 {
			return 0, nil
		}

		if !bc.HasHeader(block.Hash(), block.NumberU64()) {
			return i, fmt.Errorf("containing header #%d [%x…] unknown", block.Number(), block.Hash().Bytes()[:4])
		}

		if bc.HasBlock(block.Hash(), block.NumberU64()) {
			stats.ignored++
			continue
		}

		SetReceiptsData(bc.config, block, receipts)

		if err := WriteBody(batch, block.Hash(), block.NumberU64(), block.Body()); err != nil {
			return i, fmt.Errorf("failed to write block body: %v", err)
		}
		if err := WriteBlockReceipts(batch, block.Hash(), block.NumberU64(), receipts); err != nil {
			return i, fmt.Errorf("failed to write block receipts: %v", err)
		}
		if err := WriteTxLookupEntries(batch, block); err != nil {
			return i, fmt.Errorf("failed to write lookup metadata: %v", err)
		}
		stats.processed++

		if batch.ValueSize() >= aoadb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return 0, err
			}
			bytes += batch.ValueSize()
			batch = bc.chainDb.NewBatch()
		}
	}
	if batch.ValueSize() > 0 {
		bytes += batch.ValueSize()
		if err := batch.Write(); err != nil {
			return 0, err
		}
	}

	bc.mu.Lock()
	head := blockChain[len(blockChain)-1]
	if td := bc.GetTd(head.Hash(), head.NumberU64()); td != nil {
		if bc.GetTd(bc.currentFastBlock.Hash(), bc.currentFastBlock.NumberU64()).Cmp(td) < 0 {
			if err := WriteHeadFastBlockHash(bc.chainDb, head.Hash()); err != nil {
				log.Crit("Failed to update head fast block hash", "err", err)
			}
			bc.currentFastBlock = head
		}
	}
	bc.mu.Unlock()

	log.Info("Imported new block receipts",
		"count", stats.processed,
		"elapsed", common.PrettyDuration(time.Since(start)),
		"bytes", bytes,
		"number", head.Number(),
		"hash", head.Hash(),
		"ignored", stats.ignored)
	return 0, nil
}

func (bc *BlockChain) WriteBlockAndState(block *types.Block, receipts []*types.Receipt, state *state.StateDB, delegatedb *delegatestate.DelegateDB) (status WriteStatus, err error) {
	bc.wg.Add(1)
	defer bc.wg.Done()

	ptd := bc.GetTd(block.ParentHash(), block.NumberU64()-1)
	if ptd == nil {
		return NonStatTy, consensus.ErrUnknownAncestor
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()

	localTd := bc.GetTd(bc.currentBlock.Hash(), bc.currentBlock.NumberU64())
	externTd := new(big.Int).Add(common.Big1, ptd)

	if err := bc.hc.WriteTd(block.Hash(), block.NumberU64(), externTd); err != nil {
		return NonStatTy, err
	}

	batch := bc.chainDb.NewBatch()
	if err := WriteBlock(batch, block); err != nil {
		return NonStatTy, err
	}
	if _, err := state.CommitTo(batch, false); err != nil {
		return NonStatTy, err
	}

	if _, err := delegatedb.CommitTo(batch, false); err != nil {
		return NonStatTy, err
	}
	if err := WriteBlockReceipts(batch, block.Hash(), block.NumberU64(), receipts); err != nil {
		return NonStatTy, err
	}

	reorg := externTd.Cmp(localTd) > 0
	log.Debug("insertChain", "externTd", externTd, "localTd", localTd, "reorg", reorg)
	if !reorg && externTd.Cmp(localTd) == 0 {

		reorg = block.NumberU64() < bc.currentBlock.NumberU64() || (block.NumberU64() == bc.currentBlock.NumberU64() && mrand.Float64() < 0.5)
		log.Debug("insertChain", "externTd == localTd|reorg", reorg)
	}
	if reorg {

		if block.ParentHash() != bc.currentBlock.Hash() {
			if err := bc.reorg(bc.currentBlock, block); err != nil {
				return NonStatTy, err
			}
		}

		if err := WriteTxLookupEntries(batch, block); err != nil {
			return NonStatTy, err
		}

		if err := WritePreimages(bc.chainDb, block.NumberU64(), state.Preimages()); err != nil {
			return NonStatTy, err
		}
		status = CanonStatTy
	} else {
		status = SideStatTy
	}
	if err := batch.Write(); err != nil {
		return NonStatTy, err
	}

	if status == CanonStatTy {
		bc.insert(block)
	}
	bc.futureBlocks.Remove(block.Hash())
	return status, nil
}

func (bc *BlockChain) InsertChain(chain types.Blocks, callback ...func()) (int, error) {
	n, events, logs, err := bc.insertChain(chain, callback...)
	bc.PostChainEvents(events, logs)
	return n, err
}

func (bc *BlockChain) PreInsertChain(chain *types.Block) error {
	err := bc.preInsertChain(chain)
	return err
}

func (bc *BlockChain) InsertBlockToChain(chain types.Blocks, callback ...func()) (int, error) {
	n, events, logs, err := bc.insertChain(chain, callback...)
	bc.PostChainEvents(events, logs)
	return n, err
}

func (bc *BlockChain) preInsertChain(block *types.Block) error {
	bc.wg.Add(1)
	defer bc.wg.Done()

	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	var (
		stats = insertStats{startTime: mclock.Now()}
	)

	headers := make([]*types.Header, 0)

	log.Info("PreInsertBlock", "blockHeader", block.Header().Number)
	headers = append(headers, block.Header())
	log.Info("PreInsertBlock", "headers", headers)

	abort, results := bc.aoaEngine.VerifyHeaders(bc, headers)
	defer close(abort)

	if atomic.LoadInt32(&bc.procInterrupt) == 1 {
		log.Debug("Premature abort during blocks processing")
		return nil
	}

	if BadHashes[block.Hash()] {
		bc.reportBlock(block, nil, ErrBlacklistedHash)
		return ErrBlacklistedHash
	}

	err := <-results
	if err == nil {
		err = bc.Validator().ValidateBody(block)
	}
	if err != nil {
		if err == ErrKnownBlock {
			stats.ignored++
			return err
		}

		if err == consensus.ErrFutureBlock {

			max := big.NewInt(time.Now().Unix() + maxTimeFutureBlocks)
			if block.Time().Cmp(max) > 0 {
				return fmt.Errorf("future block: %v > %v", block.Time(), max)
			}
			bc.futureBlocks.Add(block.Hash(), block)
			stats.queued++
			return err
		}

		if err == consensus.ErrUnknownAncestor && bc.futureBlocks.Contains(block.ParentHash()) {
			bc.futureBlocks.Add(block.Hash(), block)
			stats.queued++
			return err
		}

		bc.reportBlock(block, nil, err)
		return err
	}

	var parent *types.Block
	parent = bc.GetBlock(block.ParentHash(), block.NumberU64()-1)
	state, err := state.New(parent.Root(), bc.stateCache)

	if err != nil {
		log.Info("vaildate block generate state error")
		return err
	}
	delegateState, err := delegatestate.New(parent.DelegateRoot(), bc.delegateCache)
	if err != nil {
		log.Info("vaildate block generate delegate state error")
		return err
	}

	receipts, _, usedGas, err := bc.processor.Process(block, state, bc.vmConfig, delegateState)
	if err != nil {
		log.Info("vaildate failed")
		bc.reportBlock(block, receipts, err)
		return err
	}

	err = bc.Validator().ValidateState(block, parent, state, receipts, usedGas, delegateState)
	if err != nil {
		bc.reportBlock(block, receipts, err)
		return err
	}

	return nil
}

func (bc *BlockChain) insertChain(chain types.Blocks, syncCallback ...func()) (int, []interface{}, []*types.Log, error) {
	log.Info("Insert chain start", "len", len(chain))

	for i := 1; i < len(chain); i++ {
		if chain[i].NumberU64() != chain[i-1].NumberU64()+1 || chain[i].ParentHash() != chain[i-1].Hash() {

			log.Error("Non contiguous block insert", "number", chain[i].Number(), "hash", chain[i].Hash(),
				"parent", chain[i].ParentHash(), "prevnumber", chain[i-1].Number(), "prevhash", chain[i-1].Hash())

			return 0, nil, nil, fmt.Errorf("non contiguous insert: item %d is #%d [%x…], item %d is #%d [%x…] (parent [%x…])", i-1, chain[i-1].NumberU64(),
				chain[i-1].Hash().Bytes()[:4], i, chain[i].NumberU64(), chain[i].Hash().Bytes()[:4], chain[i].ParentHash().Bytes()[:4])
		}
	}

	bc.wg.Add(1)
	defer bc.wg.Done()

	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	var (
		stats         = insertStats{startTime: mclock.Now()}
		events        = make([]interface{}, 0, len(chain))
		lastCanon     *types.Block
		coalescedLogs []*types.Log
	)

	headers := make([]*types.Header, len(chain))
	seals := make([]bool, len(chain))

	for i, block := range chain {
		headers[i] = block.Header()
		seals[i] = true
	}

	abort, results := bc.aoaEngine.VerifyHeaders(bc, headers)
	defer close(abort)

	for i, block := range chain {
		log.Debug("blockchain deal new block", "block", block.NumberU64(), "time", block.Header().Time)

		if atomic.LoadInt32(&bc.procInterrupt) == 1 {
			log.Debug("Premature abort during blocks processing")
			break
		}

		if BadHashes[block.Hash()] {
			bc.reportBlock(block, nil, ErrBlacklistedHash)
			return i, events, coalescedLogs, ErrBlacklistedHash
		}

		bstart := time.Now()

		err := <-results
		if err == nil {
			err = bc.Validator().ValidateBody(block)
		}
		log.Debug("blockchain Validate block body", "block", block.NumberU64(), "err", err)
		if err != nil {
			if err == ErrKnownBlock {
				log.Debug("blockchain", "err", "Already known block")
				stats.ignored++
				continue
			}

			if err == consensus.ErrFutureBlock {

				max := big.NewInt(time.Now().Unix() + maxTimeFutureBlocks)
				if block.Time().Cmp(max) > 0 {
					return i, events, coalescedLogs, fmt.Errorf("future block: %v > %v", block.Time(), max)
				}
				bc.futureBlocks.Add(block.Hash(), block)
				stats.queued++
				continue
			}

			if err == consensus.ErrUnknownAncestor && bc.futureBlocks.Contains(block.ParentHash()) {
				bc.futureBlocks.Add(block.Hash(), block)
				stats.queued++
				continue
			}

			bc.reportBlock(block, nil, err)
			return i, events, coalescedLogs, err
		}

		var parent *types.Block
		if i == 0 {
			parent = bc.GetBlock(block.ParentHash(), block.NumberU64()-1)
		} else {
			parent = chain[i-1]
		}
		stateDB, err := state.New(parent.Root(), bc.stateCache)
		if err != nil {
			log.Debug("Blockchain stateDB", "err", err)
			return i, events, coalescedLogs, err
		}
		delegateDB, err := delegatestate.New(parent.DelegateRoot(), bc.delegateCache)
		if err != nil {
			return i, events, coalescedLogs, err
		}

		log.Debug("blockchain process block start", "block", block.NumberU64())
		receipts, logs, usedGas, err := bc.processor.Process(block, stateDB, bc.vmConfig, delegateDB)
		log.Debug("blockchain process block end", "block", block.NumberU64(), "usedGas", usedGas, "blockGasUsed", block.GasUsed())
		if err != nil {
			bc.reportBlock(block, receipts, err)
			return i, events, coalescedLogs, err
		}

		err = bc.Validator().ValidateState(block, parent, stateDB, receipts, usedGas, delegateDB)
		if err != nil {
			bc.reportBlock(block, receipts, err)
			return i, events, coalescedLogs, err
		}

		status, err := bc.WriteBlockAndState(block, receipts, stateDB, delegateDB)
		log.Info("blockchain write block end", "block", block.NumberU64())
		if err != nil {
			return i, events, coalescedLogs, err
		}
		switch status {
		case CanonStatTy:
			log.Debug("Inserted new block", "number", block.Number(), "hash", block.Hash(),
				"txs", len(block.Transactions()), "gas", block.GasUsed(), "elapsed", common.PrettyDuration(time.Since(bstart)))

			coalescedLogs = append(coalescedLogs, logs...)
			blockInsertTimer.UpdateSince(bstart)
			events = append(events, ChainEvent{block,
				block.Hash(), logs})
			lastCanon = block

		case SideStatTy:
			log.Debug("Inserted forked block", "number", block.Number(), "hash", block.Hash(), "diff", types.BlockDifficult, "elapsed",
				common.PrettyDuration(time.Since(bstart)), "txs", len(block.Transactions()), "gas", block.GasUsed())

			blockInsertTimer.UpdateSince(bstart)
			events = append(events, ChainSideEvent{block})
		}
		stats.processed++
		stats.usedGas += usedGas
		stats.report(chain, i)
	}

	if lastCanon != nil && bc.LastBlockHash() == lastCanon.Hash() {
		events = append(events, ChainHeadEvent{lastCanon})
	}
	log.Debug("Insert chain end")
	return 0, events, coalescedLogs, nil
}

type insertStats struct {
	queued, processed, ignored int
	usedGas                    uint64
	lastIndex                  int
	startTime                  mclock.AbsTime
}

const statsReportLimit = 8 * time.Second

func (st *insertStats) report(chain []*types.Block, index int) bool {

	var (
		now     = mclock.Now()
		elapsed = time.Duration(now) - time.Duration(st.startTime)
	)

	if index == len(chain)-1 || elapsed >= statsReportLimit {
		var (
			end = chain[index]
			txs = countTransactions(chain[st.lastIndex : index+1])
		)
		context := []interface{}{
			"blocks", st.processed, "txs", txs, "mgas", float64(st.usedGas) / 1000000,
			"elapsed", common.PrettyDuration(elapsed), "mgasps", float64(st.usedGas) * 1000 / float64(elapsed),
			"number", end.Number(), "hash", end.Hash(),
		}
		if st.queued > 0 {
			context = append(context, []interface{}{"queued", st.queued}...)
		}
		if st.ignored > 0 {
			context = append(context, []interface{}{"ignored", st.ignored}...)
		}
		log.Info("Imported new chain segment", context...)

		*st = insertStats{startTime: now, lastIndex: index + 1}
		return true

	}
	return false
}

func countTransactions(chain []*types.Block) (c int) {
	for _, b := range chain {
		c += len(b.Transactions())
	}
	return c
}

func (bc *BlockChain) reorg(oldBlock, newBlock *types.Block) error {
	var (
		newChain    types.Blocks
		oldChain    types.Blocks
		commonBlock *types.Block
		deletedTxs  types.Transactions
		deletedLogs []*types.Log

		collectLogs = func(h common.Hash) {

			receipts := GetBlockReceipts(bc.chainDb, h, bc.hc.GetBlockNumber(h))
			for _, receipt := range receipts {
				for _, log := range receipt.Logs {
					del := *log
					del.Removed = true
					deletedLogs = append(deletedLogs, &del)
				}
			}
		}
	)

	if oldBlock.NumberU64() > newBlock.NumberU64() {

		for ; oldBlock != nil && oldBlock.NumberU64() != newBlock.NumberU64(); oldBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1) {
			oldChain = append(oldChain, oldBlock)
			deletedTxs = append(deletedTxs, oldBlock.Transactions()...)

			collectLogs(oldBlock.Hash())
		}
	} else {

		for ; newBlock != nil && newBlock.NumberU64() != oldBlock.NumberU64(); newBlock = bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1) {
			newChain = append(newChain, newBlock)
		}
	}
	if oldBlock == nil {
		return fmt.Errorf("Invalid old chain")
	}
	if newBlock == nil {
		return fmt.Errorf("Invalid new chain")
	}

	for {
		if oldBlock.Hash() == newBlock.Hash() {
			commonBlock = oldBlock
			break
		}

		oldChain = append(oldChain, oldBlock)
		newChain = append(newChain, newBlock)
		deletedTxs = append(deletedTxs, oldBlock.Transactions()...)
		collectLogs(oldBlock.Hash())

		oldBlock, newBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1), bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1)
		if oldBlock == nil {
			return fmt.Errorf("Invalid old chain")
		}
		if newBlock == nil {
			return fmt.Errorf("Invalid new chain")
		}
	}

	if len(oldChain) > 0 && len(newChain) > 0 {
		logFn := log.Debug
		if len(oldChain) > 63 {
			logFn = log.Warn
		}
		logFn("Chain split detected", "number", commonBlock.Number(), "hash", commonBlock.Hash(),
			"drop", len(oldChain), "dropfrom", oldChain[0].Hash(), "add", len(newChain), "addfrom", newChain[0].Hash())
	} else {
		log.Error("Impossible reorg, please file an issue", "oldnum", oldBlock.Number(), "oldhash", oldBlock.Hash(), "newnum", newBlock.Number(), "newhash", newBlock.Hash())
	}

	var addedTxs types.Transactions
	for i := len(newChain) - 1; i >= 0; i-- {

		bc.insert(newChain[i])

		if err := WriteTxLookupEntries(bc.chainDb, newChain[i]); err != nil {
			return err
		}
		addedTxs = append(addedTxs, newChain[i].Transactions()...)
	}

	diff := types.TxDifference(deletedTxs, addedTxs)

	for _, tx := range diff {
		DeleteTxLookupEntry(bc.chainDb, tx.Hash())
	}
	if len(deletedLogs) > 0 {
		go bc.rmLogsFeed.Send(RemovedLogsEvent{deletedLogs})
	}
	if len(oldChain) > 0 {
		go func() {
			for _, block := range oldChain {
				bc.chainSideFeed.Send(ChainSideEvent{Block: block})
			}
		}()
	}

	return nil
}

func (bc *BlockChain) PostChainEvents(events []interface{}, logs []*types.Log) {

	if logs != nil {
		bc.logsFeed.Send(logs)
	}
	for _, event := range events {
		switch ev := event.(type) {
		case ChainEvent:
			bc.chainFeed.Send(ev)

		case ChainHeadEvent:
			bc.chainHeadFeed.Send(ev)

		case ChainSideEvent:
			bc.chainSideFeed.Send(ev)
		}
	}
}

func (bc *BlockChain) update() {
	futureTimer := time.NewTicker(5 * time.Second)
	defer futureTimer.Stop()
	for {
		select {
		case <-futureTimer.C:
			bc.procFutureBlocks()
		case <-bc.quit:
			return
		}
	}
}

type BadBlockArgs struct {
	Hash   common.Hash   `json:"hash"`
	Header *types.Header `json:"header"`
}

func (bc *BlockChain) BadBlocks() ([]BadBlockArgs, error) {
	headers := make([]BadBlockArgs, 0, bc.badBlocks.Len())
	for _, hash := range bc.badBlocks.Keys() {
		if hdr, exist := bc.badBlocks.Peek(hash); exist {
			header := hdr.(*types.Header)
			headers = append(headers, BadBlockArgs{header.Hash(), header})
		}
	}
	return headers, nil
}

func (bc *BlockChain) addBadBlock(block *types.Block) {
	bc.badBlocks.Add(block.Header().Hash(), block.Header())
}

func (bc *BlockChain) reportBlock(block *types.Block, receipts types.Receipts, err error) {
	bc.addBadBlock(block)

	var receiptString string
	for _, receipt := range receipts {
		receiptString += fmt.Sprintf("\t%v\n", receipt)
	}
	log.Error(fmt.Sprintf(`
########## BAD BLOCK #########
Chain config: %v

Number: %v
Hash: 0x%x
%v

Error: %v
##############################
`, bc.config, block.Number(), block.Hash(), receiptString, err))
}

func (bc *BlockChain) InsertHeaderChain(chain []*types.Header, checkFreq int) (int, error) {
	start := time.Now()
	if i, err := bc.hc.ValidateHeaderChain(chain, checkFreq); err != nil {
		return i, err
	}

	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	bc.wg.Add(1)
	defer bc.wg.Done()

	whFunc := func(header *types.Header) error {
		bc.mu.Lock()
		defer bc.mu.Unlock()

		_, err := bc.hc.WriteHeader(header)
		return err
	}

	return bc.hc.InsertHeaderChain(chain, whFunc, start)
}

func (bc *BlockChain) writeHeader(header *types.Header) error {
	bc.wg.Add(1)
	defer bc.wg.Done()

	bc.mu.Lock()
	defer bc.mu.Unlock()

	_, err := bc.hc.WriteHeader(header)
	return err
}

func (bc *BlockChain) CurrentHeader() *types.Header {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return bc.hc.CurrentHeader()
}

func (bc *BlockChain) GetTd(hash common.Hash, number uint64) *big.Int {
	return bc.hc.GetTd(hash, number)
}

func (bc *BlockChain) GetTdByHash(hash common.Hash) *big.Int {
	return bc.hc.GetTdByHash(hash)
}

func (bc *BlockChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	return bc.hc.GetHeader(hash, number)
}

func (bc *BlockChain) GetHeaderByHash(hash common.Hash) *types.Header {
	return bc.hc.GetHeaderByHash(hash)
}

func (bc *BlockChain) HasHeader(hash common.Hash, number uint64) bool {
	return bc.hc.HasHeader(hash, number)
}

func (bc *BlockChain) GetBlockHashesFromHash(hash common.Hash, max uint64) []common.Hash {
	return bc.hc.GetBlockHashesFromHash(hash, max)
}

func (bc *BlockChain) GetHeaderByNumber(number uint64) *types.Header {
	return bc.hc.GetHeaderByNumber(number)
}

func (bc *BlockChain) GetCandidateWrapper() chan *types.CandidateWrapper {
	return bc.candidateWrapperChan
}

func (bc *BlockChain) GetDelegatePoll() (*map[common.Address]types.Candidate, error) {
	delegateRoot, err := bc.DelegateState()
	if err != nil {
		return nil, err
	}
	delegateList := delegateRoot.GetDelegates()
	res := make(map[common.Address]types.Candidate)
	for _, delegate := range delegateList {
		address := common.HexToAddress(delegate.Address)
		res[address] = delegate
	}
	return &res, nil
}

func (bc *BlockChain) GetGenesisConfig() *params.ChainConfig {
	return bc.config
}

func (bc *BlockChain) SetDelegatePoll(delegatePool *map[string]types.Candidate) {
	bc.delegateList = delegatePool
}

func (bc *BlockChain) Config() *params.ChainConfig { return bc.config }

func (bc *BlockChain) Engine() consensus.Engine { return bc.aoaEngine }

func (bc *BlockChain) SubscribeRemovedLogsEvent(ch chan<- RemovedLogsEvent) event.Subscription {
	return bc.scope.Track(bc.rmLogsFeed.Subscribe(ch))
}

func (bc *BlockChain) SubscribeChainEvent(ch chan<- ChainEvent) event.Subscription {
	return bc.scope.Track(bc.chainFeed.Subscribe(ch))
}

func (bc *BlockChain) SubscribeChainHeadEvent(ch chan<- ChainHeadEvent) event.Subscription {
	return bc.scope.Track(bc.chainHeadFeed.Subscribe(ch))
}

func (bc *BlockChain) SubscribeChainSideEvent(ch chan<- ChainSideEvent) event.Subscription {
	return bc.scope.Track(bc.chainSideFeed.Subscribe(ch))
}

func (bc *BlockChain) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return bc.scope.Track(bc.logsFeed.Subscribe(ch))
}

func (bc *BlockChain) GetStateDB() state.Database {
	return bc.stateCache
}

func (bc *BlockChain) GetInnerTxDb() watch.InnerTxDb {
	return bc.innerTxDb
}
