package state

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/trie"
)

type testAccount struct {
	address common.Address
	balance *big.Int
	nonce   uint64
	code    []byte
}

func makeTestState() (Database, *aoadb.MemDatabase, common.Hash, []*testAccount) {

	mem, _ := aoadb.NewMemDatabase()
	db := NewDatabase(mem)
	state, _ := New(common.Hash{}, db)

	accounts := []*testAccount{}
	for i := byte(0); i < 96; i++ {
		obj := state.GetOrNewStateObject(common.BytesToAddress([]byte{i}))
		acc := &testAccount{address: common.BytesToAddress([]byte{i})}

		obj.AddBalance(big.NewInt(int64(11 * i)))
		acc.balance = big.NewInt(int64(11 * i))

		obj.SetNonce(uint64(42 * i))
		acc.nonce = uint64(42 * i)

		if i%3 == 0 {
			obj.SetCode(crypto.Keccak256Hash([]byte{i, i, i, i, i}), []byte{i, i, i, i, i})
			acc.code = []byte{i, i, i, i, i}
		}
		state.updateStateObject(obj)
		accounts = append(accounts, acc)
	}
	root, _ := state.CommitTo(mem, false)

	return db, mem, root, accounts
}

func checkStateAccounts(t *testing.T, db aoadb.Database, root common.Hash, accounts []*testAccount) {

	state, err := New(root, NewDatabase(db))
	if err != nil {
		t.Fatalf("failed to create state trie at %x: %v", root, err)
	}
	if err := checkStateConsistency(db, root); err != nil {
		t.Fatalf("inconsistent state trie at %x: %v", root, err)
	}
	for i, acc := range accounts {
		if balance := state.GetBalance(acc.address); balance.Cmp(acc.balance) != 0 {
			t.Errorf("account %d: balance mismatch: have %v, want %v", i, balance, acc.balance)
		}
		if nonce := state.GetNonce(acc.address); nonce != acc.nonce {
			t.Errorf("account %d: nonce mismatch: have %v, want %v", i, nonce, acc.nonce)
		}
		if code := state.GetCode(acc.address); !bytes.Equal(code, acc.code) {
			t.Errorf("account %d: code mismatch: have %x, want %x", i, code, acc.code)
		}
	}
}

func checkTrieConsistency(db aoadb.Database, root common.Hash) error {
	if v, _ := db.Get(root[:]); v == nil {
		return nil
	}
	trie, err := trie.New(root, db)
	if err != nil {
		return err
	}
	it := trie.NodeIterator(nil)
	for it.Next(true) {
	}
	return it.Error()
}

func checkStateConsistency(db aoadb.Database, root common.Hash) error {

	if _, err := db.Get(root.Bytes()); err != nil {
		return nil
	}
	state, err := New(root, NewDatabase(db))
	if err != nil {
		return err
	}
	it := NewNodeIterator(state)
	for it.Next() {
	}
	return it.Error
}

func TestEmptyStateSync(t *testing.T) {
	empty := common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	db, _ := aoadb.NewMemDatabase()
	if req := NewStateSync(empty, db).Missing(1); len(req) != 0 {
		t.Errorf("content requested for empty state: %v", req)
	}
}

func TestIterativeStateSyncIndividual(t *testing.T) { testIterativeStateSync(t, 1) }
func TestIterativeStateSyncBatched(t *testing.T)    { testIterativeStateSync(t, 100) }

func testIterativeStateSync(t *testing.T, batch int) {

	_, srcMem, srcRoot, srcAccounts := makeTestState()

	dstDb, _ := aoadb.NewMemDatabase()
	sched := NewStateSync(srcRoot, dstDb)

	queue := append([]common.Hash{}, sched.Missing(batch)...)
	for len(queue) > 0 {
		results := make([]trie.SyncResult, len(queue))
		for i, hash := range queue {
			data, err := srcMem.Get(hash.Bytes())
			if err != nil {
				t.Fatalf("failed to retrieve node data for %x: %v", hash, err)
			}
			results[i] = trie.SyncResult{Hash: hash, Data: data}
		}
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(dstDb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		queue = append(queue[:0], sched.Missing(batch)...)
	}

	checkStateAccounts(t, dstDb, srcRoot, srcAccounts)
}

func TestIterativeDelayedStateSync(t *testing.T) {

	_, srcMem, srcRoot, srcAccounts := makeTestState()

	dstDb, _ := aoadb.NewMemDatabase()
	sched := NewStateSync(srcRoot, dstDb)

	queue := append([]common.Hash{}, sched.Missing(0)...)
	for len(queue) > 0 {

		results := make([]trie.SyncResult, len(queue)/2+1)
		for i, hash := range queue[:len(results)] {
			data, err := srcMem.Get(hash.Bytes())
			if err != nil {
				t.Fatalf("failed to retrieve node data for %x: %v", hash, err)
			}
			results[i] = trie.SyncResult{Hash: hash, Data: data}
		}
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(dstDb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		queue = append(queue[len(results):], sched.Missing(0)...)
	}

	checkStateAccounts(t, dstDb, srcRoot, srcAccounts)
}

func TestIterativeRandomStateSyncIndividual(t *testing.T) { testIterativeRandomStateSync(t, 1) }
func TestIterativeRandomStateSyncBatched(t *testing.T)    { testIterativeRandomStateSync(t, 100) }

func testIterativeRandomStateSync(t *testing.T, batch int) {

	_, srcMem, srcRoot, srcAccounts := makeTestState()

	dstDb, _ := aoadb.NewMemDatabase()
	sched := NewStateSync(srcRoot, dstDb)

	queue := make(map[common.Hash]struct{})
	for _, hash := range sched.Missing(batch) {
		queue[hash] = struct{}{}
	}
	for len(queue) > 0 {

		results := make([]trie.SyncResult, 0, len(queue))
		for hash := range queue {
			data, err := srcMem.Get(hash.Bytes())
			if err != nil {
				t.Fatalf("failed to retrieve node data for %x: %v", hash, err)
			}
			results = append(results, trie.SyncResult{Hash: hash, Data: data})
		}

		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(dstDb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		queue = make(map[common.Hash]struct{})
		for _, hash := range sched.Missing(batch) {
			queue[hash] = struct{}{}
		}
	}

	checkStateAccounts(t, dstDb, srcRoot, srcAccounts)
}

func TestIterativeRandomDelayedStateSync(t *testing.T) {

	_, srcMem, srcRoot, srcAccounts := makeTestState()

	dstDb, _ := aoadb.NewMemDatabase()
	sched := NewStateSync(srcRoot, dstDb)

	queue := make(map[common.Hash]struct{})
	for _, hash := range sched.Missing(0) {
		queue[hash] = struct{}{}
	}
	for len(queue) > 0 {

		results := make([]trie.SyncResult, 0, len(queue)/2+1)
		for hash := range queue {
			delete(queue, hash)

			data, err := srcMem.Get(hash.Bytes())
			if err != nil {
				t.Fatalf("failed to retrieve node data for %x: %v", hash, err)
			}
			results = append(results, trie.SyncResult{Hash: hash, Data: data})

			if len(results) >= cap(results) {
				break
			}
		}

		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(dstDb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		for _, hash := range sched.Missing(0) {
			queue[hash] = struct{}{}
		}
	}

	checkStateAccounts(t, dstDb, srcRoot, srcAccounts)
}

func TestIncompleteStateSync(t *testing.T) {

	_, srcMem, srcRoot, srcAccounts := makeTestState()

	checkTrieConsistency(srcMem, srcRoot)

	dstDb, _ := aoadb.NewMemDatabase()
	sched := NewStateSync(srcRoot, dstDb)

	added := []common.Hash{}
	queue := append([]common.Hash{}, sched.Missing(1)...)
	for len(queue) > 0 {

		results := make([]trie.SyncResult, len(queue))
		for i, hash := range queue {
			data, err := srcMem.Get(hash.Bytes())
			if err != nil {
				t.Fatalf("failed to retrieve node data for %x: %v", hash, err)
			}
			results[i] = trie.SyncResult{Hash: hash, Data: data}
		}

		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(dstDb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		for _, result := range results {
			added = append(added, result.Hash)
		}

	checkSubtries:
		for _, hash := range added {
			for _, acc := range srcAccounts {
				if hash == crypto.Keccak256Hash(acc.code) {
					continue checkSubtries
				}
			}

			if err := checkTrieConsistency(dstDb, hash); err != nil {
				t.Fatalf("state inconsistent: %v", err)
			}
		}

		queue = append(queue[:0], sched.Missing(1)...)
	}

	for _, node := range added[1:] {
		key := node.Bytes()
		value, _ := dstDb.Get(key)

		dstDb.Delete(key)
		if err := checkStateConsistency(dstDb, added[0]); err == nil {
			t.Fatalf("trie inconsistency not caught, missing: %x", key)
		}
		dstDb.Put(key, value)
	}
}
