package types

import (
	"bytes"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/rlp"
	"io"
	"math/big"
	"sort"
)

type Asset struct {
	ID      common.Address
	Balance *big.Int
}

type Assets struct {
	assetList []Asset
}

func NewAssets() *Assets {
	as := new(Assets)
	as.assetList = make([]Asset, 0)
	return as
}

func (as *Assets) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, as.assetList)
}

func (as *Assets) DecodeRLP(s *rlp.Stream) error {
	var al []Asset
	if err := s.Decode(&al); err != nil {
		return err
	}
	as.assetList = al
	return nil
}

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

func (as *Assets) SubAsset(a Asset) bool {
	i, isContain := as.indexOf(a.ID)
	if isContain {
		dest := &as.assetList[i]
		if dest.Balance.Cmp(a.Balance) >= 0 {
			dest.Balance = dest.Balance.Sub(dest.Balance, a.Balance)
			if dest.Balance.Cmp(big.NewInt(0)) == 0 {

				if i < len(as.assetList) - 1 {
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
