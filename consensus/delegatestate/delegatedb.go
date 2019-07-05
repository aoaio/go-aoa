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
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/rlp"
	"github.com/Aurorachain/go-aoa/trie"
	"math/big"
	"sort"
	"sync"
	"time"
)

type revision struct {
	id           int
	journalIndex int
}

type DelegateState interface {
	SubVote(addr common.Address, amount *big.Int)
	AddVote(addr common.Address, amount *big.Int)
	GetVote(addr common.Address) *big.Int

	GetState(common.Address, common.Hash) common.Hash
	SetState(common.Address, common.Hash, common.Hash)

	// Exist reports whether the given account exists in state.
	// Notably this should also return true for suicided accounts.
	Exist(common.Address) bool

	RevertToSnapshot(int)
	Snapshot() int

	AddLog(*types.Log)

	Suicide(common.Address) bool
	HasSuicided(common.Address) bool

	ForEachStorage(common.Address, func(common.Hash, common.Hash) bool)
}

type DelegateDB struct {
	db   Database
	trie Trie

	// This map holds 'live' objects, which will get modified while processing a state transition.
	delegateObjects      map[common.Address]*delegateObject
	delegateObjectsDirty map[common.Address]struct{}

	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by DelegateDB.Commit.
	dbErr error

	thash, bhash common.Hash
	txIndex      int
	logs         map[common.Hash][]*types.Log
	logSize      uint

	// Journal of state modifications. This is the backbone of
	// Snapshot and RevertToSnapshot.
	journal        journal
	validRevisions []revision
	nextRevisionId int

	lock sync.Mutex
}

type Storage map[common.Hash]common.Hash

// Create a new state from a given trie
func New(root common.Hash, db Database) (*DelegateDB, error) {
	tr, err := db.OpenTrie(root)
	if err != nil {
		return nil, err
	}

	dState := &DelegateDB{
		db:                   db,
		trie:                 tr,
		delegateObjects:      make(map[common.Address]*delegateObject),
		delegateObjectsDirty: make(map[common.Address]struct{}),
		logs:                 make(map[common.Hash][]*types.Log),
	}
	dState.loadDelegateToCache()
	return dState, nil
}

func (d *DelegateDB) loadDelegateToCache() {
	it := trie.NewIterator(d.trie.NodeIterator(nil))
	for it.Next() {
		addr := d.trie.GetKey(it.Key)
		d.GetStateObject(common.BytesToAddress(addr))
	}
}

// setError remembers the first non-nil error it is called with.
func (d *DelegateDB) setError(err error) {
	if d.dbErr == nil {
		d.dbErr = err
	}
}

func (d *DelegateDB) Error() error {
	return d.dbErr
}

// Reset clears out all emphemeral state objects from the delegate db, but keeps
// the underlying state trie to avoid reloading data for the next operations.
func (d *DelegateDB) Reset(root common.Hash) error {
	tr, err := d.db.OpenTrie(root)
	if err != nil {
		return err
	}
	d.trie = tr
	d.delegateObjects = make(map[common.Address]*delegateObject)
	d.delegateObjectsDirty = make(map[common.Address]struct{})
	d.thash = common.Hash{}
	d.bhash = common.Hash{}
	d.txIndex = 0
	d.logs = make(map[common.Hash][]*types.Log)
	d.logSize = 0
	d.clearJournal()
	return nil
}

func (d *DelegateDB) AddLog(log *types.Log) {
	d.journal = append(d.journal, addLogChange{txhash: d.thash})

	log.TxHash = d.thash
	log.BlockHash = d.bhash
	log.TxIndex = uint(d.txIndex)
	log.Index = d.logSize
	d.logs[d.thash] = append(d.logs[d.thash], log)
	d.logSize++
}

func (d *DelegateDB) GetLogs(hash common.Hash) []*types.Log {
	return d.logs[hash]
}

func (d *DelegateDB) Logs() []*types.Log {
	var logs []*types.Log
	for _, lgs := range d.logs {
		logs = append(logs, lgs...)
	}
	return logs
}

// get current sort delegates
func (d *DelegateDB) GetDelegates() []types.Candidate {
	list := make([]types.Candidate, 0)

	for k, v := range d.delegateObjects {
		if v.data.Delete || v.suicided {
			continue
		}
		candidate := types.Candidate{Address: k.Hex(), Vote: v.data.Vote.Uint64(), Nickname: v.data.Nickname, RegisterTime: v.data.RegisterTime}
		list = append(list, candidate)
	}
	if len(list) > 1 {
		sort.Sort(types.CandidateSlice(list))
	}
	return list
}

func (d *DelegateDB) clearJournal() {
	d.journal = nil
	d.validRevisions = d.validRevisions[:0]
}

// Exist reports whether the given account address exists in the state.
// Notably this also returns true for suicided accounts.
func (d *DelegateDB) Exist(addr common.Address) bool {
	return d.GetStateObject(addr) != nil
}

func (d *DelegateDB) SubExist(addr common.Address) bool {
	return d.getStateObjectContainDelete(addr) != nil
}

func (d *DelegateDB) getStateObjectContainDelete(addr common.Address) (delegateObject *delegateObject) {
	if obj := d.delegateObjects[addr]; obj != nil {
		log.Infof("getStateObjectContainDelete, stateObj=%s", obj)
		return obj
	}
	// Load the object from the database.
	enc, err := d.trie.TryGet(addr[:])
	if len(enc) == 0 {
		d.setError(err)
		return nil
	}
	var data Delegate
	if err := rlp.DecodeBytes(enc, &data); err != nil {
		log.Error("Failed to decode delegate object", "addr", addr, "err", err)
		return nil
	}
	// Insert into the live set.
	obj := newObject(d, addr, data, d.MarkStateObjectDirty)
	d.setStateObject(obj)
	return obj
}

// Retrieve a state object given my the address. Returns nil if not found.
func (d *DelegateDB) GetStateObject(addr common.Address) (delegateObject *delegateObject) {
	// Prefer 'live' objects.
	if obj := d.delegateObjects[addr]; obj != nil {
		if obj.deleted || obj.data.Delete {
			return nil
		}
		return obj
	}
	// Load the object from the database.
	enc, err := d.trie.TryGet(addr[:])
	if len(enc) == 0 {
		d.setError(err)
		return nil
	}
	var data Delegate
	if err := rlp.DecodeBytes(enc, &data); err != nil {
		log.Error("Failed to decode delegate object", "addr", addr, "err", err)
		return nil
	}
	// Insert into the live set.
	obj := newObject(d, addr, data, d.MarkStateObjectDirty)
	d.setStateObject(obj)
	return obj
}

func (d *DelegateDB) updateStateObject(delegateObject *delegateObject) {
	addr := delegateObject.Address()
	data, err := rlp.EncodeToBytes(delegateObject)
	if err != nil {
		panic(fmt.Errorf("can't encode object at %x: %v", addr[:], err))
	}
	d.setError(d.trie.TryUpdate(addr[:], data))
}

func (d *DelegateDB) deleteStateObject(delegateObject *delegateObject) {
	delegateObject.deleted = true
	addr := delegateObject.Address()
	d.setError(d.trie.TryDelete(addr[:]))
}

func (d *DelegateDB) setStateObject(object *delegateObject) {
	d.delegateObjects[object.Address()] = object
}

// Retrieve a state object or create a new state object if nil
func (d *DelegateDB) GetOrNewStateObject(addr common.Address, nickname string, registerTime uint64) *delegateObject {
	stateObject := d.getStateObjectContainDelete(addr)
	if stateObject == nil {
		stateObject, _ = d.createObject(addr, nickname, registerTime)
		log.Infof("delegateDB|registerDelegateSuccess, address=%s, nickname=%s,registerTime=%v", addr.Hex(),  nickname,  time.Unix(int64(registerTime), 0))
		return stateObject
	}
	if stateObject.data.Delete {
		preVote := stateObject.Vote()
		stateObject, _ = d.createObject(addr, nickname, registerTime)
		d.AddVote(addr, preVote)
		log.Infof("delegateDB|registerDelegateSuccess again, address=%s, nickname=%s,registerTime=%v", addr.Hex(),  nickname,  time.Unix(int64(registerTime),0))

	}
	return stateObject
}

func (d *DelegateDB) GetState(a common.Address, b common.Hash) common.Hash {
	stateObject := d.GetStateObject(a)
	if stateObject != nil {
		return stateObject.GetState(d.db, b)
	}
	return common.Hash{}
}

func (d *DelegateDB) SetState(addr common.Address, key common.Hash, value common.Hash) {
	stateObject := d.GetStateObject(addr)
	if stateObject != nil {
		stateObject.SetState(d.db, key, value)
	}
}

func (d *DelegateDB) GetVote(addr common.Address) *big.Int {
	delegateObject := d.getStateObjectContainDelete(addr)
	if delegateObject != nil {
		return delegateObject.Vote()
	}
	return common.Big0
}

func (d *DelegateDB) AddVote(addr common.Address, amount *big.Int) {
	delegateObject := d.GetStateObject(addr)
	if delegateObject != nil {
		delegateObject.AddVote(amount)
		delegateObject.tryMarkDirty()
	}
}

func (d *DelegateDB) SubVote(addr common.Address, amount *big.Int) {
	delegateObject := d.getStateObjectContainDelete(addr)
	if delegateObject != nil {
		delegateObject.SubVote(amount)
		delegateObject.tryMarkDirty()
	}
}

func (d *DelegateDB) SetVote(addr common.Address, amount *big.Int) {
	delegateObject := d.GetStateObject(addr)
	if delegateObject != nil {
		delegateObject.setVote(amount)
		delegateObject.tryMarkDirty()
	}
}

func (d *DelegateDB) Suicide(addr common.Address) bool {
	stateObject := d.GetStateObject(addr)
	if stateObject == nil {
		return false
	}
	d.journal = append(d.journal, suicideChange{
		account:  &addr,
		prev:     stateObject.suicided,
		prevVote: new(big.Int).Set(stateObject.Vote()),
	})
	stateObject.data.Delete = true
	stateObject.tryMarkDirty()
	// stateObject.markSuicided()
	// stateObject.data.Vote = new(big.Int)
	return true
}

func (d *DelegateDB) HasSuicided(addr common.Address) bool {
	stateObject := d.GetStateObject(addr)
	if stateObject != nil {
		return stateObject.data.Delete
	}
	return false
}

// createObject creates a new state object. If there is an existing account with
// the given address, it is overwritten and returned as the second return value.
func (d *DelegateDB) createObject(addr common.Address, nickname string, registerTime uint64) (newobj, prev *delegateObject) {
	prev = d.GetStateObject(addr)
	newobj = newObject(d, addr, Delegate{Nickname: nickname, RegisterTime: registerTime, Vote: big.NewInt(0)}, d.MarkStateObjectDirty)
	if prev == nil {
		d.journal = append(d.journal, createObjectChange{account: &addr})
	} else {
		d.journal = append(d.journal, resetObjectChange{prev: prev})
	}
	d.setStateObject(newobj)
	return newobj, prev
}

// MarkStateObjectDirty adds the specified object to the dirty map to avoid costly
// state object cache iteration to find a handful of modified ones.
func (d *DelegateDB) MarkStateObjectDirty(addr common.Address) {
	d.delegateObjectsDirty[addr] = struct{}{}
}

func (d *DelegateDB) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) {
	so := d.GetStateObject(addr)
	if so == nil {
		return
	}

	// When iterating over the storage check the cache first
	for h, value := range so.cachedStorage {
		cb(h, value)
	}

	it := trie.NewIterator(so.getTrie(d.db).NodeIterator(nil))
	for it.Next() {
		// ignore cached values
		key := common.BytesToHash(d.trie.GetKey(it.Key))
		if _, ok := so.cachedStorage[key]; !ok {
			cb(key, common.BytesToHash(it.Value))
		}
	}
}

func (d *DelegateDB) Copy() *DelegateDB {
	d.lock.Lock()
	defer d.lock.Unlock()

	// Copy all the basic fields, initialize the memory ones
	state := &DelegateDB{
		db:                   d.db,
		trie:                 d.db.CopyTrie(d.trie),
		delegateObjects:      make(map[common.Address]*delegateObject, len(d.delegateObjectsDirty)),
		delegateObjectsDirty: make(map[common.Address]struct{}, len(d.delegateObjectsDirty)),
		logs:                 make(map[common.Hash][]*types.Log, len(d.logs)),
		logSize:              d.logSize,
	}
	// Copy the dirty states, logs, and preimages
	for addr := range d.delegateObjectsDirty {
		state.delegateObjects[addr] = d.delegateObjects[addr].deepCopy(state, state.MarkStateObjectDirty)
		state.delegateObjectsDirty[addr] = struct{}{}
	}
	for hash, logs := range d.logs {
		state.logs[hash] = make([]*types.Log, len(logs))
		copy(state.logs[hash], logs)
	}
	return state
}

// Snapshot returns an identifier for the current revision of the state.
func (d *DelegateDB) Snapshot() int {
	id := d.nextRevisionId
	d.nextRevisionId++
	d.validRevisions = append(d.validRevisions, revision{id, len(d.journal)})
	return id
}

// RevertToSnapshot reverts all state changes made since the given revision.
func (d *DelegateDB) RevertToSnapshot(revid int) {
	// Find the snapshot in the stack of valid snapshots.
	idx := sort.Search(len(d.validRevisions), func(i int) bool {
		return d.validRevisions[i].id >= revid
	})
	if idx == len(d.validRevisions) || d.validRevisions[idx].id != revid {
		panic(fmt.Errorf("revision id %v cannot be reverted", revid))
	}
	snapshot := d.validRevisions[idx].journalIndex

	// Replay the journal to undo changes.
	for i := len(d.journal) - 1; i >= snapshot; i-- {
		d.journal[i].undo(d)
	}
	d.journal = d.journal[:snapshot]

	// Remove invalidated snapshots from the stack.
	d.validRevisions = d.validRevisions[:idx]
}

// Finalise finalises the state by removing the self destructed objects
// and clears the journal as well as the refunds.
func (d *DelegateDB) Finalise(deleteEmptyObjects bool) {
	for addr := range d.delegateObjectsDirty {
		delegateObject := d.delegateObjects[addr]
		if delegateObject.suicided {
			log.Infof("DelegateDB|Finalise|delete, addr=%v", addr)
			d.deleteStateObject(delegateObject)
		} else {
			delegateObject.updateRoot(d.db)
			d.updateStateObject(delegateObject)
		}
	}
	d.clearJournal()
}

func (d *DelegateDB) IntermediateRoot(deleteEmptyObjects bool) common.Hash {
	d.Finalise(deleteEmptyObjects)
	return d.trie.Hash()
}

func (d *DelegateDB) Prepare(thash, bhash common.Hash, ti int) {
	d.thash = thash
	d.bhash = bhash
	d.txIndex = ti
}

// CommitTo writes the state to the given database.
func (d *DelegateDB) CommitTo(dbw trie.DatabaseWriter, deleteEmptyObjects bool) (root common.Hash, err error) {
	defer d.clearJournal()

	// Commit objects to the trie.
	for addr, delegateObject := range d.delegateObjects {
		_, isDirty := d.delegateObjectsDirty[addr]
		switch {
		case delegateObject.suicided:
			// If the object has been removed, don't bother syncing it
			// and just mark it for deletion in the trie.
			d.deleteStateObject(delegateObject)
		case isDirty:
			// Write any storage changes in the state object to its storage trie.
			if err := delegateObject.CommitTrie(d.db, dbw); err != nil {
				return common.Hash{}, err
			}
			// Update the object in the main account trie.
			d.updateStateObject(delegateObject)
		}
		delete(d.delegateObjectsDirty, addr)

	}
	// Write trie changes.
	root, err = d.trie.CommitTo(dbw)
	log.Infof("delegate Trie commit, rootHash=%v, err=%v", root.Hex(), err)
	log.Infof("delegate Trie cache stats after commit, misses=%v, unloads=%v", trie.CacheMisses(), trie.CacheUnloads())
	return root, err
}

func (storage Storage) String() (str string) {
	for key, value := range storage {
		str += fmt.Sprintf("%X : %X\n", key, value)
	}

	return
}

func (storage Storage) Copy() Storage {
	cpy := make(Storage)
	for key, value := range storage {
		cpy[key] = value
	}

	return cpy
}
