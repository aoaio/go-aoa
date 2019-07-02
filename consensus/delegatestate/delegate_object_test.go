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
	"fmt"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"math/big"
	"testing"
	"time"
)

type DelegateSuite struct {
	db    *aoadb.MemDatabase
	state *DelegateDB
}

var toAddr = common.BytesToAddress

func TestDelegateDB_Dump(t *testing.T) {
	d, err := getDelegateSuite()
	if err != nil {
		fmt.Errorf("create db err:%v\n", err)
		return
	}
	// generate a few entries
	obj1 := d.state.GetOrNewStateObject(toAddr([]byte{0x01}), "node1", 1524187807)
	obj1.AddVote(big.NewInt(2))
	obj2 := d.state.GetOrNewStateObject(toAddr([]byte{0x01, 0x02}), "node2", 1524187807)
	obj2.AddVote(big.NewInt(3))
	obj3 := d.state.GetOrNewStateObject(toAddr([]byte{0x02}), "node3", 1524187807)
	obj3.AddVote(big.NewInt(4))

	// write some of them to the trie
	d.state.updateStateObject(obj1)
	d.state.updateStateObject(obj2)
	d.state.updateStateObject(obj3)
	d.state.CommitTo(d.db, false)

	// check that dump contains the state objects that are in trie
	got := string(d.state.Dump())

	want := `{
	"root": "979a8dc033f7f9efecc2fe36b7c555eaa5b2e7d5ad7d52eed04e04456135203f",
		"delegates": {
	       "0000000000000000000000000000000000000001": {
	           "vote": 2,
	           "root": "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
               "nickname": "node1",
               "registerTime": 1524187807,
	           "storage": {}
	       },
	       "0000000000000000000000000000000000000002": {
               "vote": 4,
	           "root": "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
               "nickname": "node3",
               "registerTime": 1524187807,
	           "storage": {}
	       },
	       "0000000000000000000000000000000000000102": {
	           "vote": 3,
	           "root": "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
               "nickname": "node2",
               "registerTime": 1524187807,
	           "storage": {}
	       }
	    }
    }`
	fmt.Println(got)
	fmt.Println(want)

}

func TestDelegateDB_Snapshot(t *testing.T) {
	d, err := getDelegateSuite()
	if err != nil {
		t.Fatalf("create db err:%v\n", err)
	}
	delegateobjaddr := toAddr([]byte("aa"))
	var storageaddr common.Hash
	data1 := common.BytesToHash([]byte{42})
	data2 := common.BytesToHash([]byte{43})

	// set initial delegate object value
	d.state.createObject(delegateobjaddr, "node1", 1524187807)
	// set must ensure already create,otherwise it cann't effect
	d.state.SetState(delegateobjaddr, storageaddr, data1)

	res1 := d.state.GetState(delegateobjaddr, storageaddr)
	fmt.Printf("res1:%s\n", res1.Hex())
	// get snapshot of current state
	snapshot := d.state.Snapshot()

	d.state.createObject(delegateobjaddr, "node2", 1524187807)
	// set new delegate object value
	d.state.SetState(delegateobjaddr, storageaddr, data2)

	res2 := d.state.GetState(delegateobjaddr, storageaddr)
	fmt.Printf("res2:%s\n", res2.Hex())
	// restore snapshot
	d.state.RevertToSnapshot(snapshot)

	// get state storage value
	res3 := d.state.GetState(delegateobjaddr, storageaddr)

	fmt.Printf("res3:%s\n", res3.Hex())

	if res1 == res3 {
		fmt.Println("revert success")
	} else {
		fmt.Println("revert fail")
	}
}

func TestSnapshotEmpty(t *testing.T) {
	d, err := getDelegateSuite()
	if err != nil {
		fmt.Errorf("create db err:%v\n", err)
		return
	}
	d.state.RevertToSnapshot(d.state.Snapshot())
}

func TestSnapshot2(t *testing.T) {
	d, err := getDelegateSuite()
	if err != nil {
		t.Fatalf("create db err:%v\n", err)
	}

	delegateobjaddr0 := toAddr([]byte("so0"))
	delegateobjaddr1 := toAddr([]byte("so1"))
	var storageaddr common.Hash

	data0 := common.BytesToHash([]byte{17})
	data1 := common.BytesToHash([]byte{18})

	d.state.createObject(delegateobjaddr0, "node1", 1524187807)
	d.state.createObject(delegateobjaddr1, "node2", 1524187807)

	d.state.SetState(delegateobjaddr0, storageaddr, data0)
	d.state.SetState(delegateobjaddr1, storageaddr, data1)

	// db,trie are already non-empty values
	d0 := d.state.GetStateObject(delegateobjaddr0)
	d0.setVote(big.NewInt(1))
	d0.deleted = false
	d.state.setStateObject(d0)

	root, err := d.state.CommitTo(d.db, false)
	if err != nil {
		t.Fatalf("state commit fail:%v\n", err)
	}
	d.state.Reset(root)

	// and one with deleted == true
	d1 := d.state.GetStateObject(delegateobjaddr1)
	d1.setVote(big.NewInt(2))
	d1.deleted = true
	d.state.setStateObject(d1)

	d1 = d.state.GetStateObject(delegateobjaddr1)
	if d1 != nil {
		t.Fatalf("deleted object not nil when getting \n")
	}
	snapshot := d.state.Snapshot()
	d.state.RevertToSnapshot(snapshot)

	d0Revert := d.state.GetStateObject(delegateobjaddr0)
	// Update values before comparing
	d0Revert.GetState(d.state.db, storageaddr)

	// non-deleted is equal (restored)
	compareDelegateObjects(d0Revert, d0, t)

	do1Restored := d.state.GetStateObject(delegateobjaddr1)
	if do1Restored != nil {
		t.Fatalf("deleted object not nil after restoring snapshot:%+v \n", do1Restored)
	}

}

func getDelegateSuite() (*DelegateSuite, error) {
	db, err := aoadb.NewMemDatabase()
	if err != nil {
		fmt.Errorf("create db err:%v\n", err)
		return nil, err
	}
	delegateDB, err := New(common.Hash{}, NewDatabase(db))
	if err != nil {
		fmt.Errorf("create delegateDB err:%v\n", err)
		return nil, err
	}
	d := &DelegateSuite{db, delegateDB}
	return d, nil
}

func compareDelegateObjects(do0, do1 *delegateObject, t *testing.T) {
	if do0.Address() != do1.Address() {
		t.Fatalf("Address mismatch: have %v, want %v", do0.address, do1.address)
	}
	if do0.Vote().Cmp(do1.Vote()) != 0 {
		t.Fatalf("Vote mismatch: have %v, want %v", do0.Vote(), do0.Vote())
	}
	if do0.data.Root != do1.data.Root {
		t.Errorf("Root mismatch: have %x, want %x", do0.data.Root[:], do1.data.Root[:])
	}
	if len(do1.cachedStorage) != len(do0.cachedStorage) {
		t.Errorf("Storage size mismatch: have %d, want %d", len(do1.cachedStorage), len(do0.cachedStorage))
	}
	for k, v := range do1.cachedStorage {
		if do0.cachedStorage[k] != v {
			t.Errorf("Storage key %x mismatch: have %v, want %v", k, do0.cachedStorage[k], v)
		}
	}
	for k, v := range do0.cachedStorage {
		if do1.cachedStorage[k] != v {
			t.Errorf("Storage key %x mismatch: have %v, want none.", k, v)
		}
	}
	if do0.deleted != do1.deleted {
		t.Fatalf("Deleted mismatch: have %v, want %v", do0.deleted, do1.deleted)
	}
}

func TestTouchDelete(t *testing.T) {
	d, err := getDelegateSuite()
	if err != nil {
		fmt.Errorf("create db err:%v\n", err)
		return
	}
	d.state.createObject(common.Address{}, "node1", uint64(time.Now().Unix()))
	root, _ := d.state.CommitTo(d.db, false)
	d.state.Reset(root)

	snapshot := d.state.Snapshot()
	d.state.createObject(common.Address{}, "node1", uint64(time.Now().Unix()))
	d.state.AddVote(common.Address{}, new(big.Int))

	if len(d.state.delegateObjectsDirty) != 1 {
		t.Fatal("expected one dirty state object")
	}

	d.state.RevertToSnapshot(snapshot)
	if len(d.state.delegateObjectsDirty) != 0 {
		t.Fatal("expected no dirty state object")
	}
}
