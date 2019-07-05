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
	"math/big"
	"reflect"
	"testing"

	"encoding/json"
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/davecgh/go-spew/spew"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common/hexutil"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/params"
	"io/ioutil"
	"os"
	"github.com/Aurorachain/go-aoa/rlp"
	"strconv"
)

func TestDefaultGenesisBlock(t *testing.T) {
	file, _ := os.Open("/Users/name/workspace/go-workspace/github-workspace/src/github.com/Aurorachain/go-aoa/genesis.json")
	data, _ := ioutil.ReadFile("/Users/name/workspace/go-workspace/github-workspace/src/github.com/Aurorachain/go-aoa/genesis.json")
	geneJson := string(data)
	fmt.Println(geneJson)
	var aa Genesis
	json.Unmarshal([]byte(geneJson), &aa)
	json.NewDecoder(file).Decode(&aa)

	//aa.UnmarshalJSON(data)
	genesis := DefaultGenesisBlock()

	bytes, _ := genesis.MarshalJSON()
	json := string(bytes)
	fmt.Println(json)
	block, _, _ := genesis.ToBlock()
	if block.Hash() != params.MainnetGenesisHash {
		t.Errorf("wrong mainnet genesis hash, got %v, want %v", block.Hash(), params.MainnetGenesisHash)
	}
	block, _, _ = DefaultTestnetGenesisBlock().ToBlock()
	if block.Hash() != params.TestnetGenesisHash {
		t.Errorf("wrong testnet genesis hash, got %v, want %v", block.Hash(), params.TestnetGenesisHash)
	}

	// ExtraData hex to string
	decode := hexutil.MustDecode("0x7869786978697869")
	fmt.Println(string(decode))
	// string to hex
	encodeResult := hexutil.Encode([]byte(string(decode)))
	fmt.Println(encodeResult)
}

func TestDefaultGenesisBlock2(t *testing.T) {
	genesis := DefaultGenesisBlock()
	block, _, _ := genesis.ToBlock()
	fmt.Println("main genesisBlockHash = ",block.Hash().Hex())



	genesis2 := DefaultTestnetGenesisBlock()
	block2, _, _ := genesis2.ToBlock()
	fmt.Println("test genesisBlockHash = ",block2.Hash().Hex())
}

type testGenesis struct {
	Config     *params.ChainConfig `json:"config"`
	Nonce      uint64              `json:"nonce"`
	Timestamp  uint64              `json:"timestamp"`
	ExtraData  []byte              `json:"extraData"`
	GasLimit   uint64              `json:"gasLimit"   gencodec:"required"`
	Difficulty *big.Int            `json:"difficulty" gencodec:"required"`
	Mixhash    common.Hash         `json:"mixHash"`
	Coinbase   common.Address      `json:"coinbase"`
	Alloc      GenesisAlloc        `json:"alloc"      gencodec:"required"`
	Agents     types.StoreData     `json:"agents,omitempty" gencodec:"required"`

	// These fields are used for consensus tests. Please don't use them
	// in actual genesis blocks.
	Number     uint64      `json:"number"`
	GasUsed    uint64      `json:"gasUsed"`
	ParentHash common.Hash `json:"parentHash"`
}

func TestDeveloperGenesisBlock(t *testing.T) {
	genesisTest := Genesis{Agents: decodeGenesisAgents(mainnetAgentData)}
	bytes, _ := json.Marshal(genesisTest)

	fmt.Println(string(bytes))
	var data Genesis
	json.Unmarshal(bytes, &data)
	fmt.Println(data)
}

func TestSetupGenesis(t *testing.T) {
	var (
		customghash = common.HexToHash("0x89c99d90b79719238d2645c7642f2c9295246e80775b38cfd162b696817fbd50")
		customg     = Genesis{
			Config: &params.ChainConfig{},
			Alloc: GenesisAlloc{
				{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			},
		}
		oldcustomg = customg
	)
	oldcustomg.Config = &params.ChainConfig{}
	tests := []struct {
		name       string
		fn         func(aoadb.Database) (*params.ChainConfig, common.Hash, *Genesis, error)
		wantConfig *params.ChainConfig
		wantHash   common.Hash
		wantErr    error
	}{
		{
			name: "genesis without ChainConfig",
			fn: func(db aoadb.Database) (*params.ChainConfig, common.Hash, *Genesis, error) {
				return SetupGenesisBlock(db, new(Genesis))
			},
			wantErr:    errGenesisNoConfig,
			//wantConfig: params.AllEthashProtocolChanges,
		},
		{
			name: "no block in DB, genesis == nil",
			fn: func(db aoadb.Database) (*params.ChainConfig, common.Hash, *Genesis, error) {
				return SetupGenesisBlock(db, nil)
			},
			wantHash:   params.MainnetGenesisHash,
			wantConfig: params.MainnetChainConfig,
		},
		{
			name: "mainnet block in DB, genesis == nil",
			fn: func(db aoadb.Database) (*params.ChainConfig, common.Hash, *Genesis, error) {
				DefaultGenesisBlock().MustCommit(db)
				return SetupGenesisBlock(db, nil)
			},
			wantHash:   params.MainnetGenesisHash,
			wantConfig: params.MainnetChainConfig,
		},
		{
			name: "custom block in DB, genesis == nil",
			fn: func(db aoadb.Database) (*params.ChainConfig, common.Hash, *Genesis, error) {
				customg.MustCommit(db)
				return SetupGenesisBlock(db, nil)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "custom block in DB, genesis == testnet",
			fn: func(db aoadb.Database) (*params.ChainConfig, common.Hash, *Genesis, error) {
				customg.MustCommit(db)
				return SetupGenesisBlock(db, DefaultTestnetGenesisBlock())
			},
			wantErr:    &GenesisMismatchError{Stored: customghash, New: params.TestnetGenesisHash},
			wantHash:   params.TestnetGenesisHash,
			wantConfig: params.TestnetChainConfig,
		},
		{
			name: "compatible config in DB",
			fn: func(db aoadb.Database) (*params.ChainConfig, common.Hash, *Genesis, error) {
				oldcustomg.MustCommit(db)
				return SetupGenesisBlock(db, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
	}

	for _, test := range tests {
		db, _ := aoadb.NewMemDatabase()
		config, hash, _, err := test.fn(db)
		// Check the return values.
		if !reflect.DeepEqual(err, test.wantErr) {
			spew := spew.ConfigState{DisablePointerAddresses: true, DisableCapacities: true}
			t.Errorf("%s: returned error %#v, want %#v", test.name, spew.NewFormatter(err), spew.NewFormatter(test.wantErr))
		}
		if !reflect.DeepEqual(config, test.wantConfig) {
			t.Errorf("%s:\nreturned %v\nwant     %v", test.name, config, test.wantConfig)
		}
		if hash != test.wantHash {
			t.Errorf("%s: returned hash %s, want %s", test.name, hash.Hex(), test.wantHash.Hex())
		} else if err == nil {
			// Check database content.
			stored := GetBlock(db, test.wantHash, 0)
			if stored.Hash() != test.wantHash {
				t.Errorf("%s: block in DB has hash %s, want %s", test.name, stored.Hash(), test.wantHash)
			}
		}
	}
}

func TestDefaultTestnetGenesisBlock(t *testing.T) {
	testnetGenesisBlock := DefaultTestnetGenesisBlock()
	block, _, _ := testnetGenesisBlock.ToBlock()
	fmt.Println(block.Hash().Hex())
	//decode := hexutil.MustDecode("0x11bbe8db4e347b4e8c937c1c8370e4b5ed33adb3db69cbdb7a38e1e50b1b82fa")
	//fmt.Println(string(bitutil.CompressBytes(decode)))
}

func TestDefaultGenesisBlock3(t *testing.T) {
	defaultGenesisBlock := DefaultGenesisBlock()
	block, _, _ := defaultGenesisBlock.ToBlock()
	fmt.Println(block.Hash().Hex())
}

func TestDefaultRinkebyGenesisBlock(t *testing.T) {
	aa, _ := big.NewInt(0).SetString("3200000000000000000000000000", 10)
	fmt.Println(hexutil.EncodeBig(aa))
	bb, _ := big.NewInt(0).SetString("2000000000000000000000000", 10)
	fmt.Println(hexutil.EncodeBig(bb))
	cc, err := big.NewInt(0).SetString("0x18ee90ff6c05e6318e200f34e", 0)
	fmt.Println(cc, err)
}

func TestGenesisAgents(t *testing.T) {
	var list GenesisAgents
	candidateList := []types.Candidate{
		{"0x71af77518da8ee1e152068ea4727d1041d71b813", uint64(1), "node1-1", 1492009146}, // 172.16.134.100
		{"0xa51bac4fe71640157f29317c2fe233c26b71c6c8", uint64(1), "node1-2", 1492009146}, // 172.16.134.100
		{"0xb0b81949b3b6d6ff926336d6227cec04ceca88b2", uint64(1), "node1-3", 1492009146}, // 172.16.134.100
		{"0x4d8bfcdbc0192e3a2e189ed133ee4e98e4e381f8", uint64(1), "node2-1", 1492009146}, // 172.16.134.101
		{"0xe92c157278abafa68e3547d4d5bd3ed4a5afccb3", uint64(1), "node2-2", 1492009146}, // 172.16.134.101
		{"0x5ac2ff101f11ae3c2b7093e25f5300018252c2a3", uint64(1), "node2-3", 1492009146}, // 172.16.134.101
	}
	list = append(list,candidateList...)

	data, err := rlp.EncodeToBytes(list)
	if err != nil {
		t.Fatal(err)
	}
	result := strconv.QuoteToASCII(string(data))


	list2 := decodeGenesisAgents(result)
	fmt.Println("result = ", result)
	fmt.Println("\n", list2)

}
