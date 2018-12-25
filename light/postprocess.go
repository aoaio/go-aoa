package light

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/bitutil"
	"github.com/Aurorachain/go-Aurora/core"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/rlp"
	"github.com/Aurorachain/go-Aurora/trie"
)

const (
	ChtFrequency                   = 32768
	ChtV1Frequency                 = 4096
	HelperTrieConfirmations        = 2048
	HelperTrieProcessConfirmations = 256
)

type trustedCheckpoint struct {
	name                                string
	sectionIdx                          uint64
	sectionHead, chtRoot, bloomTrieRoot common.Hash
}

var (
	mainnetCheckpoint = trustedCheckpoint{
		name:          "ETH mainnet",
		sectionIdx:    150,
		sectionHead:   common.HexToHash("1e2e67f289565cbe7bd4367f7960dbd73a3f7c53439e1047cd7ba331c8109e39"),
		chtRoot:       common.HexToHash("f2a6c9ca143d647b44523cc249f1072c8912358ab873a77a5fdc792b8df99e80"),
		bloomTrieRoot: common.HexToHash("c018952fa1513c97857e79fbb9a37acaf8432d5b85e52a78eca7dff5fd5900ee"),
	}

	ropstenCheckpoint = trustedCheckpoint{
		name:          "Ropsten testnet",
		sectionIdx:    75,
		sectionHead:   common.HexToHash("12e68324f4578ea3e8e7fb3968167686729396c9279287fa1f1a8b51bb2d05b4"),
		chtRoot:       common.HexToHash("3e51dc095c69fa654a4cac766e0afff7357515b4b3c3a379c675f810363e54be"),
		bloomTrieRoot: common.HexToHash("33e3a70b33c1d73aa698d496a80615e98ed31fa8f56969876180553b32333339"),
	}
)

var trustedCheckpoints = map[common.Hash]trustedCheckpoint{
	params.MainnetGenesisHash: mainnetCheckpoint,
	params.TestnetGenesisHash: ropstenCheckpoint,
}

var (
	ErrNoTrustedCht       = errors.New("No trusted canonical hash trie")
	ErrNoTrustedBloomTrie = errors.New("No trusted bloom trie")
	ErrNoHeader           = errors.New("Header not found")
	chtPrefix             = []byte("chtRoot-")
	ChtTablePrefix        = "cht-"
)

type ChtNode struct {
	Hash common.Hash
	Td   *big.Int
}

func GetChtRoot(db aoadb.Database, sectionIdx uint64, sectionHead common.Hash) common.Hash {
	var encNumber [8]byte
	binary.BigEndian.PutUint64(encNumber[:], sectionIdx)
	data, _ := db.Get(append(append(chtPrefix, encNumber[:]...), sectionHead.Bytes()...))
	return common.BytesToHash(data)
}

func GetChtV2Root(db aoadb.Database, sectionIdx uint64, sectionHead common.Hash) common.Hash {
	return GetChtRoot(db, (sectionIdx+1)*(ChtFrequency/ChtV1Frequency)-1, sectionHead)
}

func StoreChtRoot(db aoadb.Database, sectionIdx uint64, sectionHead, root common.Hash) {
	var encNumber [8]byte
	binary.BigEndian.PutUint64(encNumber[:], sectionIdx)
	db.Put(append(append(chtPrefix, encNumber[:]...), sectionHead.Bytes()...), root.Bytes())
}

type ChtIndexerBackend struct {
	db, cdb              aoadb.Database
	section, sectionSize uint64
	lastHash             common.Hash
	trie                 *trie.Trie
}

func NewChtIndexer(db aoadb.Database, clientMode bool) *core.ChainIndexer {
	cdb := aoadb.NewTable(db, ChtTablePrefix)
	idb := aoadb.NewTable(db, "chtIndex-")
	var sectionSize, confirmReq uint64
	if clientMode {
		sectionSize = ChtFrequency
		confirmReq = HelperTrieConfirmations
	} else {
		sectionSize = ChtV1Frequency
		confirmReq = HelperTrieProcessConfirmations
	}
	return core.NewChainIndexer(db, idb, &ChtIndexerBackend{db: db, cdb: cdb, sectionSize: sectionSize}, sectionSize, confirmReq, time.Millisecond*100, "cht")
}

func (c *ChtIndexerBackend) Reset(section uint64, lastSectionHead common.Hash) error {
	var root common.Hash
	if section > 0 {
		root = GetChtRoot(c.db, section-1, lastSectionHead)
	}
	var err error
	c.trie, err = trie.New(root, c.cdb)
	c.section = section
	return err
}

func (c *ChtIndexerBackend) Process(header *types.Header) {
	hash, num := header.Hash(), header.Number.Uint64()
	c.lastHash = hash

	td := core.GetTd(c.db, hash, num)
	if td == nil {
		panic(nil)
	}
	var encNumber [8]byte
	binary.BigEndian.PutUint64(encNumber[:], num)
	data, _ := rlp.EncodeToBytes(ChtNode{hash, td})
	c.trie.Update(encNumber[:], data)
}

func (c *ChtIndexerBackend) Commit() error {
	batch := c.cdb.NewBatch()
	root, err := c.trie.CommitTo(batch)
	if err != nil {
		return err
	} else {
		batch.Write()
		if ((c.section+1)*c.sectionSize)%ChtFrequency == 0 {
			log.Info("Storing CHT", "idx", c.section*c.sectionSize/ChtFrequency, "sectionHead", fmt.Sprintf("%064x", c.lastHash), "root", fmt.Sprintf("%064x", root))
		}
		StoreChtRoot(c.db, c.section, c.lastHash, root)
	}
	return nil
}

const (
	BloomTrieFrequency        = 32768
	ethBloomBitsSection       = 4096
	ethBloomBitsConfirmations = 256
)

var (
	bloomTriePrefix      = []byte("bltRoot-")
	BloomTrieTablePrefix = "blt-"
)

func GetBloomTrieRoot(db aoadb.Database, sectionIdx uint64, sectionHead common.Hash) common.Hash {
	var encNumber [8]byte
	binary.BigEndian.PutUint64(encNumber[:], sectionIdx)
	data, _ := db.Get(append(append(bloomTriePrefix, encNumber[:]...), sectionHead.Bytes()...))
	return common.BytesToHash(data)
}

func StoreBloomTrieRoot(db aoadb.Database, sectionIdx uint64, sectionHead, root common.Hash) {
	var encNumber [8]byte
	binary.BigEndian.PutUint64(encNumber[:], sectionIdx)
	db.Put(append(append(bloomTriePrefix, encNumber[:]...), sectionHead.Bytes()...), root.Bytes())
}

type BloomTrieIndexerBackend struct {
	db, cdb                                    aoadb.Database
	section, parentSectionSize, bloomTrieRatio uint64
	trie                                       *trie.Trie
	sectionHeads                               []common.Hash
}

func NewBloomTrieIndexer(db aoadb.Database, clientMode bool) *core.ChainIndexer {
	cdb := aoadb.NewTable(db, BloomTrieTablePrefix)
	idb := aoadb.NewTable(db, "bltIndex-")
	backend := &BloomTrieIndexerBackend{db: db, cdb: cdb}
	var confirmReq uint64
	if clientMode {
		backend.parentSectionSize = BloomTrieFrequency
		confirmReq = HelperTrieConfirmations
	} else {
		backend.parentSectionSize = ethBloomBitsSection
		confirmReq = HelperTrieProcessConfirmations
	}
	backend.bloomTrieRatio = BloomTrieFrequency / backend.parentSectionSize
	backend.sectionHeads = make([]common.Hash, backend.bloomTrieRatio)
	return core.NewChainIndexer(db, idb, backend, BloomTrieFrequency, confirmReq-ethBloomBitsConfirmations, time.Millisecond*100, "bloomtrie")
}

func (b *BloomTrieIndexerBackend) Reset(section uint64, lastSectionHead common.Hash) error {
	var root common.Hash
	if section > 0 {
		root = GetBloomTrieRoot(b.db, section-1, lastSectionHead)
	}
	var err error
	b.trie, err = trie.New(root, b.cdb)
	b.section = section
	return err
}

func (b *BloomTrieIndexerBackend) Process(header *types.Header) {
	num := header.Number.Uint64() - b.section*BloomTrieFrequency
	if (num+1)%b.parentSectionSize == 0 {
		b.sectionHeads[num/b.parentSectionSize] = header.Hash()
	}
}

func (b *BloomTrieIndexerBackend) Commit() error {
	var compSize, decompSize uint64

	for i := uint(0); i < types.BloomBitLength; i++ {
		var encKey [10]byte
		binary.BigEndian.PutUint16(encKey[0:2], uint16(i))
		binary.BigEndian.PutUint64(encKey[2:10], b.section)
		var decomp []byte
		for j := uint64(0); j < b.bloomTrieRatio; j++ {
			data, err := core.GetBloomBits(b.db, i, b.section*b.bloomTrieRatio+j, b.sectionHeads[j])
			if err != nil {
				return err
			}
			decompData, err2 := bitutil.DecompressBytes(data, int(b.parentSectionSize/8))
			if err2 != nil {
				return err2
			}
			decomp = append(decomp, decompData...)
		}
		comp := bitutil.CompressBytes(decomp)

		decompSize += uint64(len(decomp))
		compSize += uint64(len(comp))
		if len(comp) > 0 {
			b.trie.Update(encKey[:], comp)
		} else {
			b.trie.Delete(encKey[:])
		}
	}

	batch := b.cdb.NewBatch()
	root, err := b.trie.CommitTo(batch)
	if err != nil {
		return err
	} else {
		batch.Write()
		sectionHead := b.sectionHeads[b.bloomTrieRatio-1]
		log.Info("Storing BloomTrie", "section", b.section, "sectionHead", fmt.Sprintf("%064x", sectionHead), "root", fmt.Sprintf("%064x", root), "compression ratio", float64(compSize)/float64(decompSize))
		StoreBloomTrieRoot(b.db, b.section, sectionHead, root)
	}

	return nil
}
