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

package state

import (
	"bytes"
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/rlp"
	"github.com/Aurorachain/go-aoa/trie"
	"io"
	"math/big"
)

var emptyCodeHash = crypto.Keccak256(nil)

type Code []byte

func (self Code) String() string {
	return string(self) //strings.Join(Disassemble(self), " ")
}

type Storage map[common.Hash]common.Hash

func (self Storage) String() (str string) {
	for key, value := range self {
		str += fmt.Sprintf("%X : %X\n", key, value)
	}

	return
}

func (self Storage) Copy() Storage {
	cpy := make(Storage)
	for key, value := range self {
		cpy[key] = value
	}

	return cpy
}

// stateObject represents an Aurora account which is being modified.
//
// The usage pattern is as follows:
// First you need to obtain a state object.
// Account values can be accessed and modified through the object.
// Finally, call CommitTrie to write the modified storage trie into a database.
type stateObject struct {
	address  common.Address
	addrHash common.Hash // hash of aurora address of the account
	data     Account
	db       *StateDB

	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by StateDB.Commit.
	dbErr error

	// Write caches.
	trie      Trie   // storage trie, which becomes non-nil on first access
	code      Code   // contract bytecode, which gets set when code is loaded
	abi       string // contract abi, can be "".
	assetData []byte //asset data, only when the account is a assetAccount that this data has values.

	cachedStorage Storage // Storage entry cache to avoid duplicate reads
	dirtyStorage  Storage // Storage entries that need to be flushed to disk

	// Cache flags.
	// When an object is marked suicided it will be delete from the trie
	// during the "update" phase of the state transition.
	dirtyCode      bool // true if the code was updated
	suicided       bool
	touched        bool
	deleted        bool
	dirtyAssetData bool
	onDirty        func(addr common.Address) // Callback method to mark a state object newly dirty
}

// empty returns whether the account is considered empty.
func (s *stateObject) empty() bool {
	return s.data.Nonce == 0 && s.data.Balance.Sign() == 0 && bytes.Equal(s.data.CodeHash, emptyCodeHash) && s.data.AssetList.IsEmpty()
}

// Account is the Aurora consensus representation of accounts.
// These objects are stored in the main account trie.
type Account struct {
	Nonce       uint64
	Balance     *big.Int
	Root        common.Hash // merkle root of the storage trie
	CodeHash    []byte
	LockBalance *big.Int         // lock vote balance
	VoteList    []common.Address // vote list
	AssetList   *types.Assets    // ascending alphabet ordered Assets.
	AssetHash   []byte
}

// newObject creates a state object.
func newObject(db *StateDB, address common.Address, data Account, onDirty func(addr common.Address)) *stateObject {
	if data.Balance == nil {
		data.Balance = new(big.Int)
	}
	if data.CodeHash == nil {
		data.CodeHash = emptyCodeHash
	}
	if data.AssetList == nil {
		data.AssetList = types.NewAssets()
	}
	return &stateObject{
		db:            db,
		address:       address,
		addrHash:      crypto.Keccak256Hash(address[:]),
		data:          data,
		cachedStorage: make(Storage),
		dirtyStorage:  make(Storage),
		onDirty:       onDirty,
	}
}

// EncodeRLP implements rlp.Encoder.
func (c *stateObject) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, c.data)
}

// setError remembers the first non-nil error it is called with.
func (self *stateObject) setError(err error) {
	if self.dbErr == nil {
		self.dbErr = err
	}
}

//tryMarkDirty trying to mark this object as dirty, at most once.
func (self *stateObject) tryMarkDirty() {
	if self.onDirty != nil {
		self.onDirty(self.Address())
		self.onDirty = nil
	}
}

func (self *stateObject) markSuicided() {
	self.suicided = true
	self.tryMarkDirty()
}

func (self *stateObject) touch() {
	self.db.journal = append(self.db.journal, touchChange{
		account:   &self.address,
		prev:      self.touched,
		prevDirty: self.onDirty == nil,
	})
	self.tryMarkDirty()
	self.touched = true
}

func (c *stateObject) getTrie(db Database) Trie {
	if c.trie == nil {
		var err error
		c.trie, err = db.OpenStorageTrie(c.addrHash, c.data.Root)
		if err != nil {
			c.trie, _ = db.OpenStorageTrie(c.addrHash, common.Hash{})
			c.setError(fmt.Errorf("can't create storage trie: %v", err))
		}
	}
	return c.trie
}

// GetState returns a value in account storage.
func (self *stateObject) GetState(db Database, key common.Hash) common.Hash {
	value, exists := self.cachedStorage[key]
	if exists {
		return value
	}
	// Load from DB in case it is missing.
	enc, err := self.getTrie(db).TryGet(key[:])
	if err != nil {
		self.setError(err)
		return common.Hash{}
	}
	if len(enc) > 0 {
		_, content, _, err := rlp.Split(enc)
		if err != nil {
			self.setError(err)
		}
		value.SetBytes(content)
	}
	if (value != common.Hash{}) {
		self.cachedStorage[key] = value
	}
	return value
}

// SetState updates a value in account storage.
func (self *stateObject) SetState(db Database, key, value common.Hash) {
	self.db.journal = append(self.db.journal, storageChange{
		account:  &self.address,
		key:      key,
		prevalue: self.GetState(db, key),
	})
	self.setState(key, value)
}

func (self *stateObject) setState(key, value common.Hash) {
	self.cachedStorage[key] = value
	self.dirtyStorage[key] = value

	self.tryMarkDirty()
}

// updateTrie writes cached storage modifications into the object's storage trie.
func (self *stateObject) updateTrie(db Database) Trie {
	tr := self.getTrie(db)
	for key, value := range self.dirtyStorage {
		delete(self.dirtyStorage, key)
		if (value == common.Hash{}) {
			self.setError(tr.TryDelete(key[:]))
			continue
		}
		// Encoding []byte cannot fail, ok to ignore the error.
		v, _ := rlp.EncodeToBytes(bytes.TrimLeft(value[:], "\x00"))
		self.setError(tr.TryUpdate(key[:], v))
	}
	return tr
}

// UpdateRoot sets the trie root to the current root hash of
func (self *stateObject) updateRoot(db Database) {
	self.updateTrie(db)
	self.data.Root = self.trie.Hash()
}

// CommitTrie the storage trie of the object to dwb.
// This updates the trie root.
func (self *stateObject) CommitTrie(db Database, dbw trie.DatabaseWriter) error {
	self.updateTrie(db)
	if self.dbErr != nil {
		return self.dbErr
	}
	root, err := self.trie.CommitTo(dbw)
	if err == nil {
		self.data.Root = root
	}
	return err
}

// AddBalance removes amount from c's balance.
// It is used to add funds to the destination account of a transfer.
func (c *stateObject) AddBalance(amount *big.Int) {
	// EIP158: We must check emptiness for the objects such that the account
	// clearing (0,0,0 objects) can take effect.
	if amount.Sign() == 0 {
		if c.empty() {
			c.touch()
		}

		return
	}
	c.SetBalance(new(big.Int).Add(c.Balance(), amount))
}

// SubBalance removes amount from c's balance.
// It is used to remove funds from the origin account of a transfer.
func (c *stateObject) SubBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	c.SetBalance(new(big.Int).Sub(c.Balance(), amount))
}

func (self *stateObject) SetBalance(amount *big.Int) {
	self.db.journal = append(self.db.journal, balanceChange{
		account: &self.address,
		prev:    new(big.Int).Set(self.data.Balance),
	})
	self.setBalance(amount)
}

func (self *stateObject) setBalance(amount *big.Int) {
	self.data.Balance = amount
	self.tryMarkDirty()
}

func (self *stateObject) AddLockBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		if self.empty() {
			self.touch()
		}

		return
	}
	self.SetLockBalance(new(big.Int).Add(self.LockBalance(), amount))
}

func (self *stateObject) SubLockBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	self.SetLockBalance(new(big.Int).Sub(self.LockBalance(), amount))
}

func (self *stateObject) SetLockBalance(amount *big.Int) {
	self.db.journal = append(self.db.journal, lockBalanceChange{
		account: &self.address,
		prev:    new(big.Int).Set(self.data.LockBalance),
	})
	self.setLockBalance(amount)
}

func (self *stateObject) setLockBalance(amount *big.Int) {
	self.data.LockBalance = amount
	self.tryMarkDirty()
}

// Return the gas back to the origin. Used by the Virtual machine or Closures
func (c *stateObject) ReturnGas(gas *big.Int) {}

func (self *stateObject) deepCopy(db *StateDB, onDirty func(addr common.Address)) *stateObject {
	stateObject := newObject(db, self.address, self.data, onDirty)
	if self.trie != nil {
		stateObject.trie = db.db.CopyTrie(self.trie)
	}
	stateObject.code = self.code
	stateObject.dirtyStorage = self.dirtyStorage.Copy()
	stateObject.cachedStorage = self.dirtyStorage.Copy()
	stateObject.suicided = self.suicided
	stateObject.dirtyCode = self.dirtyCode
	stateObject.deleted = self.deleted
	return stateObject
}

//
// Attribute accessors
//

// Returns the address of the contract/account
func (c *stateObject) Address() common.Address {
	return c.address
}

// Code returns the contract code associated with this object, if any.
func (self *stateObject) Code(db Database) []byte {
	if self.code != nil {
		return self.code
	}
	if bytes.Equal(self.CodeHash(), emptyCodeHash) {
		return nil
	}
	code, err := db.ContractCode(self.addrHash, common.BytesToHash(self.CodeHash()))
	if err != nil {
		self.setError(fmt.Errorf("can't load code hash %x: %v", self.CodeHash(), err))
	}
	self.code = code
	return code
}

func (self *stateObject) Abi(db Database) string {
	if len(self.abi) > 0 {
		return self.abi
	}
	if bytes.Equal(self.CodeHash(), emptyCodeHash) {
		return ""
	}
	abi, err := db.ContractAbi(self.addrHash, common.BytesToHash(self.CodeHash()))
	if err != nil {
		self.setError(fmt.Errorf("can't load abi. code hash %x: %v", self.CodeHash(), err))
	}
	self.abi = abi
	return abi
}

func (self *stateObject) SetCode(codeHash common.Hash, code []byte) {
	prevcode := self.Code(self.db.db)
	self.db.journal = append(self.db.journal, codeChange{
		account:  &self.address,
		prevhash: self.CodeHash(),
		prevcode: prevcode,
	})
	self.setCode(codeHash, code)
}

func (self *stateObject) setCode(codeHash common.Hash, code []byte) {
	self.code = code
	self.data.CodeHash = codeHash[:]
	self.dirtyCode = true
	self.tryMarkDirty()
}

func (self *stateObject) SetAbi(abi string) {
	self.abi = abi
}

func (self *stateObject) SetNonce(nonce uint64) {
	self.db.journal = append(self.db.journal, nonceChange{
		account: &self.address,
		prev:    self.data.Nonce,
	})
	self.setNonce(nonce)
}

func (self *stateObject) setNonce(nonce uint64) {
	self.data.Nonce = nonce
	self.tryMarkDirty()
}

func (self *stateObject) SetVoteList(voteList []common.Address) {
	self.db.journal = append(self.db.journal, voteListChange{
		account: &self.address,
		prev:    self.data.VoteList,
	})
	self.setVoteList(voteList)
}

func (self *stateObject) setVoteList(voteList []common.Address) {
	self.data.VoteList = voteList
	self.tryMarkDirty()
}

func (self *stateObject) CodeHash() []byte {
	return self.data.CodeHash
}

func (self *stateObject) Balance() *big.Int {
	return self.data.Balance
}

func (self *stateObject) Nonce() uint64 {
	return self.data.Nonce
}

// Never called, but must be present to allow stateObject to be used
// as a vm.Account interface that also satisfies the vm.ContractRef
// interface. Interfaces are awesome.
func (self *stateObject) Value() *big.Int {
	panic("Value on stateObject should never be called")
}

// Get vote list
func (self *stateObject) LockBalance() *big.Int {
	return self.data.LockBalance
}

// Get vote list
func (self *stateObject) VoteList() []common.Address {
	return self.data.VoteList
}

func (self *stateObject) BalanceOf(asset common.Address) *big.Int {
	return self.data.AssetList.BalanceOf(asset)
}

func (self *stateObject) SubAssetBalance(asset common.Address, amount *big.Int) bool {
	var a = types.Asset{ID: asset, Balance: new(big.Int).Set(amount)}
	isOk := self.data.AssetList.SubAsset(a)
	if isOk {
		self.db.journal = append(self.db.journal, assetBalanceChange{
			account:    &self.address,
			asset:      types.Asset{ID: asset, Balance: new(big.Int).Set(amount)},
			preOpIsAdd: false,
		})
		self.tryMarkDirty()
	}
	return isOk
}

func (self *stateObject) AddAssetBalance(asset common.Address, amount *big.Int) {
	self.db.journal = append(self.db.journal, assetBalanceChange{
		account:    &self.address,
		asset:      types.Asset{ID: asset, Balance: new(big.Int).Set(amount)},
		preOpIsAdd: true,
	})
	var a = types.Asset{ID: asset, Balance: new(big.Int).Set(amount)}
	self.data.AssetList.AddAsset(a)
	self.tryMarkDirty()
}

func (self *stateObject) revertAssetBalance(asset types.Asset, preOpIsAdd bool) {
	if preOpIsAdd {
		self.data.AssetList.SubAsset(asset)
	} else {
		self.data.AssetList.AddAsset(asset)
	}
	self.tryMarkDirty()
}

func (self *stateObject) GetAssets() []types.Asset {
	return self.data.AssetList.GetAssets()
}

func (self *stateObject) SetAssetData(hash common.Hash, data []byte) error {
	if len(self.assetData) > 0 {
		return fmt.Errorf("assetData already exist, can not be updated")
	}
	self.assetData = append(self.assetData, data...)
	self.data.AssetHash = hash[:]
	self.dirtyAssetData = true
	self.tryMarkDirty()
	return nil
}

func (self *stateObject) AssetHash() []byte {
	return self.data.AssetHash
}

// AssetData returns the Asset data associated with this object, if any.
func (self *stateObject) AssetData(db Database) ([]byte, error) {
	if self.assetData != nil {
		return self.assetData, nil
	}
	if !self.IsAssetAccount() {
		return nil, nil
	}
	assetdata, err := db.AssetData(self.addrHash, common.BytesToHash(self.data.AssetHash))
	self.assetData = assetdata
	return assetdata, err
}

func (self *stateObject) IsAssetAccount() bool {
	if len(self.data.AssetHash) == 0 || bytes.Equal(self.data.AssetHash, emptyCodeHash) {
		return false
	}
	return true
}
