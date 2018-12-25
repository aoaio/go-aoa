package params

import (
	"fmt"
	"github.com/Aurorachain/go-Aurora/common"
	"math/big"
)

var (
	MainnetGenesisHash = common.HexToHash("")
	TestnetGenesisHash = common.HexToHash("")
)

var (

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

	TestnetChainConfig = &ChainConfig{
		ChainId:              big.NewInt(11),
		ByzantiumBlock:       big.NewInt(1700000),
		FrontierBlockReward:  big.NewInt(5e+18),
		ByzantiumBlockReward: big.NewInt(2e+18),
		MaxElectDelegate:     big.NewInt(101),
		BlockInterval:        big.NewInt(10),
		AresBlock:            big.NewInt(4000),
		EpiphronBlock:        big.NewInt(20000),
	}

	AllAuroraProtocolChanges = &ChainConfig{
		ChainId:              big.NewInt(60),
		ByzantiumBlock:       big.NewInt(10000),
		FrontierBlockReward:  big.NewInt(5e+18),
		ByzantiumBlockReward: big.NewInt(1e+18),
		MaxElectDelegate:     big.NewInt(1),
		BlockInterval:        big.NewInt(10),
		AresBlock:            big.NewInt(90),
		EpiphronBlock:        big.NewInt(3750),
	}

	TestChainConfig = &ChainConfig{
		ChainId:        big.NewInt(1),
		ByzantiumBlock: big.NewInt(0),
		AresBlock:      big.NewInt(90),
	}
	TestRules = TestChainConfig.Rules(new(big.Int))
)

type ChainConfig struct {
	ChainId        *big.Int `json:"chainId"`                  
	ByzantiumBlock *big.Int `json:"byzantiumBlock,omitempty"` 

	FrontierBlockReward  *big.Int 
	ByzantiumBlockReward *big.Int 
	MaxElectDelegate     *big.Int 
	BlockInterval        *big.Int

	AresBlock     *big.Int `json:"aresBlock,omitempty"`     
	EpiphronBlock *big.Int `json:"epiphronBlock,omitempty"` 
}

func (c *ChainConfig) String() string {
	return fmt.Sprintf("{ChainID: %v Byzantium: %v AresBlock: %v EpiphronBlock: %v Engine: %v}",
		c.ChainId,
		c.ByzantiumBlock,
		c.AresBlock,
		c.EpiphronBlock,
		"DPOS-BFT",
	)
}

func (c *ChainConfig) CheckCompatible(newcfg *ChainConfig, height uint64) *ConfigCompatError {
	bhead := new(big.Int).SetUint64(height)

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

func (c *ChainConfig) checkCompatible(newcfg *ChainConfig, head *big.Int) *ConfigCompatError {

	if isForkIncompatible(c.ByzantiumBlock, newcfg.ByzantiumBlock, head) {
		return newCompatError("Byzantium fork block", c.ByzantiumBlock, newcfg.ByzantiumBlock)
	}
	if isForkIncompatible(c.EpiphronBlock, newcfg.EpiphronBlock, head) {
		return newCompatError("Epiphron fork block", c.EpiphronBlock, newcfg.EpiphronBlock)
	}

	return nil
}

func isForkIncompatible(s1, s2, head *big.Int) bool {
	return (isForked(s1, head) || isForked(s2, head)) && !configNumEqual(s1, s2)
}

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

type ConfigCompatError struct {
	What string

	StoredConfig, NewConfig *big.Int

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
