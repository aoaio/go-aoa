package types

import (
	"bytes"
	"fmt"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/rlp"
	"math/big"
	"reflect"
	"testing"
)

func TestBlockEncoding(t *testing.T) {
	blockEnc := common.FromHex("f901faf901f3a0d2e91d3554d254eb6a3db17ea03bc8d2af305eab483a777a23fd7181ba29b563948888f1f195afa192cfee860698584c030f4c9db1a00000000000000000000000000000000000000000000000000000000000000000a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421b9010000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000018261a880845afbaa3e8088000000000000000080a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000c0806180")
	var block Block
	if err := rlp.DecodeBytes(blockEnc, &block); err != nil {
		t.Fatal("block decode error: ", err)
	}

	check := func(f string, got, want interface{}) {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s mismatch: got %v, want %v", f, got, want)
		}
	}

	check("GasLimit", block.GasLimit(), uint64(25000))
	check("GasUsed", block.GasUsed(), uint64(0))
	check("Coinbase", block.Coinbase(), common.HexToAddress("0x8888f1f195afa192cfee860698584c030f4c9db1"))
	check("Root", block.Root(), common.HexToHash("0000000000000000000000000000000000000000000000000000000000000000"))
	check("DelegateRoot",block.DelegateRoot(),common.HexToHash("0000000000000000000000000000000000000000000000000000000000000000"))
	check("Hash", block.Hash(), common.HexToHash("0xa5d7262d4a1fc3bbb5090228cdaa6239b68cf54ee55741173f7a4f177363662c"))

	check("Time", block.Time(), big.NewInt(1526442558))
	check("Size", block.Size(), common.StorageSize(len(blockEnc)))

	ourBlockEnc, err := rlp.EncodeToBytes(&block)
	if err != nil {
		t.Fatal("encode error: ", err)
	}
	if !bytes.Equal(ourBlockEnc, blockEnc) {
		t.Errorf("encoded block mismatch:\ngot:  %x\nwant: %x", ourBlockEnc, blockEnc)
	}
}

func TestBlock_Hash(t *testing.T) {
	header := &Header{
		ParentHash: common.HexToHash("0xd2e91d3554d254eb6a3db17ea03bc8d2af305eab483a777a23fd7181ba29b563"),
		Number:     common.Big1,
		GasLimit:   25000,
		Extra:      nil,
		Time:       big.NewInt(1526442558),
		Coinbase:   common.HexToAddress("0x8888f1f195afa192cfee860698584c030f4c9db1"),
		AgentName:  nil,
	}
	block := NewBlock(header, nil, nil)

	rlpEncodes, err := rlp.EncodeToBytes(block)
	if err != nil {
		t.Fatalf("block rlp eocode error:%v", err)
	}
	fmt.Println(common.Bytes2Hex(rlpEncodes))
	fmt.Printf("block:%v\n", block.String())
	var data Block
	err = rlp.DecodeBytes(rlpEncodes, &data)
	if err != nil {
		t.Fatalf("block rlp decode error:%v", err)
	}
	fmt.Printf("without sign blockHash:%s\n", block.Hash().Hex())

	fmt.Printf("with sign blockHash:%s\n", block.Hash().Hex())

}
