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

package params

import (
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"math/big"
)

var (
	MainNetGenesisHash = common.HexToHash("0x231c31e2b9303ec8cbd6d89af3aff01866b6a02925da401cd2429aefc76c829f")
	TestNetGenesisHash = common.HexToHash("0x52d892862b3af7bed302a73a56d8a8aa07696f25f9349375b61d2f6ad936cc0a")
)

var (
	// MainNetChainConfig is the chain parameters to run a node on the main network.
	MainNetChainConfig = &ChainConfig{
		ChainId:          big.NewInt(1),
		Reward:           big.NewInt(1e+18),
		MaxElectDelegate: big.NewInt(101),
		BlockInterval:    big.NewInt(3),
	}

	// TestNetChainConfig contains the chain parameters to run a node on the Ropsten test network.
	TestNetChainConfig = &ChainConfig{
		ChainId:          big.NewInt(11),
		Reward:           big.NewInt(2e+18),
		MaxElectDelegate: big.NewInt(101),
		BlockInterval:    big.NewInt(3),
	}

	// chainId must between 1 ~ 255
	AllAuroraProtocolChanges = &ChainConfig{
		ChainId:          big.NewInt(60),
		Reward:           big.NewInt(1e+18),
		MaxElectDelegate: big.NewInt(3),
		BlockInterval:    big.NewInt(3),
	}

	TestChainConfig = &ChainConfig{
		ChainId: big.NewInt(1),
	}
)

// ChainConfig is the core config which determines the blockchain settings.
// ChainConfig is stored in the database on a per block basis. This means
// that any network, identified by its genesis block, can have its own
// set of configuration options.
type ChainConfig struct {
	ChainId          *big.Int `json:"chainId"` // Chain id identifies the current chain and is used for replay protection
	Reward           *big.Int // Block reward in wei for successfully produce a block upward from Byzantium
	MaxElectDelegate *big.Int // dpos max elect delegate number
	BlockInterval    *big.Int
}

// String implements the fmt.Stringer interface.
func (c *ChainConfig) String() string {
	return fmt.Sprintf("{ChainID: %v Engine: %v}", c.ChainId, "DPOS-BFT")
}

// ConfigCompatError is raised if the locally-stored blockchain is initialised with a
// ChainConfig that would alter the past.
type ConfigCompatError struct {
	What string
	// block numbers of the stored and new configurations
	StoredConfig, NewConfig *big.Int
	// the block number to which the local chain must be rewound to correct the error
	RewindTo uint64
}

func (err *ConfigCompatError) Error() string {
	return fmt.Sprintf("mismatching %s in database (have %d, want %d, rewindto %d)", err.What, err.StoredConfig, err.NewConfig, err.RewindTo)
}

// GasTable returns the gas table corresponding to the current phase (first phase or epiphron reprice).
//
// The returned GasTable's fields shouldn't, under any circumstances, be changed.
func (c *ChainConfig) GasTable(num *big.Int) GasTable {
	return GasTable{}
}

type Rules struct {
	ChainId *big.Int
}

func (c *ChainConfig) Rules(num *big.Int) Rules {
	chainId := c.ChainId
	if chainId == nil {
		chainId = new(big.Int)
	}
	return Rules{ChainId: new(big.Int).Set(chainId)}
}
