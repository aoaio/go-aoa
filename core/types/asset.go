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
	"bytes"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/rlp"
	"io"
	"math/big"
	"sort"
)

//Asset is the struct to record an account's asset balance.
type Asset struct {
	ID      common.Address
	Balance *big.Int
}

/////////////////Assets

type Assets struct {
	assetList []Asset //maintain it as ascending alphabetical order
}

func NewAssets() *Assets {
	as := new(Assets)
	as.assetList = make([]Asset, 0)
	return as
}

// EncodeRLP implements rlp.Encoder.
func (as *Assets) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, as.assetList)
}

// DecodeRLP implements rlp.Decoder, and loads the consensus fields of a receipt
// from an RLP stream.
func (as *Assets) DecodeRLP(s *rlp.Stream) error {
	var al []Asset
	if err := s.Decode(&al); err != nil {
		return err
	}
	as.assetList = al
	return nil
}

//AddAsset在列表中增加资产，若相应的资产已存在，则直接增加资产余额。
func (as *Assets) AddAsset(a Asset) {
	i, isContain := as.indexOf(a.ID)
	if isContain {
		as.assetList[i].Balance = new(big.Int).Add(as.assetList[i].Balance, a.Balance)
	} else {
		temp := append([]Asset{}, as.assetList[i:]...)
		as.assetList = append(as.assetList[:i], a)
		as.assetList = append(as.assetList, temp...)
	}
}

//SubAsset根据指定的Asset对象，扣减想对应Asset的值。返回值指示是否成功（失败时对应资产余额不变）
func (as *Assets) SubAsset(a Asset) bool {
	i, isContain := as.indexOf(a.ID)
	if isContain {
		dest := &as.assetList[i]
		if dest.Balance.Cmp(a.Balance) >= 0 {
			dest.Balance = dest.Balance.Sub(dest.Balance, a.Balance)
			if dest.Balance.Cmp(big.NewInt(0)) == 0 {
				//if balance is 0, then remove it
				if i < len(as.assetList)-1 {
					as.assetList = append(as.assetList[:i], as.assetList[i+1:]...)
				} else {
					as.assetList = as.assetList[:i]
				}
			}
			return true
		}
	}
	return false
}

//indexOf returns the index where the asset should be, and also a bool value showing that is the asset already in this Assets.
//It will use binary search.
func (as *Assets) indexOf(id common.Address) (int, bool) {
	n := len(as.assetList)
	if n == 0 {
		return 0, false
	}
	i := sort.Search(n, func(i int) bool { return bytes.Compare(as.assetList[i].ID.Bytes(), id.Bytes()) >= 0 })
	var b bool
	if i < n {
		b = bytes.Compare(as.assetList[i].ID.Bytes(), id.Bytes()) == 0
	} else {
		b = false
	}
	return i, b
}

func (as *Assets) BalanceOf(id common.Address) *big.Int {
	i, isContain := as.indexOf(id)
	if isContain {
		return new(big.Int).Set(as.assetList[i].Balance)
	}
	return common.Big0
}

func (as *Assets) GetAssets() []Asset {
	if len(as.assetList) > 0 {
		list := make([]Asset, 0, len(as.assetList))
		for _, a := range as.assetList {
			na := Asset{
				ID:      a.ID,
				Balance: new(big.Int).Set(a.Balance),
			}
			list = append(list, na)
		}
		return list
	}
	return nil
}

func (as *Assets) IsEmpty() bool {
	return len(as.assetList) == 0
}
