package types

import (
	"encoding/binary"
	"fmt"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/hexutil"
	"github.com/Aurorachain/go-Aurora/crypto/sha3"
	"github.com/Aurorachain/go-Aurora/rlp"
	"io"
	"math/big"
	"sort"
	"sync/atomic"
	"time"
)

var (
	EmptyRootHash  = DeriveSha(Transactions{})
	BlockDifficult = common.Big1
)

type BlockNonce [8]byte

func EncodeNonce(i uint64) BlockNonce {
	var n BlockNonce
	binary.BigEndian.PutUint64(n[:], i)
	return n
}

func (n BlockNonce) Uint64() uint64 {
	return binary.BigEndian.Uint64(n[:])
}

func (n BlockNonce) MarshalText() ([]byte, error) {
	return hexutil.Bytes(n[:]).MarshalText()
}

func (n *BlockNonce) UnmarshalText(input []byte) error {
	return hexutil.UnmarshalFixedText("BlockNonce", input, n[:])
}

type Header struct {
	ParentHash common.Hash `json:"parentHash"       gencodec:"required"`

	Coinbase    common.Address `json:"miner"            gencodec:"required"`
	Root        common.Hash    `json:"stateRoot"        gencodec:"required"`
	TxHash      common.Hash    `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash common.Hash    `json:"receiptsRoot"     gencodec:"required"`
	Bloom       Bloom          `json:"logsBloom"        gencodec:"required"`

	Number   *big.Int `json:"number"           gencodec:"required"`
	GasLimit uint64   `json:"gasLimit"         gencodec:"required"`
	GasUsed  uint64   `json:"gasUsed"          gencodec:"required"`
	Time     *big.Int `json:"timestamp"        gencodec:"required"`
	Extra    []byte   `json:"extraData"        gencodec:"required"`

	AgentName          []byte      `json:"agentName"        gencodec:"required"`
	DelegateRoot       common.Hash `json:"delegateRoot"     gencodec:"required"`
	ShuffleHash        common.Hash `json:"shuffleHash"      gencodec:"required"`
	ShuffleBlockNumber *big.Int    `json:"shuffleBlockNumber"        gencodec:"required"`
}

type headerMarshaling struct {

	Number   *hexutil.Big
	GasLimit hexutil.Uint64
	GasUsed  hexutil.Uint64
	Time     *hexutil.Big
	Extra    hexutil.Bytes
	Hash     common.Hash `json:"hash"`
}

func (h *Header) Hash() common.Hash {
	return rlpHash(h)
}

func (h *Header) HashNoNonce() common.Hash {
	return rlpHash([]interface{}{
		h.ParentHash,
		h.Coinbase,
		h.Root,
		h.TxHash,
		h.ReceiptHash,
		h.Bloom,
		h.Number,
		h.GasLimit,
		h.GasUsed,
		h.Time,
		h.Extra,
		h.ShuffleHash,
		h.DelegateRoot,
		h.AgentName,
		h.ShuffleBlockNumber,
	})
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

type Body struct {
	Transactions []*Transaction

}

type Block struct {
	header       *Header
	transactions Transactions

	hash atomic.Value
	size atomic.Value

	td *big.Int

	ReceivedAt   time.Time
	ReceivedFrom interface{}
	Signature    []byte `json:"signature"        gencodec:"required"`
	RlpEncodeSigns []byte
}

func (b *Block) DeprecatedTd() *big.Int {
	return b.td
}

type StorageBlock Block

type extblock struct {
	Header         *Header
	Txs            []*Transaction
	Signature      []byte
}

type storageblock struct {
	Header         *Header
	Txs            []*Transaction
	TD             *big.Int
	Signature      []byte
}

func NewBlock(header *Header, txs []*Transaction, receipts []*Receipt) *Block {
	b := &Block{header: CopyHeader(header), td: new(big.Int)}

	if len(txs) == 0 {
		b.header.TxHash = EmptyRootHash
	} else {
		b.header.TxHash = DeriveSha(Transactions(txs))
		b.transactions = make(Transactions, len(txs))
		copy(b.transactions, txs)
	}

	if len(receipts) == 0 {
		b.header.ReceiptHash = EmptyRootHash
	} else {
		b.header.ReceiptHash = DeriveSha(Receipts(receipts))
		b.header.Bloom = CreateBloom(receipts)
	}
	return b
}

func NewBlockWithHeader(header *Header) *Block {
	return &Block{header: CopyHeader(header)}
}

func CopyHeader(h *Header) *Header {
	cpy := *h
	if cpy.Time = new(big.Int); h.Time != nil {
		cpy.Time.Set(h.Time)
	}

	if cpy.Number = new(big.Int); h.Number != nil {
		cpy.Number.Set(h.Number)
	}
	if len(h.Extra) > 0 {
		cpy.Extra = make([]byte, len(h.Extra))
		copy(cpy.Extra, h.Extra)
	}
	return &cpy
}

func (b *Block) DecodeRLP(s *rlp.Stream) error {
	var eb extblock
	_, size, _ := s.Kind()
	if err := s.Decode(&eb); err != nil {
		return err
	}
	b.header, b.transactions, b.Signature = eb.Header, eb.Txs, eb.Signature
	b.size.Store(common.StorageSize(rlp.ListSize(size)))
	return nil
}

func (b *Block) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, extblock{
		Header:    b.header,
		Txs:       b.transactions,
		Signature: b.Signature,
	})
}

func (b *StorageBlock) DecodeRLP(s *rlp.Stream) error {
	var sb storageblock
	if err := s.Decode(&sb); err != nil {
		return err
	}
	b.header, b.transactions, b.td, b.Signature = sb.Header, sb.Txs, sb.TD, sb.Signature
	return nil
}

func (b *Block) Transactions() Transactions { return b.transactions }

func (b *Block) Transaction(hash common.Hash) *Transaction {
	for _, transaction := range b.transactions {
		if transaction.Hash() == hash {
			return transaction
		}
	}
	return nil
}

func (b *Block) Number() *big.Int { return new(big.Int).Set(b.header.Number) }
func (b *Block) GasLimit() uint64 { return b.header.GasLimit }
func (b *Block) GasUsed() uint64  { return b.header.GasUsed }

func (b *Block) Time() *big.Int {
	return new(big.Int).Set(b.header.Time)
}

func (b *Block) NumberU64() uint64        { return b.header.Number.Uint64() }
func (b *Block) Bloom() Bloom             { return b.header.Bloom }
func (b *Block) Coinbase() common.Address { return b.header.Coinbase }
func (b *Block) Root() common.Hash        { return b.header.Root }
func (b *Block) DelegateRoot() common.Hash {
	return b.header.DelegateRoot
}
func (b *Block) ParentHash() common.Hash  { return b.header.ParentHash }
func (b *Block) TxHash() common.Hash      { return b.header.TxHash }
func (b *Block) ReceiptHash() common.Hash { return b.header.ReceiptHash }
func (b *Block) Extra() []byte            { return common.CopyBytes(b.header.Extra) }

func (b *Block) Header() *Header { return CopyHeader(b.header) }

func (b *Block) Body() *Body { return &Body{b.transactions} }

func (b *Block) HashNoNonce() common.Hash {
	return b.header.HashNoNonce()
}

func (b *Block) Size() common.StorageSize {
	if size := b.size.Load(); size != nil {
		return size.(common.StorageSize)
	}
	c := writeCounter(0)
	rlp.Encode(&c, b)
	b.size.Store(common.StorageSize(c))
	return common.StorageSize(c)
}

type writeCounter common.StorageSize

func (c *writeCounter) Write(b []byte) (int, error) {
	*c += writeCounter(len(b))
	return len(b), nil
}

func (b *Block) WithSeal(header *Header) *Block {
	cpy := *header

	return &Block{
		header:       &cpy,
		transactions: b.transactions,
	}
}

func (b *Block) WithBody(transactions []*Transaction) *Block {
	block := &Block{
		header:       CopyHeader(b.header),
		transactions: make([]*Transaction, len(transactions)),
	}
	copy(block.transactions, transactions)
	return block
}

func (b *Block) Hash() common.Hash {
	if hash := b.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	v := b.header.Hash()
	b.hash.Store(v)
	return v
}

func (b *Block) String() string {
	str := fmt.Sprintf(`Block(#%v): Size: %v {
MinerHash: %x
%v
Transactions:
%v
}
`, b.Number(), b.Size(), b.header.HashNoNonce(), b.header, b.transactions)
	return str
}

func (h *Header) String() string {
	return fmt.Sprintf(`Header(%x):
[
	ParentHash:	    %x
    Coinbase:	    %x
	Root:		    %x
	TxSha		    %x
	ReceiptSha:	    %x
	Bloom:		    %x
    Number:		    %v
	GasLimit:	    %v
	GasUsed:	    %v
	Time:		    %v
	Extra:		    %s
    AgentName:      %s
    DelegateRoot:   %x
    ShuffleHash:    %x
    ShuffleBlockNumber: %v
]`, h.Hash(), h.ParentHash, h.Coinbase, h.Root, h.TxHash, h.ReceiptHash, h.Bloom, h.Number, h.GasLimit, h.GasUsed, h.Time, h.Extra,h.AgentName, h.DelegateRoot, h.ShuffleHash, h.ShuffleBlockNumber)
}

type Blocks []*Block

type BlockBy func(b1, b2 *Block) bool

func (b BlockBy) Sort(blocks Blocks) {
	bs := blockSorter{
		blocks: blocks,
		by:     b,
	}
	sort.Sort(bs)
}

type blockSorter struct {
	blocks Blocks
	by     func(b1, b2 *Block) bool
}

func (b blockSorter) Len() int { return len(b.blocks) }
func (b blockSorter) Swap(i, j int) {
	b.blocks[i], b.blocks[j] = b.blocks[j], b.blocks[i]
}
func (b blockSorter) Less(i, j int) bool { return b.by(b.blocks[i], b.blocks[j]) }

func Number(b1, b2 *Block) bool { return b1.header.Number.Cmp(b2.header.Number) < 0 }
