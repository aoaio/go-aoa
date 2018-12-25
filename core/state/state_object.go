package state

import (
	"bytes"
	"fmt"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/rlp"
	"github.com/Aurorachain/go-Aurora/trie"
	"io"
	"math/big"
)

var emptyCodeHash = crypto.Keccak256(nil)

type Code []byte

func (self Code) String() string {
	return string(self) 
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

type stateObject struct {
	address  common.Address
	addrHash common.Hash 
	data     Account
	db       *StateDB

	dbErr error

	trie      Trie   
	code      Code   
	abi       string 
	assetData []byte 

	cachedStorage Storage 
	dirtyStorage  Storage 

	dirtyCode      bool 
	suicided       bool
	touched        bool
	deleted        bool
	dirtyAssetData bool
	onDirty        func(addr common.Address) 
}

func (s *stateObject) empty() bool {
	return s.data.Nonce == 0 && s.data.Balance.Sign() == 0 && bytes.Equal(s.data.CodeHash, emptyCodeHash) && s.data.AssetList.IsEmpty()
}

type Account struct {
	Nonce       uint64
	Balance     *big.Int
	Root        common.Hash 
	CodeHash    []byte
	LockBalance *big.Int         
	VoteList    []common.Address 
	AssetList   *types.Assets    
	AssetHash   []byte
}

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

func (c *stateObject) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, c.data)
}

func (self *stateObject) setError(err error) {
	if self.dbErr == nil {
		self.dbErr = err
	}
}

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

func (self *stateObject) GetState(db Database, key common.Hash) common.Hash {
	value, exists := self.cachedStorage[key]
	if exists {
		return value
	}

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

func (self *stateObject) updateTrie(db Database) Trie {
	tr := self.getTrie(db)
	for key, value := range self.dirtyStorage {
		delete(self.dirtyStorage, key)
		if (value == common.Hash{}) {
			self.setError(tr.TryDelete(key[:]))
			continue
		}

		v, _ := rlp.EncodeToBytes(bytes.TrimLeft(value[:], "\x00"))
		self.setError(tr.TryUpdate(key[:], v))
	}
	return tr
}

func (self *stateObject) updateRoot(db Database) {
	self.updateTrie(db)
	self.data.Root = self.trie.Hash()
}

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

func (c *stateObject) AddBalance(amount *big.Int) {

	if amount.Sign() == 0 {
		if c.empty() {
			c.touch()
		}

		return
	}
	c.SetBalance(new(big.Int).Add(c.Balance(), amount))
}

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

func (c *stateObject) Address() common.Address {
	return c.address
}

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

func (self *stateObject) Value() *big.Int {
	panic("Value on stateObject should never be called")
}

func (self *stateObject) LockBalance() *big.Int {
	return self.data.LockBalance
}

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
