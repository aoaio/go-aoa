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

package watch

import (
	"errors"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/rlp"
)

// InnerTxDb wraps access to internal transactions data.
type InnerTxDb interface {
	Set(txhash common.Hash, itxs []*types.InnerTx) error
	Has(txhash common.Hash) (bool, error)
	Get(txhash common.Hash) ([]*types.InnerTx, error)
}

type itxdb struct {
	db aoadb.Database
}

func NewInnerTxDb(db aoadb.Database) InnerTxDb {
	return &itxdb{db: db}
}

func (db *itxdb) Set(txhash common.Hash, itxs []*types.InnerTx) error {
	if len(itxs) > 0 {
		v, err := rlp.EncodeToBytes(itxs)
		if nil != err {
			return err
		}
		err = db.db.Put(txhash.Bytes(), v)
		return err
	} else {
		return errors.New("no value to save")
	}
}

func (db *itxdb) Has(txhash common.Hash) (bool, error) {
	k := txhash.Bytes()
	return db.db.Has(k)
}

func (db *itxdb) Get(txhash common.Hash) ([]*types.InnerTx, error) {
	k := txhash.Bytes()
	v, err := db.db.Get(k)
	if nil != err {
		return nil, err
	}
	var itxs []*types.InnerTx
	err = rlp.DecodeBytes(v, &itxs)
	return itxs, err
}
