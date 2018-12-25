package core

import (
	crand "crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	mrand "math/rand"
	"time"

	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/consensus"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/hashicorp/golang-lru"
)

const (
	headerCacheLimit = 512
	tdCacheLimit     = 1024
	numberCacheLimit = 2048
)

type HeaderChain struct {
	config *params.ChainConfig

	chainDb       aoadb.Database
	genesisHeader *types.Header

	currentHeader     *types.Header
	currentHeaderHash common.Hash

	headerCache *lru.Cache
	tdCache     *lru.Cache
	numberCache *lru.Cache

	procInterrupt func() bool

	rand   *mrand.Rand
	engine consensus.Engine
}

func NewHeaderChain(chainDb aoadb.Database, config *params.ChainConfig, engine consensus.Engine, procInterrupt func() bool) (*HeaderChain, error) {
	headerCache, _ := lru.New(headerCacheLimit)
	tdCache, _ := lru.New(tdCacheLimit)
	numberCache, _ := lru.New(numberCacheLimit)

	seed, err := crand.Int(crand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		return nil, err
	}

	hc := &HeaderChain{
		config:        config,
		chainDb:       chainDb,
		headerCache:   headerCache,
		tdCache:       tdCache,
		numberCache:   numberCache,
		procInterrupt: procInterrupt,
		rand:          mrand.New(mrand.NewSource(seed.Int64())),
		engine:        engine,
	}

	hc.genesisHeader = hc.GetHeaderByNumber(0)
	if hc.genesisHeader == nil {
		return nil, ErrNoGenesis
	}

	hc.currentHeader = hc.genesisHeader
	if head := GetHeadBlockHash(chainDb); head != (common.Hash{}) {
		if chead := hc.GetHeaderByHash(head); chead != nil {
			hc.currentHeader = chead
		}
	}
	hc.currentHeaderHash = hc.currentHeader.Hash()

	return hc, nil
}

func (hc *HeaderChain) GetBlockNumber(hash common.Hash) uint64 {
	if cached, ok := hc.numberCache.Get(hash); ok {
		return cached.(uint64)
	}
	number := GetBlockNumber(hc.chainDb, hash)
	if number != missingNumber {
		hc.numberCache.Add(hash, number)
	}
	return number
}

func (hc *HeaderChain) WriteHeader(header *types.Header) (status WriteStatus, err error) {

	var (
		hash   = header.Hash()
		number = header.Number.Uint64()
	)

	ptd := hc.GetTd(header.ParentHash, number-1)
	if ptd == nil {
		return NonStatTy, consensus.ErrUnknownAncestor
	}
	localTd := hc.GetTd(hc.currentHeaderHash, hc.currentHeader.Number.Uint64())
	externTd := new(big.Int).Add(types.BlockDifficult, ptd)

	if err := hc.WriteTd(hash, number, externTd); err != nil {
		log.Crit("Failed to write header total difficulty", "err", err)
	}
	if err := WriteHeader(hc.chainDb, header); err != nil {
		log.Crit("Failed to write header content", "err", err)
	}

	if externTd.Cmp(localTd) > 0 || (externTd.Cmp(localTd) == 0 && mrand.Float64() < 0.5) {

		for i := number + 1; ; i++ {
			hash := GetCanonicalHash(hc.chainDb, i)
			if hash == (common.Hash{}) {
				break
			}
			DeleteCanonicalHash(hc.chainDb, i)
		}

		var (
			headHash   = header.ParentHash
			headNumber = header.Number.Uint64() - 1
			headHeader = hc.GetHeader(headHash, headNumber)
		)
		for GetCanonicalHash(hc.chainDb, headNumber) != headHash {
			WriteCanonicalHash(hc.chainDb, headHash, headNumber)

			headHash = headHeader.ParentHash
			headNumber = headHeader.Number.Uint64() - 1
			headHeader = hc.GetHeader(headHash, headNumber)
		}

		if err := WriteCanonicalHash(hc.chainDb, hash, number); err != nil {
			log.Crit("Failed to insert header number", "err", err)
		}
		if err := WriteHeadHeaderHash(hc.chainDb, hash); err != nil {
			log.Crit("Failed to insert head header hash", "err", err)
		}
		hc.currentHeaderHash, hc.currentHeader = hash, types.CopyHeader(header)

		status = CanonStatTy
	} else {
		status = SideStatTy
	}

	hc.headerCache.Add(hash, header)
	hc.numberCache.Add(hash, number)

	return
}

type WhCallback func(*types.Header) error

func (hc *HeaderChain) ValidateHeaderChain(chain []*types.Header, checkFreq int) (int, error) {

	for i := 1; i < len(chain); i++ {
		if chain[i].Number.Uint64() != chain[i-1].Number.Uint64()+1 || chain[i].ParentHash != chain[i-1].Hash() {

			log.Error("Non contiguous header insert", "number", chain[i].Number, "hash", chain[i].Hash(),
				"parent", chain[i].ParentHash, "prevnumber", chain[i-1].Number, "prevhash", chain[i-1].Hash())

			return 0, fmt.Errorf("non contiguous insert: item %d is #%d [%x…], item %d is #%d [%x…] (parent [%x…])", i-1, chain[i-1].Number,
				chain[i-1].Hash().Bytes()[:4], i, chain[i].Number, chain[i].Hash().Bytes()[:4], chain[i].ParentHash[:4])
		}
	}

	abort, results := hc.engine.VerifyHeaders(hc, chain)
	defer close(abort)

	for i, header := range chain {

		if hc.procInterrupt() {
			log.Debug("Premature abort during headers verification")
			return 0, errors.New("aborted")
		}

		if BadHashes[header.Hash()] {
			return i, ErrBlacklistedHash
		}

		if err := <-results; err != nil {
			return i, err
		}
	}

	return 0, nil
}

func (hc *HeaderChain) InsertHeaderChain(chain []*types.Header, writeHeader WhCallback, start time.Time) (int, error) {

	stats := struct{ processed, ignored int }{}

	for i, header := range chain {

		if hc.procInterrupt() {
			log.Debug("Premature abort during headers import")
			return i, errors.New("aborted")
		}

		if hc.HasHeader(header.Hash(), header.Number.Uint64()) {
			stats.ignored++
			continue
		}
		if err := writeHeader(header); err != nil {
			return i, err
		}
		stats.processed++
	}

	last := chain[len(chain)-1]
	log.Info("Imported new block headers", "count", stats.processed, "elapsed", common.PrettyDuration(time.Since(start)),
		"number", last.Number, "hash", last.Hash(), "ignored", stats.ignored)

	return 0, nil
}

func (hc *HeaderChain) GetBlockHashesFromHash(hash common.Hash, max uint64) []common.Hash {

	header := hc.GetHeaderByHash(hash)
	if header == nil {
		return nil
	}

	chain := make([]common.Hash, 0, max)
	for i := uint64(0); i < max; i++ {
		next := header.ParentHash
		if header = hc.GetHeader(next, header.Number.Uint64()-1); header == nil {
			break
		}
		chain = append(chain, next)
		if header.Number.Sign() == 0 {
			break
		}
	}
	return chain
}

func (hc *HeaderChain) GetTd(hash common.Hash, number uint64) *big.Int {

	if cached, ok := hc.tdCache.Get(hash); ok {
		return cached.(*big.Int)
	}
	td := GetTd(hc.chainDb, hash, number)
	if td == nil {
		return nil
	}

	hc.tdCache.Add(hash, td)
	return td
}

func (hc *HeaderChain) GetTdByHash(hash common.Hash) *big.Int {
	return hc.GetTd(hash, hc.GetBlockNumber(hash))
}

func (hc *HeaderChain) WriteTd(hash common.Hash, number uint64, td *big.Int) error {
	if err := WriteTd(hc.chainDb, hash, number, td); err != nil {
		return err
	}
	hc.tdCache.Add(hash, new(big.Int).Set(td))
	return nil
}

func (hc *HeaderChain) GetHeader(hash common.Hash, number uint64) *types.Header {

	if header, ok := hc.headerCache.Get(hash); ok {
		return header.(*types.Header)
	}
	header := GetHeader(hc.chainDb, hash, number)
	if header == nil {
		return nil
	}

	hc.headerCache.Add(hash, header)
	return header
}

func (hc *HeaderChain) GetHeaderByHash(hash common.Hash) *types.Header {
	return hc.GetHeader(hash, hc.GetBlockNumber(hash))
}

func (hc *HeaderChain) HasHeader(hash common.Hash, number uint64) bool {
	if hc.numberCache.Contains(hash) || hc.headerCache.Contains(hash) {
		return true
	}
	ok, _ := hc.chainDb.Has(headerKey(hash, number))
	return ok
}

func (hc *HeaderChain) GetHeaderByNumber(number uint64) *types.Header {
	hash := GetCanonicalHash(hc.chainDb, number)
	if hash == (common.Hash{}) {
		return nil
	}
	return hc.GetHeader(hash, number)
}

func (hc *HeaderChain) CurrentHeader() *types.Header {
	return hc.currentHeader
}

func (hc *HeaderChain) SetCurrentHeader(head *types.Header) {
	if err := WriteHeadHeaderHash(hc.chainDb, head.Hash()); err != nil {
		log.Crit("Failed to insert head header hash", "err", err)
	}
	hc.currentHeader = head
	hc.currentHeaderHash = head.Hash()
}

type DeleteCallback func(common.Hash, uint64)

func (hc *HeaderChain) SetHead(head uint64, delFn DeleteCallback) {
	height := uint64(0)
	if hc.currentHeader != nil {
		height = hc.currentHeader.Number.Uint64()
	}

	for hc.currentHeader != nil && hc.currentHeader.Number.Uint64() > head {
		hash := hc.currentHeader.Hash()
		num := hc.currentHeader.Number.Uint64()
		if delFn != nil {
			delFn(hash, num)
		}
		DeleteHeader(hc.chainDb, hash, num)
		DeleteTd(hc.chainDb, hash, num)
		hc.currentHeader = hc.GetHeader(hc.currentHeader.ParentHash, hc.currentHeader.Number.Uint64()-1)
	}

	for i := height; i > head; i-- {
		DeleteCanonicalHash(hc.chainDb, i)
	}

	hc.headerCache.Purge()
	hc.tdCache.Purge()
	hc.numberCache.Purge()

	if hc.currentHeader == nil {
		hc.currentHeader = hc.genesisHeader
	}
	hc.currentHeaderHash = hc.currentHeader.Hash()

	if err := WriteHeadHeaderHash(hc.chainDb, hc.currentHeaderHash); err != nil {
		log.Crit("Failed to reset head header hash", "err", err)
	}
}

func (hc *HeaderChain) SetGenesis(head *types.Header) {
	hc.genesisHeader = head
}

func (hc *HeaderChain) Config() *params.ChainConfig { return hc.config }

func (hc *HeaderChain) Engine() consensus.Engine { return hc.engine }

func (hc *HeaderChain) GetBlock(hash common.Hash, number uint64) *types.Block {
	return nil
}
