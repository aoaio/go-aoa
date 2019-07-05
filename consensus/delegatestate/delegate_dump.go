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

package delegatestate

import (
	"encoding/json"
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/rlp"
	"github.com/Aurorachain/go-aoa/trie"
)

type DumpDelegate struct {
	Vote         uint64            `json:"vote"`
	Root         string            `json:"root"`
	Nickname     string            `json:"nickname"`
	RegisterTime uint64            `json:"registerTime"`
	Storage      map[string]string `json:"storage"`
}

type Dump struct {
	Root      string                  `json:"root"`
	Delegates map[string]DumpDelegate `json:"delegates"`
}

func (d *DelegateDB) RawDump() Dump {
	dump := Dump{
		Root:      fmt.Sprintf("%x", d.trie.Hash()),
		Delegates: make(map[string]DumpDelegate),
	}

	it := trie.NewIterator(d.trie.NodeIterator(nil))
	for it.Next() {
		addr := d.trie.GetKey(it.Key)
		var data Delegate
		if err := rlp.DecodeBytes(it.Value, &data); err != nil {
			panic(err)
		}

		obj := newObject(nil, common.BytesToAddress(addr), data, nil)
		delegate := DumpDelegate{
			Vote:         data.Vote.Uint64(),
			Root:         common.Bytes2Hex(data.Root[:]),
			Storage:      make(map[string]string),
			Nickname:     data.Nickname,
			RegisterTime: data.RegisterTime,
		}
		storageIt := trie.NewIterator(obj.getTrie(d.db).NodeIterator(nil))
		for storageIt.Next() {
			delegate.Storage[common.Bytes2Hex(d.trie.GetKey(storageIt.Key))] = common.Bytes2Hex(storageIt.Value)
		}
		dump.Delegates[common.Bytes2Hex(addr)] = delegate
	}
	return dump
}

func (d *DelegateDB) Dump() []byte {
	jsonBytes, err := json.MarshalIndent(d.RawDump(), "", "    ")
	if err != nil {
		fmt.Println("dump err", err)
	}
	return jsonBytes
}
