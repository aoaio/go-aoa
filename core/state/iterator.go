package state

import (
	"bytes"
	"fmt"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/rlp"
	"github.com/Aurorachain/go-Aurora/trie"
)

type NodeIterator struct {
	state *StateDB

	stateIt trie.NodeIterator
	dataIt  trie.NodeIterator

	accountHash common.Hash
	codeHash    common.Hash
	code        []byte

	Hash   common.Hash
	Parent common.Hash

	Error error
}

func NewNodeIterator(state *StateDB) *NodeIterator {
	return &NodeIterator{
		state: state,
	}
}

func (it *NodeIterator) Next() bool {

	if it.Error != nil {
		return false
	}

	if err := it.step(); err != nil {
		it.Error = err
		return false
	}
	return it.retrieve()
}

func (it *NodeIterator) step() error {

	if it.state == nil {
		return nil
	}

	if it.stateIt == nil {
		it.stateIt = it.state.trie.NodeIterator(nil)
	}

	if it.dataIt != nil {
		if cont := it.dataIt.Next(true); !cont {
			if it.dataIt.Error() != nil {
				return it.dataIt.Error()
			}
			it.dataIt = nil
		}
		return nil
	}

	if it.code != nil {
		it.code = nil
		return nil
	}

	if cont := it.stateIt.Next(true); !cont {
		if it.stateIt.Error() != nil {
			return it.stateIt.Error()
		}
		it.state, it.stateIt = nil, nil
		return nil
	}

	if !it.stateIt.Leaf() {
		return nil
	}

	var account Account
	if err := rlp.Decode(bytes.NewReader(it.stateIt.LeafBlob()), &account); err != nil {
		return err
	}
	dataTrie, err := it.state.db.OpenStorageTrie(common.BytesToHash(it.stateIt.LeafKey()), account.Root)
	if err != nil {
		return err
	}
	it.dataIt = dataTrie.NodeIterator(nil)
	if !it.dataIt.Next(true) {
		it.dataIt = nil
	}
	if !bytes.Equal(account.CodeHash, emptyCodeHash) {
		it.codeHash = common.BytesToHash(account.CodeHash)
		addrHash := common.BytesToHash(it.stateIt.LeafKey())
		it.code, err = it.state.db.ContractCode(addrHash, common.BytesToHash(account.CodeHash))
		if err != nil {
			return fmt.Errorf("code %x: %v", account.CodeHash, err)
		}
	}
	it.accountHash = it.stateIt.Parent()
	return nil
}

func (it *NodeIterator) retrieve() bool {

	it.Hash = common.Hash{}

	if it.state == nil {
		return false
	}

	switch {
	case it.dataIt != nil:
		it.Hash, it.Parent = it.dataIt.Hash(), it.dataIt.Parent()
		if it.Parent == (common.Hash{}) {
			it.Parent = it.accountHash
		}
	case it.code != nil:
		it.Hash, it.Parent = it.codeHash, it.accountHash
	case it.stateIt != nil:
		it.Hash, it.Parent = it.stateIt.Hash(), it.stateIt.Parent()
	}
	return true
}
