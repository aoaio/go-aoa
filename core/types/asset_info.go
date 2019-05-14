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

package types

import (
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"math/big"
	"regexp"
	"strings"
)

const (
	AssetNameMaxLen   = 50
	AssetSymbolMaxLen = 20
	AssetDescMaxLen   = 1024
)

var validSymbol = regexp.MustCompile(`^[a-zA-Z]{1}[a-zA-Z0-9]+$`)
var illegalSymbol = regexp.MustCompile("AOA|AURORA|极光链")

type AssetInfo struct {
	Issuer *common.Address `json:"issuer,omitempty" rlp:"nil"`
	Name   string          `json:"name"`
	Symbol string          `json:"symbol"`
	Supply *big.Int        `json:"supply"`
	Desc   string          `json:"desc"`
}

func IsAssetInfoValid(ai *AssetInfo) error {
	if nil != ai {
		if len(strings.TrimSpace(ai.Symbol)) == 0 || len(strings.TrimSpace(ai.Name)) == 0 {
			return fmt.Errorf("name or symbol can not be empty")
		}
		if len(ai.Symbol) > AssetSymbolMaxLen || len(ai.Name) > AssetNameMaxLen || len(ai.Desc) > AssetDescMaxLen {
			return fmt.Errorf("Asset name or symbol or desc is too long. maxNameLen:%d; maxSymbolLen:%d; maxDescLen: %d", AssetNameMaxLen, AssetSymbolMaxLen, AssetDescMaxLen)
		}
		if illegal := illegalSymbol.MatchString(strings.ToUpper(ai.Name)); illegal {
			return fmt.Errorf("name %s illegal", ai.Name)
		}
		if illegal := illegalSymbol.MatchString(strings.ToUpper(ai.Symbol)); illegal {
			return fmt.Errorf("symbol %s illegal", ai.Symbol)
		}
		if valid := validSymbol.MatchString(ai.Symbol); !valid {
			return fmt.Errorf("symbol %s invalid", ai.Symbol)
		}
		if nil == ai.Supply || ai.Supply.Cmp(common.Big0) == 0 {
			return fmt.Errorf("Supply must be greater then 0 ")
		}
		return nil
	} else {
		return fmt.Errorf("assetInfo is Nil")
	}
}
