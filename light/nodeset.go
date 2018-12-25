package light

import (
	"errors"
	"sync"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/rlp"
	"github.com/Aurorachain/go-Aurora/trie"
)

type NodeSet struct {
	db       map[string][]byte
	dataSize int
	lock     sync.RWMutex
}

func NewNodeSet() *NodeSet {
	return &NodeSet{
		db: make(map[string][]byte),
	}
}

func (db *NodeSet) Put(key []byte, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	if _, ok := db.db[string(key)]; !ok {
		db.db[string(key)] = common.CopyBytes(value)
		db.dataSize += len(value)
	}
	return nil
}

func (db *NodeSet) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if entry, ok := db.db[string(key)]; ok {
		return entry, nil
	}
	return nil, errors.New("not found")
}

func (db *NodeSet) Has(key []byte) (bool, error) {
	_, err := db.Get(key)
	return err == nil, nil
}

func (db *NodeSet) KeyCount() int {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return len(db.db)
}

func (db *NodeSet) DataSize() int {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return db.dataSize
}

func (db *NodeSet) NodeList() NodeList {
	db.lock.RLock()
	defer db.lock.RUnlock()

	var values NodeList
	for _, value := range db.db {
		values = append(values, value)
	}
	return values
}

func (db *NodeSet) Store(target trie.Database) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	for key, value := range db.db {
		target.Put([]byte(key), value)
	}
}

type NodeList []rlp.RawValue

func (n NodeList) Store(db trie.Database) {
	for _, node := range n {
		db.Put(crypto.Keccak256(node), node)
	}
}

func (n NodeList) NodeSet() *NodeSet {
	db := NewNodeSet()
	n.Store(db)
	return db
}

func (n *NodeList) Put(key []byte, value []byte) error {
	*n = append(*n, value)
	return nil
}

func (n NodeList) DataSize() int {
	var size int
	for _, node := range n {
		size += len(node)
	}
	return size
}
