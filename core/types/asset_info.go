package types

import (
	"github.com/Aurorachain/go-Aurora/common"
	"math/big"
	"fmt"
	"strings"
	"regexp"
)

const (
	AssetNameMaxLen   = 50
	AssetSymbolMaxLen = 20
	AssetDescMaxLen   = 1024
)
var validSymbol  = regexp.MustCompile(`^[a-zA-Z]{1}[a-zA-Z0-9]+$`)
var illegalSymbol  = regexp.MustCompile("AOA|AURORA")

type AssetInfo struct {
	Issuer *common.Address `json:"issuer,omitempty" rlp:"nil"`
	Name   string          `json:"name"`
	Symbol string          `json:"symbol"`
	Supply *big.Int        `json:"supply"`
	Desc   string          `json:"desc"`
}

func IsAssetInfoValid(ai *AssetInfo) (error) {
	if nil != ai {
		if len(strings.TrimSpace(ai.Symbol)) == 0 || len(strings.TrimSpace(ai.Name))==0 {
			return fmt.Errorf("name or symbol can not be empty")
		}
		if len(ai.Symbol) > AssetSymbolMaxLen || len(ai.Name) > AssetNameMaxLen || len(ai.Desc) > AssetDescMaxLen {
			return fmt.Errorf("Asset name or symbol or desc is too long. maxNameLen:%d; maxSymbolLen:%d; maxDescLen: %d", AssetNameMaxLen, AssetSymbolMaxLen, AssetDescMaxLen)
		}
		if illegal := illegalSymbol.MatchString(strings.ToUpper(ai.Name)); illegal{
			return fmt.Errorf("name %s illegal",ai.Name)
		}
		if illegal := illegalSymbol.MatchString(strings.ToUpper(ai.Symbol)); illegal{
			return fmt.Errorf("symbol %s illegal",ai.Symbol)
		}
		if valid := validSymbol.MatchString(ai.Symbol); !valid {
			return fmt.Errorf("symbol %s invalid",ai.Symbol)
		}
		if nil == ai.Supply || ai.Supply.Cmp(common.Big0) == 0 {
			return fmt.Errorf("Supply must be greater then 0 ")
		}
		return nil
	}else {
		return fmt.Errorf("assetInfo is Nil")
	}
}
