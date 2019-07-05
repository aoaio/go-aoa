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
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

func TestUpdateLeaks(t *testing.T) {
	db, err := aoadb.NewMemDatabase()
	if err != nil {
		t.Fatalf("create db err:%v\n", err)
	}
	delegate, err := New(common.Hash{}, NewDatabase(db))
	if err != nil {
		t.Fatalf("create delegateDB err:%v\n", err)
	}

	// Update it with some accounts
	for i := byte(0); i < 255; i++ {
		addr := common.BytesToAddress([]byte{i})
		delegate.createObject(addr, strings.Join([]string{"node", string(i)}, ":"), uint64(time.Now().Unix()))
		delegate.AddVote(addr, big.NewInt(int64(12*i)))
		if i%2 == 0 {
			delegate.SetState(addr, common.BytesToHash([]byte{i, i, i}), common.BytesToHash([]byte{i, i, i, i}))
		}
		delegate.IntermediateRoot(false)
	}
	for _, key := range db.Keys() {
		value, _ := db.Get(key)
		t.Errorf("delegateState leaked into database: %x -> %x \n", key, value)
	}
}

func TestDelegateDB_CommitTo(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()
	delegateDb, _ := New(common.Hash{}, NewDatabase(db))
	address1 := common.Address{1}
	root1 := delegateDb.IntermediateRoot(false)
	fmt.Printf("empty delegateRoot:%s\n", root1.Hex())
	if !delegateDb.Exist(address1) {
		delegateDb.GetOrNewStateObject(address1, "address1", uint64(time.Now().Unix()))
		// delegateDb.SetVote(address1,big.NewInt(0))
	}
	delegateDb.Finalise(false)
	root2 := delegateDb.IntermediateRoot(false)
	delegateDb.CommitTo(db, false)
	fmt.Printf("create a 0 vote delegateRoot:%s\n", root2.Hex())
	delegateDb.AddVote(address1, big.NewInt(1))
	delegateDb.Finalise(false)
	root3 := delegateDb.IntermediateRoot(false)
	delegateDb.CommitTo(db, false)
	fmt.Printf("add 1 vote delegateRoot:%s\n", root3.Hex())
	delegates := delegateDb.GetDelegates()
	fmt.Printf("root3 delegates:%v\n", delegates)
	object := delegateDb.GetStateObject(address1)
	delegateDb.deleteStateObject(object)
	root4 := delegateDb.IntermediateRoot(false)
	delegateDb.CommitTo(db, false)
	objectDelegate := delegateDb.GetStateObject(address1)
	fmt.Printf("delete address root4 delegates:%v\n", root4.Hex())
	delegateDb.Finalise(false)
	fmt.Println(objectDelegate)
	delegates = delegateDb.GetDelegates()
	fmt.Printf("root4 delegates:%v\n", delegates)
	vote := delegateDb.GetVote(address1)
	fmt.Printf("delete vote:%v\n", vote)
	delegateDb.CommitTo(db, false)
	delegateRoot2Db, _ := New(root2, NewDatabase(db))
	vote2 := delegateRoot2Db.GetVote(address1)
	fmt.Printf("root2 vote2:%v\n", vote2)
	root2Delegates := delegateRoot2Db.GetDelegates()
	fmt.Printf("root2 delegates:%v\n", root2Delegates)
	delegateDb.GetOrNewStateObject(address1, "address1", uint64(time.Now().Unix()))
	delegates = delegateDb.GetDelegates()
	fmt.Printf("create again delegates:%v\n", delegates)
}

func TestDelegateDB_Suicide(t *testing.T) {
	db, _ := aoadb.NewMemDatabase()
	delegateDb, _ := New(common.Hash{}, NewDatabase(db))
	address1 := common.Address{1}

	root1 := delegateDb.IntermediateRoot(false)
	delegateDb.CommitTo(db, false)
	fmt.Printf("empty delegateRoot:%s\n", root1.Hex())
	if !delegateDb.Exist(address1) {
		delegateDb.GetOrNewStateObject(address1, "address1", uint64(time.Now().Unix()))
	}
	root2 := delegateDb.IntermediateRoot(false)
	delegateDb.CommitTo(db, false)
	fmt.Printf("create delegateRoot:%s\n", root2.Hex())

	object := delegateDb.GetStateObject(address1)
	object.AddVote(big.NewInt(2))

	root3 := delegateDb.IntermediateRoot(false)
	delegateDb.CommitTo(db, false)
	fmt.Printf("add vote delegateRoot:%s\n", root3.Hex())

	delegateDb.Suicide(address1)
	root4 := delegateDb.IntermediateRoot(false)
	delegateDb.CommitTo(db, false)
	fmt.Printf("delete delegateRoot:%s\n", root4.Hex())

	delegateRoot2Db, err := New(root3, NewDatabase(db))
	if err != nil {
		t.Fatal(err)
	}
	root5 := delegateRoot2Db.IntermediateRoot(false)
	fmt.Printf("revert delegateRoot:%s\n", root5.Hex())

	_, err = New(root4, NewDatabase(db))
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(root1, NewDatabase(db))
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(root2, NewDatabase(db))
	if err != nil {
		t.Fatal(err)
	}

	o := delegateDb.GetStateObject(address1)
	fmt.Println(o)

}

func TestIntermediateLeaks(t *testing.T) {
	trDb, _ := aoadb.NewMemDatabase()
	fiDb, _ := aoadb.NewMemDatabase()
	trDelegate, _ := New(common.Hash{}, NewDatabase(trDb))
	fiDelegate, _ := New(common.Hash{}, NewDatabase(fiDb))

	modify := func(delegate *DelegateDB, addr common.Address, i, tweak byte) {
		if !delegate.Exist(addr) {
			delegate.createObject(addr, strings.Join([]string{"node", string(i)}, ":"), uint64(time.Now().Unix()))
		}
		delegate.SetVote(addr, big.NewInt(int64(12*i)+int64(tweak)))
		if i%2 == 0 {
			delegate.SetState(addr, common.Hash{i, i, i, 0}, common.Hash{})
			delegate.SetState(addr, common.Hash{i, i, i, tweak}, common.Hash{i, i, i, i, tweak})
		}
	}

	for i := byte(0); i < 255; i++ {
		modify(trDelegate, common.Address{byte(i)}, i, 0)
	}
	// write modifications to trie
	trDelegate.IntermediateRoot(false)

	// Overwrite all data with new values in tr database
	for i := byte(0); i < 255; i++ {
		modify(trDelegate, common.Address{byte(i)}, i, 99)
		modify(fiDelegate, common.Address{byte(i)}, i, 99)
	}

	// Commit and cross check the databases
	if _, err := trDelegate.CommitTo(trDb, false); err != nil {
		t.Fatalf("failed to commit delegate state: %v", err)
	}

	if _, err := fiDelegate.CommitTo(fiDb, false); err != nil {
		t.Fatalf("failed to commit final state: %v", err)
	}

	for _, key := range fiDb.Keys() {
		if _, err := trDb.Get(key); err != nil {
			val, _ := fiDb.Get(key)
			t.Errorf("entry missing from the delegate database: %x -> %x", key, val)
		}
	}

	for _, key := range trDb.Keys() {
		if _, err := fiDb.Get(key); err != nil {
			val, _ := trDb.Get(key)
			t.Errorf("extra entry in the delegate database: %x -> %x", key, val)
		}
	}
}

// TestCopy tests theat copying a delegatestate object indeed makes the original and
// the copy independent of each other.
func TestCopy(t *testing.T) {
	mem, _ := aoadb.NewMemDatabase()
	orig, _ := New(common.Hash{}, NewDatabase(mem))

	for i := byte(0); i < 255; i++ {
		obj := orig.GetOrNewStateObject(common.BytesToAddress([]byte{i}), strings.Join([]string{"node", string(i)}, ":"), uint64(time.Now().Unix()))
		obj.AddVote(big.NewInt(int64(i)))
		orig.updateStateObject(obj)
	}

	orig.Finalise(false)

	// Copy the state,modify both in-memory
	cp := orig.Copy()

	for i := byte(0); i < 255; i++ {
		origObj := orig.GetOrNewStateObject(common.BytesToAddress([]byte{i}), strings.Join([]string{"node", string(i)}, ":"), uint64(time.Now().Unix()))
		copyObj := cp.GetOrNewStateObject(common.BytesToAddress([]byte{i}), strings.Join([]string{"node", string(i)}, ":"), uint64(time.Now().Unix()))

		origObj.AddVote(big.NewInt(2 * int64(i)))
		copyObj.AddVote(big.NewInt(2 * int64(i)))

		orig.updateStateObject(origObj)
		cp.updateStateObject(copyObj)
	}

	// Finalise the changes on both concurrently
	done := make(chan struct{})
	go func() {
		orig.Finalise(true)
		close(done)
	}()

	cp.Finalise(true)
	<-done

	// Verify that the two states have been updated independently
	for i := byte(0); i < 255; i++ {
		origObj := orig.GetOrNewStateObject(common.BytesToAddress([]byte{i}), strings.Join([]string{"node", string(i)}, ":"), uint64(time.Now().Unix()))
		copyObj := orig.GetOrNewStateObject(common.BytesToAddress([]byte{i}), strings.Join([]string{"node", string(i)}, ":"), uint64(time.Now().Unix()))

		if want := big.NewInt(3 * int64(i)); origObj.Vote().Cmp(want) != 0 {
			t.Errorf("orig obj %d: vote mismatch: have %v, want %v", i, origObj.Vote(), want)
		}

		if want := big.NewInt(3 * int64(i)); copyObj.Vote().Cmp(want) != 0 {
			t.Errorf("copy obj %d: vote mismatch: have %v, want %v", i, copyObj.Vote(), want)
		}

	}
}

func TestSnapshotRandom(t *testing.T) {
	config := &quick.Config{MaxCount: 1000}
	err := quick.Check((*snapshotTest).run, config)
	if cerr, ok := err.(*quick.CheckError); ok {
		test := cerr.In[0].(*snapshotTest)
		t.Errorf("%v:\n%s", test.err, test)
	} else if err != nil {
		t.Error(err)
	}

}

func TestCreateDelegates(t *testing.T) {
	db, err := aoadb.NewMemDatabase()
	if err != nil {
		t.Fatalf("create db err:%v\n", err)
	}
	delegatedb, err := New(common.Hash{}, NewDatabase(db))
	if err != nil {
		t.Fatalf("create delegateDB err:%v\n", err)
	}
	snapshot := delegatedb.Snapshot()
	d1 := delegatedb.GetOrNewStateObject(common.HexToAddress("0x71af77518da8ee1e152068ea4727d1041d71b813"), "node-1", 1492009146)
	d2 := delegatedb.GetOrNewStateObject(common.HexToAddress("0xa51bac4fe71640157f29317c2fe233c26b71c6c8"), "node-1", 1492009146)
	d3 := delegatedb.GetOrNewStateObject(common.HexToAddress("0xb0b81949b3b6d6ff926336d6227cec04ceca88b2"), "node-1", 1492009146)
	d4 := delegatedb.GetOrNewStateObject(common.HexToAddress("0x4d8bfcdbc0192e3a2e189ed133ee4e98e4e381f8"), "node-1", 1492009146)
	d5 := delegatedb.GetOrNewStateObject(common.HexToAddress("0xe92c157278abafa68e3547d4d5bd3ed4a5afccb3"), "node-1", 1492009146)
	d6 := delegatedb.GetOrNewStateObject(common.HexToAddress("0x5ac2ff101f11ae3c2b7093e25f5300018252c2a3"), "node-1", 1492009146)

	d1.AddVote(big.NewInt(1))
	d2.AddVote(big.NewInt(1))
	d3.AddVote(big.NewInt(1))
	d4.AddVote(big.NewInt(1))
	d5.AddVote(big.NewInt(1))
	d6.AddVote(big.NewInt(1))

	//delegatestate.IntermediateRoot(false)
	//root, _ := delegatestate.CommitTo(db, false)

	list := delegatedb.GetDelegates()

	fmt.Printf("candidateSize:%d\n", len(list))
	for _, v := range list {
		fmt.Printf("candidate:%v\n", v)
	}
	// delegatestate.Reset(root)
	// revertToSnapshot only can be use when not commit
	delegatedb.RevertToSnapshot(snapshot)
	list2 := delegatedb.GetDelegates()
	fmt.Printf("candidateSize:%d\n", len(list2))
	for _, v := range list2 {
		fmt.Printf("candidate:%v\n", v)
	}

}

type snapshotTest struct {
	addrs     []common.Address // all account addresses
	actions   []testAction     // modifications to the delegate db
	snapshots []int
	err       error // failure details are reported through this field
}

type testAction struct {
	name   string
	fn     func(action testAction, db *DelegateDB)
	args   []int64
	noAddr bool
}

func (test *snapshotTest) run() bool {
	// Run all actions and create snapshots.
	var (
		db, _        = aoadb.NewMemDatabase()
		state, _     = New(common.Hash{}, NewDatabase(db))
		snapshotRevs = make([]int, len(test.snapshots))
		sindex       = 0
	)
	for i, action := range test.actions {
		if len(test.snapshots) > sindex && i == test.snapshots[sindex] {
			snapshotRevs[sindex] = state.Snapshot()
			sindex++
		}
		action.fn(action, state)
	}

	// Revert all snapshots in reverse order. Each revert must yield a state
	// that is equivalent to fresh state with all actions up the snapshot applied.
	for sindex--; sindex >= 0; sindex-- {
		checkstate, _ := New(common.Hash{}, NewDatabase(db))
		for _, action := range test.actions[:test.snapshots[sindex]] {
			action.fn(action, checkstate)
		}
		state.RevertToSnapshot(snapshotRevs[sindex])
		if err := test.checkEqual(state, checkstate); err != nil {
			test.err = fmt.Errorf("state mismatch after revert to snapshot %d\n%v", sindex, err)
			return false
		}
	}
	return true
}

// checkEqual checks that methods of state and checkstate return the same values.
func (test *snapshotTest) checkEqual(state, checkstate *DelegateDB) error {
	for _, addr := range test.addrs {
		var err error
		checkeq := func(op string, a, b interface{}) bool {
			if err == nil && !reflect.DeepEqual(a, b) {
				err = fmt.Errorf("got %s(%s) == %v, want %v", op, addr.Hex(), a, b)
				return false
			}
			return true
		}

		// Check basic accessor methods.
		checkeq("Exist", state.Exist(addr), checkstate.Exist(addr))
		checkeq("GetVote", state.GetVote(addr), checkstate.GetVote(addr))
		// Check storage.
		if obj := state.GetStateObject(addr); obj != nil {
			state.ForEachStorage(addr, func(key, val common.Hash) bool {
				return checkeq("GetState("+key.Hex()+")", val, checkstate.GetState(addr, key))
			})
			checkstate.ForEachStorage(addr, func(key, checkval common.Hash) bool {
				return checkeq("GetState("+key.Hex()+")", state.GetState(addr, key), checkval)
			})
		}
		if err != nil {
			return err
		}
	}
	if !reflect.DeepEqual(state.GetLogs(common.Hash{}), checkstate.GetLogs(common.Hash{})) {
		return fmt.Errorf("got GetLogs(common.Hash{}) == %v, want GetLogs(common.Hash{}) == %v",
			state.GetLogs(common.Hash{}), checkstate.GetLogs(common.Hash{}))
	}
	return nil
}

func (test *snapshotTest) String() string {
	out := new(bytes.Buffer)
	sindex := 0
	for i, action := range test.actions {
		if len(test.snapshots) > sindex && i == test.snapshots[sindex] {
			fmt.Fprintf(out, "---- snapshot %d ----\n", sindex)
			sindex++
		}
		fmt.Fprintf(out, "%4d: %s\n", i, action.name)
	}
	return out.String()
}

// newTestAction creates a random action that changes state.
func newTestAction(addr common.Address, r *rand.Rand) testAction {
	actions := []testAction{
		{
			name: "SetVote",
			fn: func(a testAction, d *DelegateDB) {
				d.SetVote(addr, big.NewInt(a.args[0]))
			},
			args: make([]int64, 1),
		},
		{
			name: "AddVote",
			fn: func(a testAction, d *DelegateDB) {
				d.AddVote(addr, big.NewInt(a.args[0]))
			},
			args: make([]int64, 1),
		},
		{
			name: "SetState",
			fn: func(a testAction, d *DelegateDB) {
				var key, val common.Hash
				binary.BigEndian.PutUint16(key[:], uint16(a.args[0]))
				binary.BigEndian.PutUint16(val[:], uint16(a.args[1]))
				d.SetState(addr, key, val)
			},
			args: make([]int64, 2),
		},
		{
			name: "AddLog",
			fn: func(a testAction, d *DelegateDB) {
				data := make([]byte, 2)
				binary.BigEndian.PutUint16(data, uint16(a.args[0]))
				d.AddLog(&types.Log{Address: addr, Data: data})
			},
			args: make([]int64, 1),
		},
	}
	action := actions[r.Intn(len(actions))]
	var nameargs []string
	if !action.noAddr {
		nameargs = append(nameargs, addr.Hex())
	}
	for _, i := range action.args {
		action.args[i] = rand.Int63n(100)
		nameargs = append(nameargs, fmt.Sprint(action.args[i]))
	}
	action.name += strings.Join(nameargs, ", ")
	return action
}

// Generate returns a new snapshot test of the given size. All randomness is
// derived from r.
func (*snapshotTest) Generate(r *rand.Rand, size int) reflect.Value {
	// Generate random actions.
	addrs := make([]common.Address, 50)
	for i := range addrs {
		addrs[i][0] = byte(i)
	}
	actions := make([]testAction, size)
	for i := range actions {
		addr := addrs[r.Intn(len(addrs))]
		actions[i] = newTestAction(addr, r)
	}
	// Generate snapshot indexes.
	nsnapshots := int(math.Sqrt(float64(size)))
	if size > 0 && nsnapshots == 0 {
		nsnapshots = 1
	}
	snapshots := make([]int, nsnapshots)
	snaplen := len(actions) / nsnapshots
	for i := range snapshots {
		// Try to place the snapshots some number of actions apart from each other.
		snapshots[i] = (i * snaplen) + r.Intn(snaplen)
	}
	return reflect.ValueOf(&snapshotTest{addrs, actions, snapshots, nil})
}
