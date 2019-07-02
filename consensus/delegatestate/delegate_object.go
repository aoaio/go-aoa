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
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/rlp"
	"github.com/Aurorachain/go-aoa/trie"
	"io"
	"math/big"
)

type delegateObject struct {
	address  common.Address
	addrHash common.Hash // hash of Aurora address of the account
	data     Delegate
	db       *DelegateDB

	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by DelegateDB.Commit.
	dbErr error

	// Write caches.
	trie          Trie    // storage trie, which becomes non-nil on first access
	cachedStorage Storage // Storage entry cache to avoid duplicate reads
	dirtyStorage  Storage // Storage entries that need to be flushed to disk

	touched  bool
	deleted  bool
	suicided bool
	onDirty  func(addr common.Address) // Callback method to mark a state object newly dirty
}

type Delegate struct {
	Root         common.Hash // merkle root of the storage trie
	Vote         *big.Int    // vote number
	Nickname     string      // delegate name
	RegisterTime uint64      // delegate register time
	Delete       bool        // whether delete
}

// Returns the address of the contract/account
func (delegateObject *delegateObject) Address() common.Address {
	return delegateObject.address
}

// newObject creates a state object.
func newObject(db *DelegateDB, address common.Address, delegate Delegate, onDirty func(addr common.Address)) *delegateObject {
	if delegate.Vote == nil {
		delegate.Vote = big.NewInt(0)
	}
	dObject := &delegateObject{
		db:            db,
		address:       address,
		addrHash:      crypto.Keccak256Hash(address[:]),
		data:          delegate,
		cachedStorage: make(Storage),
		dirtyStorage:  make(Storage),
		onDirty:       onDirty,
	}
	dObject.tryMarkDirty()
	return dObject
}

func (delegateObject *delegateObject) getTrie(db Database) Trie {
	if delegateObject.trie == nil {
		var err error
		delegateObject.trie, err = db.OpenStorageTrie(delegateObject.addrHash, delegateObject.data.Root)
		if err != nil {
			delegateObject.trie, _ = db.OpenStorageTrie(delegateObject.addrHash, common.Hash{})
			delegateObject.setError(fmt.Errorf("can't create storage trie: %v", err))
		}
	}
	return delegateObject.trie
}

func (d *delegateObject) touch() {
	d.db.journal = append(d.db.journal, touchChange{
		account:   &d.address,
		prev:      d.touched,
		prevDirty: d.onDirty == nil,
	})
	if d.onDirty != nil {
		d.onDirty(d.Address())
		d.onDirty = nil
	}
	d.touched = true
}

func (d *delegateObject) Vote() *big.Int {
	return new(big.Int).Set(d.data.Vote)
}

func (d *delegateObject) SubVote(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	result := new(big.Int).Sub(d.Vote(), amount)
	if result.Cmp(common.Big0) < 0 {
		d.setVote(common.Big0)
	} else {
		d.setVote(result)
	}

}

func (d *delegateObject) AddVote(amount *big.Int) {
	if amount.Sign() == 0 {
		if d.empty() {
			d.touch()
		}

		return
	}
	d.setVote(new(big.Int).Add(d.Vote(), amount))
}

func (d *delegateObject) SetVote(amount *big.Int) {
	d.db.journal = append(d.db.journal, voteChange{
		account: &d.address,
		prev:    new(big.Int).Set(d.data.Vote),
	})
	d.setVote(amount)
}

func (d *delegateObject) setVote(amount *big.Int) {
	d.data.Vote = amount
	if d.onDirty != nil {
		d.onDirty(d.Address())
		d.onDirty = nil
	}
}

// GetState returns a value in account storage.
func (delegateObject *delegateObject) GetState(db Database, key common.Hash) common.Hash {
	value, exists := delegateObject.cachedStorage[key]
	if exists {
		return value
	}
	// Load from DB in case it is missing.
	enc, err := delegateObject.getTrie(db).TryGet(key[:])
	if err != nil {
		delegateObject.setError(err)
		return common.Hash{}
	}
	if len(enc) > 0 {
		_, content, _, err := rlp.Split(enc)
		if err != nil {
			delegateObject.setError(err)
		}
		value.SetBytes(content)
	}
	if (value != common.Hash{}) {
		delegateObject.cachedStorage[key] = value
	}
	return value
}

// SetState updates a value in account storage.
func (delegateObject *delegateObject) SetState(db Database, key, value common.Hash) {
	delegateObject.db.journal = append(delegateObject.db.journal, storageChange{
		account:  &delegateObject.address,
		key:      key,
		prevalue: delegateObject.GetState(db, key),
	})
	delegateObject.setState(key, value)
}

func (delegateObject *delegateObject) setState(key, value common.Hash) {
	delegateObject.cachedStorage[key] = value
	delegateObject.dirtyStorage[key] = value

	if delegateObject.onDirty != nil {
		delegateObject.onDirty(delegateObject.Address())
		delegateObject.onDirty = nil
	}
}

// updateTrie writes cached storage modifications into the object's storage trie.
func (delegateObject *delegateObject) updateTrie(db Database) Trie {
	tr := delegateObject.getTrie(db)
	for key, value := range delegateObject.dirtyStorage {
		delete(delegateObject.dirtyStorage, key)
		if (value == common.Hash{}) {
			delegateObject.setError(tr.TryDelete(key[:]))
			continue
		}
		// Encoding []byte cannot fail, ok to ignore the error.
		v, _ := rlp.EncodeToBytes(bytes.TrimLeft(value[:], "\x00"))
		delegateObject.setError(tr.TryUpdate(key[:], v))
	}
	return tr
}

// UpdateRoot sets the trie root to the current root hash of
func (delegateObject *delegateObject) updateRoot(db Database) {
	delegateObject.updateTrie(db)
	delegateObject.data.Root = delegateObject.trie.Hash()
}

// CommitTrie the storage trie of the object to dwb.
// This updates the trie root.
func (delegateObject *delegateObject) CommitTrie(db Database, dbw trie.DatabaseWriter) error {
	delegateObject.updateTrie(db)
	if delegateObject.dbErr != nil {
		return delegateObject.dbErr
	}
	root, err := delegateObject.trie.CommitTo(dbw)
	if err == nil {
		delegateObject.data.Root = root
	}
	return err
}

func (d *delegateObject) markSuicided() {
	d.suicided = true
	d.tryMarkDirty()
}

func (delegateObject *delegateObject) deepCopy(db *DelegateDB, onDirty func(addr common.Address)) *delegateObject {
	stateObject := newObject(db, delegateObject.address, delegateObject.data, onDirty)
	if delegateObject.trie != nil {
		stateObject.trie = db.db.CopyTrie(delegateObject.trie)
	}

	stateObject.dirtyStorage = delegateObject.dirtyStorage.Copy()
	stateObject.cachedStorage = delegateObject.dirtyStorage.Copy()
	stateObject.deleted = delegateObject.deleted
	return stateObject
}

func (delegateObject *delegateObject) empty() bool {
	return false
}

// setError remembers the first non-nil error it is called with.
func (delegateObject *delegateObject) setError(err error) {
	if delegateObject.dbErr == nil {
		delegateObject.dbErr = err
	}
}

// EncodeRLP implements rlp.Encoder.
func (delegateObject *delegateObject) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, delegateObject.data)
}

func (delegateObject *delegateObject) tryMarkDirty() {
	if delegateObject.onDirty != nil {
		delegateObject.onDirty(delegateObject.Address())
		delegateObject.onDirty = nil
	}
}
