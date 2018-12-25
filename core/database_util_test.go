package core

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/crypto/sha3"
	"github.com/Aurorachain/go-Aurora/rlp"
	"time"
	"fmt"
)

type Candidate struct {
	Address string

	Vote      uint64 
	Nickname  string 
	PublicKey []byte
}

type candidateData struct {
	Votes           []Candidate
	LastBlockHeight uint64
}

func TestHeaderStorage(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()

	header := &types.Header{Number: big.NewInt(42), Extra: []byte("test header")}
	if entry := GetHeader(db, header.Hash(), header.Number.Uint64()); entry != nil {
		t.Fatalf("Non existent header returned: %v", entry)
	}

	if err := WriteHeader(db, header); err != nil {
		t.Fatalf("Failed to write header into database: %v", err)
	}
	if entry := GetHeader(db, header.Hash(), header.Number.Uint64()); entry == nil {
		t.Fatalf("Stored header not found")
	} else if entry.Hash() != header.Hash() {
		t.Fatalf("Retrieved header mismatch: have %v, want %v", entry, header)
	}
	if entry := GetHeaderRLP(db, header.Hash(), header.Number.Uint64()); entry == nil {
		t.Fatalf("Stored header RLP not found")
	} else {
		hasher := sha3.NewKeccak256()
		hasher.Write(entry)

		if hash := common.BytesToHash(hasher.Sum(nil)); hash != header.Hash() {
			t.Fatalf("Retrieved RLP header mismatch: have %v, want %v", entry, header)
		}
	}

	DeleteHeader(db, header.Hash(), header.Number.Uint64())
	if entry := GetHeader(db, header.Hash(), header.Number.Uint64()); entry != nil {
		t.Fatalf("Deleted header returned: %v", entry)
	}
}

func TestBodyStorage(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()

	body := &types.Body{}

	hasher := sha3.NewKeccak256()
	rlp.Encode(hasher, body)
	hash := common.BytesToHash(hasher.Sum(nil))

	if entry := GetBody(db, hash, 0); entry != nil {
		t.Fatalf("Non existent body returned: %v", entry)
	}

	if err := WriteBody(db, hash, 0, body); err != nil {
		t.Fatalf("Failed to write body into database: %v", err)
	}
	if entry := GetBody(db, hash, 0); entry == nil {
		t.Fatalf("Stored body not found")
	} else if types.DeriveSha(types.Transactions(entry.Transactions)) != types.DeriveSha(types.Transactions(body.Transactions)) {
		t.Fatalf("Retrieved body mismatch: have %v, want %v", entry, body)
	}
	if entry := GetBodyRLP(db, hash, 0); entry == nil {
		t.Fatalf("Stored body RLP not found")
	} else {
		hasher := sha3.NewKeccak256()
		hasher.Write(entry)

		if calc := common.BytesToHash(hasher.Sum(nil)); calc != hash {
			t.Fatalf("Retrieved RLP body mismatch: have %v, want %v", entry, body)
		}
	}

	DeleteBody(db, hash, 0)
	if entry := GetBody(db, hash, 0); entry != nil {
		t.Fatalf("Deleted body returned: %v", entry)
	}
}

func TestBlockStorage(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()

	block := types.NewBlockWithHeader(&types.Header{
		Extra:       []byte("test block"),
		TxHash:      types.EmptyRootHash,
		ReceiptHash: types.EmptyRootHash,
	})
	if entry := GetBlock(db, block.Hash(), block.NumberU64()); entry != nil {
		t.Fatalf("Non existent block returned: %v", entry)
	}
	if entry := GetHeader(db, block.Hash(), block.NumberU64()); entry != nil {
		t.Fatalf("Non existent header returned: %v", entry)
	}
	if entry := GetBody(db, block.Hash(), block.NumberU64()); entry != nil {
		t.Fatalf("Non existent body returned: %v", entry)
	}

	if err := WriteBlock(db, block); err != nil {
		t.Fatalf("Failed to write block into database: %v", err)
	}
	if entry := GetBlock(db, block.Hash(), block.NumberU64()); entry == nil {
		t.Fatalf("Stored block not found")
	} else if entry.Hash() != block.Hash() {
		t.Fatalf("Retrieved block mismatch: have %v, want %v", entry, block)
	}
	if entry := GetHeader(db, block.Hash(), block.NumberU64()); entry == nil {
		t.Fatalf("Stored header not found")
	} else if entry.Hash() != block.Header().Hash() {
		t.Fatalf("Retrieved header mismatch: have %v, want %v", entry, block.Header())
	}
	if entry := GetBody(db, block.Hash(), block.NumberU64()); entry == nil {
		t.Fatalf("Stored body not found")
	} else if types.DeriveSha(types.Transactions(entry.Transactions)) != types.DeriveSha(block.Transactions()) {
		t.Fatalf("Retrieved body mismatch: have %v, want %v", entry, block.Body())
	}

	DeleteBlock(db, block.Hash(), block.NumberU64())
	if entry := GetBlock(db, block.Hash(), block.NumberU64()); entry != nil {
		t.Fatalf("Deleted block returned: %v", entry)
	}
	if entry := GetHeader(db, block.Hash(), block.NumberU64()); entry != nil {
		t.Fatalf("Deleted header returned: %v", entry)
	}
	if entry := GetBody(db, block.Hash(), block.NumberU64()); entry != nil {
		t.Fatalf("Deleted body returned: %v", entry)
	}
}

func TestPartialBlockStorage(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()
	block := types.NewBlockWithHeader(&types.Header{
		Extra:       []byte("test block"),
		TxHash:      types.EmptyRootHash,
		ReceiptHash: types.EmptyRootHash,
	})

	if err := WriteHeader(db, block.Header()); err != nil {
		t.Fatalf("Failed to write header into database: %v", err)
	}
	if entry := GetBlock(db, block.Hash(), block.NumberU64()); entry != nil {
		t.Fatalf("Non existent block returned: %v", entry)
	}
	DeleteHeader(db, block.Hash(), block.NumberU64())

	if err := WriteBody(db, block.Hash(), block.NumberU64(), block.Body()); err != nil {
		t.Fatalf("Failed to write body into database: %v", err)
	}
	if entry := GetBlock(db, block.Hash(), block.NumberU64()); entry != nil {
		t.Fatalf("Non existent block returned: %v", entry)
	}
	DeleteBody(db, block.Hash(), block.NumberU64())

	if err := WriteHeader(db, block.Header()); err != nil {
		t.Fatalf("Failed to write header into database: %v", err)
	}
	if err := WriteBody(db, block.Hash(), block.NumberU64(), block.Body()); err != nil {
		t.Fatalf("Failed to write body into database: %v", err)
	}
	if entry := GetBlock(db, block.Hash(), block.NumberU64()); entry == nil {
		t.Fatalf("Stored block not found")
	} else if entry.Hash() != block.Hash() {
		t.Fatalf("Retrieved block mismatch: have %v, want %v", entry, block)
	}
}

func TestTdStorage(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()

	hash, td := common.Hash{}, big.NewInt(314)
	if entry := GetTd(db, hash, 0); entry != nil {
		t.Fatalf("Non existent TD returned: %v", entry)
	}

	if err := WriteTd(db, hash, 0, td); err != nil {
		t.Fatalf("Failed to write TD into database: %v", err)
	}
	if entry := GetTd(db, hash, 0); entry == nil {
		t.Fatalf("Stored TD not found")
	} else if entry.Cmp(td) != 0 {
		t.Fatalf("Retrieved TD mismatch: have %v, want %v", entry, td)
	}

	DeleteTd(db, hash, 0)
	if entry := GetTd(db, hash, 0); entry != nil {
		t.Fatalf("Deleted TD returned: %v", entry)
	}
}

func TestCanonicalMappingStorage(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()

	hash, number := common.Hash{0: 0xff}, uint64(314)
	if entry := GetCanonicalHash(db, number); entry != (common.Hash{}) {
		t.Fatalf("Non existent canonical mapping returned: %v", entry)
	}

	if err := WriteCanonicalHash(db, hash, number); err != nil {
		t.Fatalf("Failed to write canonical mapping into database: %v", err)
	}
	if entry := GetCanonicalHash(db, number); entry == (common.Hash{}) {
		t.Fatalf("Stored canonical mapping not found")
	} else if entry != hash {
		t.Fatalf("Retrieved canonical mapping mismatch: have %v, want %v", entry, hash)
	}

	DeleteCanonicalHash(db, number)
	if entry := GetCanonicalHash(db, number); entry != (common.Hash{}) {
		t.Fatalf("Deleted canonical mapping returned: %v", entry)
	}
}

func TestHeadStorage(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()

	blockHead := types.NewBlockWithHeader(&types.Header{Extra: []byte("test block header")})
	blockFull := types.NewBlockWithHeader(&types.Header{Extra: []byte("test block full")})
	blockFast := types.NewBlockWithHeader(&types.Header{Extra: []byte("test block fast")})

	if entry := GetHeadHeaderHash(db); entry != (common.Hash{}) {
		t.Fatalf("Non head header entry returned: %v", entry)
	}
	if entry := GetHeadBlockHash(db); entry != (common.Hash{}) {
		t.Fatalf("Non head block entry returned: %v", entry)
	}
	if entry := GetHeadFastBlockHash(db); entry != (common.Hash{}) {
		t.Fatalf("Non fast head block entry returned: %v", entry)
	}

	if err := WriteHeadHeaderHash(db, blockHead.Hash()); err != nil {
		t.Fatalf("Failed to write head header hash: %v", err)
	}
	if err := WriteHeadBlockHash(db, blockFull.Hash()); err != nil {
		t.Fatalf("Failed to write head block hash: %v", err)
	}
	if err := WriteHeadFastBlockHash(db, blockFast.Hash()); err != nil {
		t.Fatalf("Failed to write fast head block hash: %v", err)
	}

	if entry := GetHeadHeaderHash(db); entry != blockHead.Hash() {
		t.Fatalf("Head header hash mismatch: have %v, want %v", entry, blockHead.Hash())
	}
	if entry := GetHeadBlockHash(db); entry != blockFull.Hash() {
		t.Fatalf("Head block hash mismatch: have %v, want %v", entry, blockFull.Hash())
	}
	if entry := GetHeadFastBlockHash(db); entry != blockFast.Hash() {
		t.Fatalf("Fast head block hash mismatch: have %v, want %v", entry, blockFast.Hash())
	}
}

func TestLookupStorage(t *testing.T) {

}

func TestBlockReceiptStorage(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()

	receipt1 := &types.Receipt{
		Status:            types.ReceiptStatusFailed,
		CumulativeGasUsed: 1,
		Logs: []*types.Log{
			{Address: common.BytesToAddress([]byte{0x11})},
			{Address: common.BytesToAddress([]byte{0x01, 0x11})},
		},
		TxHash:          common.BytesToHash([]byte{0x11, 0x11}),
		ContractAddress: common.BytesToAddress([]byte{0x01, 0x11, 0x11}),
		GasUsed:         111111,
	}
	receipt2 := &types.Receipt{
		PostState:         common.Hash{2}.Bytes(),
		CumulativeGasUsed: 2,
		Logs: []*types.Log{
			{Address: common.BytesToAddress([]byte{0x22})},
			{Address: common.BytesToAddress([]byte{0x02, 0x22})},
		},
		TxHash:          common.BytesToHash([]byte{0x22, 0x22}),
		ContractAddress: common.BytesToAddress([]byte{0x02, 0x22, 0x22}),
		GasUsed:         222222,
	}
	receipts := []*types.Receipt{receipt1, receipt2}

	hash := common.BytesToHash([]byte{0x03, 0x14})
	if rs := GetBlockReceipts(db, hash, 0); len(rs) != 0 {
		t.Fatalf("non existent receipts returned: %v", rs)
	}

	if err := WriteBlockReceipts(db, hash, 0, receipts); err != nil {
		t.Fatalf("failed to write block receipts: %v", err)
	}
	if rs := GetBlockReceipts(db, hash, 0); len(rs) == 0 {
		t.Fatalf("no receipts returned")
	} else {
		for i := 0; i < len(receipts); i++ {
			rlpHave, _ := rlp.EncodeToBytes(rs[i])
			rlpWant, _ := rlp.EncodeToBytes(receipts[i])

			if !bytes.Equal(rlpHave, rlpWant) {
				t.Fatalf("receipt #%d: receipt mismatch: have %v, want %v", i, rs[i], receipts[i])
			}
		}
	}

	DeleteBlockReceipts(db, hash, 0)
	if rs := GetBlockReceipts(db, hash, 0); len(rs) != 0 {
		t.Fatalf("deleted receipts returned: %v", rs)
	}
}

func TestWriteDelegateBodyRLP(t *testing.T) {

	db, _ := aoadb.NewLDBDatabase("234", 0, 0)
	can := []Candidate{
		{"0x70715a2a44255ddce2779d60ba95968b770fc759", uint64(2), "node1", nil},
		{"0xfd48a829397a16b3bc6c319a06a47cd2ce6b3f58", uint64(3), "node2", nil},
	}

	data, err := rlp.EncodeToBytes(candidateData{can, 12})
	if err != nil {
		t.Fatalf("failed to rlp encode", err)
	}
	err = WriteDelegateBodyRLP(db, data)
	if err != nil {
		t.Fatalf("failed to store one", err)
	}

	get, err := db.Get([]byte(datagateDataPrefix))
	var oneData candidateData
	err = rlp.DecodeBytes(get, &oneData)
	t.Log(oneData)
	if err != nil {
		t.Fatalf("failed to rlp decode ", err)
	}
}

func TestWriteDelegateShuffleBlockHeightRLP(t *testing.T) {
	db, _ := aoadb.NewLDBDatabase("456", 0, 0)

	shuffleDelegateData := types.ShuffleDelegateData{BlockNumber: *big.NewInt(2), ShuffleTime: *big.NewInt(time.Now().Unix())}
	data, err := rlp.EncodeToBytes(shuffleDelegateData)
	if err != nil {
		t.Fatalf("failed to rlp encode", err)
	}
	err = WriteDelegateShuffleBlockHeightRLP(db, data)
	if err != nil {
		t.Fatalf("failed to store one", err)
	}
	get, err := db.Get([]byte(delegateStorePrefix))
	var data2 types.ShuffleDelegateData
	err = rlp.DecodeBytes(get, &data2)
	if err != nil {
		t.Fatalf("failed to rlp decode ", err)
	}
	fmt.Println(data2)
	fmt.Println(data2.BlockNumber.Int64())
	fmt.Println(data2.ShuffleTime.Int64())
}
