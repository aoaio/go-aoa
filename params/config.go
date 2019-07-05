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
	MainnetGenesisHash = common.HexToHash("0x231c31e2b9303ec8cbd6d89af3aff01866b6a02925da401cd2429aefc76c829f") // Mainnet genesis hash to enforce below configs on
	TestnetGenesisHash = common.HexToHash("0x52d892862b3af7bed302a73a56d8a8aa07696f25f9349375b61d2f6ad936cc0a") // Testnet genesis hash to enforce below configs on
)

var (
	// MainnetChainConfig is the chain parameters to run a node on the main network.
	MainnetChainConfig = &ChainConfig{
		ChainId:              big.NewInt(1),
		ByzantiumBlock:       big.NewInt(4370000),
		FrontierBlockReward:  big.NewInt(5e+18),
		ByzantiumBlockReward: big.NewInt(1e+18),
		MaxElectDelegate:     big.NewInt(101),
		BlockInterval:        big.NewInt(10),

		AresBlock:     big.NewInt(373300),
		EpiphronBlock: big.NewInt(485000),
	}

	// TestnetChainConfig contains the chain parameters to run a node on the Ropsten test network.
	TestnetChainConfig = &ChainConfig{
		ChainId:              big.NewInt(11),
		ByzantiumBlock:       big.NewInt(1700000),
		FrontierBlockReward:  big.NewInt(5e+18),
		ByzantiumBlockReward: big.NewInt(2e+18),
		MaxElectDelegate:     big.NewInt(101),
		BlockInterval:        big.NewInt(10),
		AresBlock:            big.NewInt(0),
		EpiphronBlock:        big.NewInt(20000),
	}

	// chainId must between 1 ~ 255
	AllAuroraProtocolChanges = &ChainConfig{
		ChainId:              big.NewInt(60),
		ByzantiumBlock:       big.NewInt(10000),
		FrontierBlockReward:  big.NewInt(5e+18),
		ByzantiumBlockReward: big.NewInt(1e+18),
		MaxElectDelegate:     big.NewInt(3),
		BlockInterval:        big.NewInt(10),
		AresBlock:            big.NewInt(0),
		EpiphronBlock:        big.NewInt(3750),
	}

	TestChainConfig = &ChainConfig{
		ChainId:        big.NewInt(1),
		ByzantiumBlock: big.NewInt(0),
		AresBlock:      big.NewInt(90),
	}
	TestRules = TestChainConfig.Rules(new(big.Int))
)

// ChainConfig is the core config which determines the blockchain settings.
//
// ChainConfig is stored in the database on a per block basis. This means
// that any network, identified by its genesis block, can have its own
// set of configuration options.
type ChainConfig struct {
	ChainId        *big.Int `json:"chainId"`                  // Chain id identifies the current chain and is used for replay protection
	ByzantiumBlock *big.Int `json:"byzantiumBlock,omitempty"` // Byzantium switch block (nil = no fork, 0 = already on byzantium)

	FrontierBlockReward  *big.Int // Block reward in wei for successfully produce a block
	ByzantiumBlockReward *big.Int // Block reward in wei for successfully produce a block upward from Byzantium
	MaxElectDelegate     *big.Int // dpos max elect delegate number
	BlockInterval        *big.Int

	AresBlock     *big.Int `json:"aresBlock,omitempty"`     // Ares switch block (nil = no fork, 0 = already on ares stage)
	EpiphronBlock *big.Int `json:"epiphronBlock,omitempty"` // Before Epiphron block, transfer asset value to a contract will always success
}

// String implements the fmt.Stringer interface.
func (c *ChainConfig) String() string {
	return fmt.Sprintf("{ChainID: %v Byzantium: %v AresBlock: %v EpiphronBlock: %v Engine: %v}",
		c.ChainId,
		c.ByzantiumBlock,
		c.AresBlock,
		c.EpiphronBlock,
		"DPOS-BFT",
	)
}

// CheckCompatible checks whether scheduled fork transitions have been imported
// with a mismatching chain configuration.
func (c *ChainConfig) CheckCompatible(newcfg *ChainConfig, height uint64) *ConfigCompatError {
	bhead := new(big.Int).SetUint64(height)

	// Iterate checkCompatible to find the lowest conflict.
	var lasterr *ConfigCompatError
	for {
		err := c.checkCompatible(newcfg, bhead)
		if err == nil || (lasterr != nil && err.RewindTo == lasterr.RewindTo) {
			break
		}
		lasterr = err
		bhead.SetUint64(err.RewindTo)
	}
	return lasterr
}

//因主链历史原因，不做AresBlock的兼容性检查
func (c *ChainConfig) checkCompatible(newcfg *ChainConfig, head *big.Int) *ConfigCompatError {

	if isForkIncompatible(c.ByzantiumBlock, newcfg.ByzantiumBlock, head) {
		return newCompatError("Byzantium fork block", c.ByzantiumBlock, newcfg.ByzantiumBlock)
	}
	if isForkIncompatible(c.EpiphronBlock, newcfg.EpiphronBlock, head) {
		return newCompatError("Epiphron fork block", c.EpiphronBlock, newcfg.EpiphronBlock)
	}

	return nil
}

// isForkIncompatible returns true if a fork scheduled at s1 cannot be rescheduled to
// block s2 because head is already past the fork.
func isForkIncompatible(s1, s2, head *big.Int) bool {
	return (isForked(s1, head) || isForked(s2, head)) && !configNumEqual(s1, s2)
}

// isForked returns whether a fork scheduled at block s is active at the given head block.
func isForked(s, head *big.Int) bool {
	if s == nil || head == nil {
		return false
	}
	return s.Cmp(head) <= 0
}

func configNumEqual(x, y *big.Int) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return x.Cmp(y) == 0
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

func newCompatError(what string, storedblock, newblock *big.Int) *ConfigCompatError {
	var rew *big.Int
	switch {
	case storedblock == nil:
		rew = newblock
	case newblock == nil || storedblock.Cmp(newblock) < 0:
		rew = storedblock
	default:
		rew = newblock
	}
	err := &ConfigCompatError{what, storedblock, newblock, 0}
	if rew != nil && rew.Sign() > 0 {
		err.RewindTo = rew.Uint64() - 1
	}
	return err
}

func (err *ConfigCompatError) Error() string {
	return fmt.Sprintf("mismatching %s in database (have %d, want %d, rewindto %d)", err.What, err.StoredConfig, err.NewConfig, err.RewindTo)
}

func (c *ChainConfig) IsByzantium(num *big.Int) bool {
	return isForked(c.ByzantiumBlock, num)
}

func (c *ChainConfig) IsAres(num *big.Int) bool {
	return isForked(c.AresBlock, num)
}

func (c *ChainConfig) IsEpiphron(num *big.Int) bool {
	return isForked(c.EpiphronBlock, num)
}

// GasTable returns the gas table corresponding to the current phase (first phase or epiphron reprice).
//
// The returned GasTable's fields shouldn't, under any circumstances, be changed.
func (c *ChainConfig) GasTable(num *big.Int) GasTable {
	if num == nil {
		return GasTable{}
	}
	switch {
	case c.IsEpiphron(num):
		return GasTableEpiphron
	default:
		return GasTable{}
	}
}

// Rules wraps ChainConfig and is merely syntatic sugar or can be used for functions
// that do not have or require information about the block.
//
// Rules is a one time interface meaning that it shouldn't be used in between transition
// phases.
type Rules struct {
	ChainId     *big.Int
	IsByzantium bool
}

func (c *ChainConfig) Rules(num *big.Int) Rules {
	chainId := c.ChainId
	if chainId == nil {
		chainId = new(big.Int)
	}
	return Rules{ChainId: new(big.Int).Set(chainId), IsByzantium: c.IsByzantium(num)}
}
