package trie

import (
	"errors"
	"fmt"

	"github.com/Aurorachain/go-Aurora/common"
	"gopkg.in/karalabe/cookiejar.v2/collections/prque"
)

var ErrNotRequested = errors.New("not requested")

var ErrAlreadyProcessed = errors.New("already processed")

type request struct {
	hash common.Hash // Hash of the node data content to retrieve
	data []byte      // Data content of the node, cached until all subtrees complete
	raw  bool        // Whether this is a raw entry (code) or a trie node

	parents []*request // Parent state nodes referencing this entry (notify all upon completion)
	depth   int        // Depth level within the trie the node is located to prioritise DFS
	deps    int        // Number of dependencies before allowed to commit this node

	callback TrieSyncLeafCallback // Callback to invoke if a leaf node it reached on this branch
}

type SyncResult struct {
	Hash common.Hash // Hash of the originally unknown trie node
	Data []byte      // Data content of the retrieved node
}

type syncMemBatch struct {
	batch map[common.Hash][]byte // In-memory membatch of recently completed items
	order []common.Hash          // Order of completion to prevent out-of-order data loss
}

func newSyncMemBatch() *syncMemBatch {
	return &syncMemBatch{
		batch: make(map[common.Hash][]byte),
		order: make([]common.Hash, 0, 256),
	}
}

type TrieSyncLeafCallback func(leaf []byte, parent common.Hash) error

type TrieSync struct {
	database DatabaseReader           // Persistent database to check for existing entries
	membatch *syncMemBatch            // Memory buffer to avoid frequest database writes
	requests map[common.Hash]*request // Pending requests pertaining to a key hash
	queue    *prque.Prque             // Priority queue with the pending requests
}

func NewTrieSync(root common.Hash, database DatabaseReader, callback TrieSyncLeafCallback) *TrieSync {
	ts := &TrieSync{
		database: database,
		membatch: newSyncMemBatch(),
		requests: make(map[common.Hash]*request),
		queue:    prque.New(),
	}
	ts.AddSubTrie(root, 0, common.Hash{}, callback)
	return ts
}

func (s *TrieSync) AddSubTrie(root common.Hash, depth int, parent common.Hash, callback TrieSyncLeafCallback) {
	if root == emptyRoot {
		return
	}
	if _, ok := s.membatch.batch[root]; ok {
		return
	}
	key := root.Bytes()
	blob, _ := s.database.Get(key)
	if local, err := decodeNode(key, blob, 0); local != nil && err == nil {
		return
	}
	req := &request{
		hash:     root,
		depth:    depth,
		callback: callback,
	}
	if parent != (common.Hash{}) {
		ancestor := s.requests[parent]
		if ancestor == nil {
			panic(fmt.Sprintf("sub-trie ancestor not found: %x", parent))
		}
		ancestor.deps++
		req.parents = append(req.parents, ancestor)
	}
	s.schedule(req)
}

func (s *TrieSync) AddRawEntry(hash common.Hash, depth int, parent common.Hash) {
	if hash == emptyState {
		return
	}
	if _, ok := s.membatch.batch[hash]; ok {
		return
	}
	if ok, _ := s.database.Has(hash.Bytes()); ok {
		return
	}
	req := &request{
		hash:  hash,
		raw:   true,
		depth: depth,
	}
	if parent != (common.Hash{}) {
		ancestor := s.requests[parent]
		if ancestor == nil {
			panic(fmt.Sprintf("raw-entry ancestor not found: %x", parent))
		}
		ancestor.deps++
		req.parents = append(req.parents, ancestor)
	}
	s.schedule(req)
}

func (s *TrieSync) Missing(max int) []common.Hash {
	requests := []common.Hash{}
	for !s.queue.Empty() && (max == 0 || len(requests) < max) {
		requests = append(requests, s.queue.PopItem().(common.Hash))
	}
	return requests
}

func (s *TrieSync) Process(results []SyncResult) (bool, int, error) {
	committed := false

	for i, item := range results {
		request := s.requests[item.Hash]
		if request == nil {
			return committed, i, ErrNotRequested
		}
		if request.data != nil {
			return committed, i, ErrAlreadyProcessed
		}
		if request.raw {
			request.data = item.Data
			s.commit(request)
			committed = true
			continue
		}
		node, err := decodeNode(item.Hash[:], item.Data, 0)
		if err != nil {
			return committed, i, err
		}
		request.data = item.Data

		requests, err := s.children(request, node)
		if err != nil {
			return committed, i, err
		}
		if len(requests) == 0 && request.deps == 0 {
			s.commit(request)
			committed = true
			continue
		}
		request.deps += len(requests)
		for _, child := range requests {
			s.schedule(child)
		}
	}
	return committed, 0, nil
}

func (s *TrieSync) Commit(dbw DatabaseWriter) (int, error) {
	for i, key := range s.membatch.order {
		if err := dbw.Put(key[:], s.membatch.batch[key]); err != nil {
			return i, err
		}
	}
	written := len(s.membatch.order)

	s.membatch = newSyncMemBatch()
	return written, nil
}

func (s *TrieSync) Pending() int {
	return len(s.requests)
}

func (s *TrieSync) schedule(req *request) {
	if old, ok := s.requests[req.hash]; ok {
		old.parents = append(old.parents, req.parents...)
		return
	}
	s.queue.Push(req.hash, float32(req.depth))
	s.requests[req.hash] = req
}

func (s *TrieSync) children(req *request, object node) ([]*request, error) {
	type child struct {
		node  node
		depth int
	}
	children := []child{}

	switch node := (object).(type) {
	case *shortNode:
		children = []child{{
			node:  node.Val,
			depth: req.depth + len(node.Key),
		}}
	case *fullNode:
		for i := 0; i < 17; i++ {
			if node.Children[i] != nil {
				children = append(children, child{
					node:  node.Children[i],
					depth: req.depth + 1,
				})
			}
		}
	default:
		panic(fmt.Sprintf("unknown node: %+v", node))
	}
	requests := make([]*request, 0, len(children))
	for _, child := range children {
		if req.callback != nil {
			if node, ok := (child.node).(valueNode); ok {
				if err := req.callback(node, req.hash); err != nil {
					return nil, err
				}
			}
		}
		if node, ok := (child.node).(hashNode); ok {
			hash := common.BytesToHash(node)
			if _, ok := s.membatch.batch[hash]; ok {
				continue
			}
			if ok, _ := s.database.Has(node); ok {
				continue
			}
			requests = append(requests, &request{
				hash:     hash,
				parents:  []*request{req},
				depth:    child.depth,
				callback: req.callback,
			})
		}
	}
	return requests, nil
}

func (s *TrieSync) commit(req *request) (err error) {
	s.membatch.batch[req.hash] = req.data
	s.membatch.order = append(s.membatch.order, req.hash)

	delete(s.requests, req.hash)

	for _, parent := range req.parents {
		parent.deps--
		if parent.deps == 0 {
			if err := s.commit(parent); err != nil {
				return err
			}
		}
	}
	return nil
}
