package discover

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/log"
)

const (
	CommNet = byte(1)
	ConsNet = byte(2)
)

const (
	alpha      = 3
	bucketSize = 16
	hashBits   = len(common.Hash{}) * 8
	nBuckets   = hashBits + 1

	maxBondingPingPongs = 16

	maxFindnodeFailures = 5

	autoRefreshInterval = 15 * time.Minute

	seedCount = 30

	seedMaxAge = 5 * 24 * time.Hour
)

type Table struct {
	mutex     sync.Mutex
	consmutex sync.Mutex

	buckets [nBuckets]*bucket

	consBuckets [nBuckets]*bucket

	nursery []*Node

	db *nodeDB

	openTop    bool
	refreshReq chan chan struct{}
	closeReq   chan struct{}
	closed     chan struct{}

	bondmu sync.Mutex

	bonding map[NodeID]*bondproc

	bondslots chan struct{}

	nodeAddedHook func(*Node)

	net transport

	self *Node
}

type bondproc struct {
	err  error
	n    *Node
	done chan struct{}
}

type transport interface {
	ping(NodeID, *net.UDPAddr, byte) error
	waitping(NodeID) error
	findnode(toid NodeID, addr *net.UDPAddr, target NodeID, netType byte) ([]*Node, error)
	openConsNet()
	close()
}

type bucket struct{ entries []*Node }

func newTable(t transport, ourID NodeID, ourAddr *net.UDPAddr, nodeDBPath string, openTop bool) (*Table, error) {

	db, err := newNodeDB(nodeDBPath, Version, ourID)
	if err != nil {
		return nil, err
	}
	tab := &Table{
		net:        t,
		db:         db,
		self:       NewNode(ourID, ourAddr.IP, uint16(ourAddr.Port), uint16(ourAddr.Port)),
		bonding:    make(map[NodeID]*bondproc),
		bondslots:  make(chan struct{}, maxBondingPingPongs),
		refreshReq: make(chan chan struct{}),
		closeReq:   make(chan struct{}),
		closed:     make(chan struct{}),
		openTop:    openTop,
	}
	for i := 0; i < cap(tab.bondslots); i++ {
		tab.bondslots <- struct{}{}
	}

	for i := range tab.buckets {
		tab.buckets[i] = new(bucket)
	}

	for i := range tab.consBuckets {
		tab.consBuckets[i] = new(bucket)
	}

	go tab.refreshLoop()
	return tab, nil
}

func (tab *Table) Self() *Node {
	return tab.self
}

func (tab *Table)  Delete(target NodeID){
	node :=tab.Resolve(target)
	if node !=nil{
		tab.delete(node,ConsNet)
		tab.delete(node,CommNet)

	}

}

func (tab *Table) OpenTopNet() {
	tab.openTop = true
	tab.net.openConsNet()
	go tab.refreshLoop()
}

func (tab *Table) ReadRandomNodes(buf []*Node, netType byte) (n int) {
	tab.mutex.Lock()
	defer tab.mutex.Unlock()

	var buckets [][]*Node
	if netType == ConsNet {
		for _, b := range tab.consBuckets {
			if len(b.entries) > 0 {
				buckets = append(buckets, b.entries[:])
			}
		}
	} else {
		for _, b := range tab.buckets {
			if len(b.entries) > 0 {
				buckets = append(buckets, b.entries[:])
			}
		}
	}

	if len(buckets) == 0 {
		return 0
	}

	for i := uint32(len(buckets)) - 1; i > 0; i-- {
		j := randUint(i)
		buckets[i], buckets[j] = buckets[j], buckets[i]
	}

	var i, j int
	for ; i < len(buf); i, j = i+1, (j+1)%len(buckets) {
		b := buckets[j]
		buf[i] = &(*b[0])
		buckets[j] = b[1:]
		if len(b) == 1 {
			buckets = append(buckets[:j], buckets[j+1:]...)
		}
		if len(buckets) == 0 {
			break
		}
	}
	return i + 1
}

func randUint(max uint32) uint32 {
	if max == 0 {
		return 0
	}
	var b [4]byte
	rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:]) % max
}

func (table *Table) refreshNode(nodeId NodeID) {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	node := table.Resolve(nodeId)
	if nil != node {
		table.delete(node, CommNet)
		table.delete(node, ConsNet)
	}
}

func (tab *Table) Close() {
	select {
	case <-tab.closed:

	case tab.closeReq <- struct{}{}:
		<-tab.closed
	}
}

func (tab *Table) SetFallbackNodes(nodes []*Node) error {
	for _, n := range nodes {
		if err := n.validateComplete(); err != nil {
			return fmt.Errorf("bad bootstrap/fallback node %q (%v)", n, err)
		}
	}
	tab.mutex.Lock()
	tab.nursery = make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		cpy := *n

		cpy.sha = crypto.Keccak256Hash(n.ID[:])
		tab.nursery = append(tab.nursery, &cpy)
	}
	tab.mutex.Unlock()
	tab.refresh()
	return nil
}

func (tab *Table) Resolve(targetID NodeID) *Node {

	hash := crypto.Keccak256Hash(targetID[:])
	tab.mutex.Lock()

	cl := tab.closest(hash, 1, CommNet)
	tab.mutex.Unlock()
	if len(cl.entries) > 0 && cl.entries[0].ID == targetID {
		return cl.entries[0]
	}

	result := tab.Lookup(targetID, CommNet)
	for _, n := range result {
		if n.ID == targetID {
			return n
		}
	}
	return nil
}

func (tab *Table) Lookup(targetID NodeID, netType byte) []*Node {
	return tab.lookup(targetID, true, netType)
}

func (tab *Table) lookup(targetID NodeID, refreshIfEmpty bool, netType byte) []*Node {
	var (
		target         = crypto.Keccak256Hash(targetID[:])
		asked          = make(map[NodeID]bool)
		seen           = make(map[NodeID]bool)
		reply          = make(chan []*Node, alpha)
		pendingQueries = 0
		result         *nodesByDistance
	)

	asked[tab.self.ID] = true

	for {
		tab.mutex.Lock()

		result = tab.closest(target, bucketSize, netType)
		tab.mutex.Unlock()
		if len(result.entries) > 0 || !refreshIfEmpty {
			break
		}

		<-tab.refresh()
		refreshIfEmpty = false
	}

	for {

		for i := 0; i < len(result.entries) && pendingQueries < alpha; i++ {
			n := result.entries[i]
			if !asked[n.ID] {
				asked[n.ID] = true
				pendingQueries++
				go func() {

					r, err := tab.net.findnode(n.ID, n.addr(), targetID, netType)
					if err != nil {

						fails := tab.db.findFails(n.ID) + 1
						tab.db.updateFindFails(n.ID, fails)
						log.Trace("Bumping findnode failure counter", "id", n.ID, "failcount", fails)

						if fails >= maxFindnodeFailures {
							log.Info("Too many findnode failures, dropping", "id", n.ID, "failcount", fails)
							tab.delete(n, netType)
						}
					}
					reply <- tab.bondall(r, netType)
				}()
			}
		}
		if pendingQueries == 0 {

			break
		}

		for _, n := range <-reply {
			if n != nil && !seen[n.ID] {
				seen[n.ID] = true
				result.push(n, bucketSize)
			}
		}
		pendingQueries--
	}
	return result.entries
}

func (tab *Table) refresh() <-chan struct{} {
	done := make(chan struct{})
	select {
	case tab.refreshReq <- done:
	case <-tab.closed:
		close(done)
	}
	return done
}

func (tab *Table) refreshLoop() {
	var (
		timer   = time.NewTicker(autoRefreshInterval)
		waiting []chan struct{}
		done    chan struct{}
	)
loop:
	for {
		select {
		case <-timer.C:
			if done == nil {
				done = make(chan struct{})
				go tab.doRefresh(done)
			}
		case req := <-tab.refreshReq:
			waiting = append(waiting, req)
			if done == nil {
				done = make(chan struct{})
				go tab.doRefresh(done)
			}
		case <-done:
			for _, ch := range waiting {
				close(ch)
			}
			waiting = nil
			done = nil
		case <-tab.closeReq:
			break loop
		}
	}

	if tab.net != nil {
		tab.net.close()
	}
	if done != nil {
		<-done
	}
	for _, ch := range waiting {
		close(ch)
	}
	tab.db.close()
	close(tab.closed)
}

func (tab *Table) doRefresh(done chan struct{}) {

	var wg sync.WaitGroup
	defer close(done)

	wg.Add(1)
	seeds := tab.db.querySeeds(seedCount, seedMaxAge)
	go func() {

		seeds = tab.bondall(append(seeds, tab.nursery...), CommNet)

		if len(seeds) == 0 {

		}
		for _, n := range seeds {
			age := log.Lazy{Fn: func() time.Duration { return time.Since(tab.db.lastPong(n.ID)) }}
			log.Trace("get seed peers", "id", n.ID, "addr", n.addr(), "age", age)
		}
		tab.mutex.Lock()
		tab.stuff(seeds, CommNet)
		tab.mutex.Unlock()
		tab.doRefreshNetType(CommNet)
		wg.Done()

	}()

	if tab.openTop {
		log.Debug("start kad net")
		wg.Add(1)
		go func() {
			seeds = tab.bondall(append(seeds, tab.nursery...), ConsNet)
			if len(seeds) == 0 {
				log.Error("No discv4 seed nodes found")
			}
			for _, n := range seeds {
				age := log.Lazy{Fn: func() time.Duration { return time.Since(tab.db.lastPong(n.ID)) }}
				log.Error("Found seed node in database", "id", n.ID, "addr", n.addr(), "age", age)
			}
			tab.consmutex.Lock()
			tab.stuff(seeds, ConsNet)
			tab.consmutex.Unlock()
			tab.doRefreshNetType(ConsNet)
			wg.Done()
		}()
	}

	wg.Wait()

}

func (tab *Table) openConsNet() {
	tab.openTop = true
	go tab.doRefreshConsNetRightNow()
}

func (tab *Table) doRefreshConsNetRightNow() {
	seeds := tab.db.querySeeds(seedCount, seedMaxAge)
	seeds = tab.bondall(append(seeds, tab.nursery...), ConsNet)
	if len(seeds) == 0 {
		log.Error("No discv4 seed nodes found")
	}
	for _, n := range seeds {
		age := log.Lazy{Fn: func() time.Duration { return time.Since(tab.db.lastPong(n.ID)) }}
		log.Trace("Found seed node in database", "id", n.ID, "addr", n.addr(), "age", age)
	}
	tab.consmutex.Lock()
	tab.stuff(seeds, ConsNet)
	tab.consmutex.Unlock()
	tab.doRefreshNetType(ConsNet)
}

func (tab *Table) doRefreshNetType(netType byte) {
	var target NodeID
	rand.Read(target[:])
	result := tab.lookup(target, false, netType)
	if len(result) > 0 {
		return
	}

	tab.lookup(tab.self.ID, false, netType)

}

func (tab *Table) closest(target common.Hash, nresults int, netType byte) *nodesByDistance {

	var close *nodesByDistance
	close = &nodesByDistance{target: target}
	if CommNet == netType {
		for _, b := range tab.buckets {
			for _, n := range b.entries {
				close.push(n, nresults)
			}
		}
	} else {
		for _, b := range tab.consBuckets {
			for _, n := range b.entries {
				close.push(n, nresults)
			}
		}
	}

	return close
}

func (tab *Table) len() (n int) {
	for _, b := range tab.buckets {
		n += len(b.entries)
	}
	return n
}

func (tab *Table) bondall(nodes []*Node, netType byte) (result []*Node) {
	rc := make(chan *Node, len(nodes))
	for i := range nodes {
		go func(n *Node) {
			nn, _ := tab.bond(false, n.ID, n.addr(), n.TCP, netType)
			rc <- nn
		}(nodes[i])
	}

	for range nodes {
		if n := <-rc; n != nil {
			result = append(result, n)
		}
	}
	return result
}

func (tab *Table) bond(pinged bool, id NodeID, addr *net.UDPAddr, tcpPort uint16, netType byte) (*Node, error) {
	if id == tab.self.ID {
		return nil, errors.New("is self")
	}

	node, fails := tab.db.node(id), 0
	if node != nil {
		fails = tab.db.findFails(id)
	}

	var result error
	age := time.Since(tab.db.lastPong(id))
	if node == nil || fails > 0 || age > nodeDBNodeExpiration {
		log.Trace("Starting bonding ping/pong", "id", id, "known", node != nil, "failcount", fails, "age", age)

		tab.bondmu.Lock()
		w := tab.bonding[id]
		if w != nil {

			tab.bondmu.Unlock()
			<-w.done
		} else {

			w = &bondproc{done: make(chan struct{})}
			tab.bonding[id] = w
			tab.bondmu.Unlock()

			tab.pingpong(w, pinged, id, addr, tcpPort, netType)

			tab.bondmu.Lock()
			delete(tab.bonding, id)
			tab.bondmu.Unlock()
		}

		result = w.err
		if result == nil {
			node = w.n
		}
	}
	if node != nil {

		tab.add(node, netType)
		tab.db.updateFindFails(id, 0)
	}
	return node, result
}

func (tab *Table) pingpong(w *bondproc, pinged bool, id NodeID, addr *net.UDPAddr, tcpPort uint16, netType byte) {

	<-tab.bondslots
	defer func() { tab.bondslots <- struct{}{} }()

	if w.err = tab.ping(id, addr, netType); w.err != nil {
		close(w.done)
		return
	}
	if !pinged {

		tab.net.waitping(id)
	}

	w.n = NewNode(id, addr.IP, uint16(addr.Port), tcpPort)
	tab.db.updateNode(w.n)
	close(w.done)
}

func (tab *Table) ping(id NodeID, addr *net.UDPAddr, netType byte) error {
	tab.db.updateLastPing(id, time.Now())
	if err := tab.net.ping(id, addr, netType); err != nil {
		return err
	}
	tab.db.updateLastPong(id, time.Now())

	tab.db.ensureExpirer()
	return nil
}

func (tab *Table) add(new *Node, netType byte) {
	if netType == CommNet {

	}
	var b *bucket
	if netType == CommNet {
		b = tab.buckets[logdist(tab.self.sha, new.sha)]
	} else {
		b = tab.consBuckets[logdist(tab.self.sha, new.sha)]
	}

	tab.mutex.Lock()
	defer tab.mutex.Unlock()
	if b.bump(new) {
		return
	}
	var oldest *Node
	if len(b.entries) == bucketSize {
		oldest = b.entries[bucketSize-1]
		if oldest.contested {

			return
		}
		oldest.contested = true

		tab.mutex.Unlock()
		err := tab.ping(oldest.ID, oldest.addr(), netType)
		tab.mutex.Lock()
		oldest.contested = false
		if err == nil {

			return
		}
	}
	added := b.replace(new, oldest)
	if added && tab.nodeAddedHook != nil {
		tab.nodeAddedHook(new)
	}
}

func (tab *Table) stuff(nodes []*Node, netType byte) {
	var bucket *bucket

outer:
	for _, n := range nodes {
		if n.ID == tab.self.ID {
			continue
		}
		if netType == CommNet {
			bucket = tab.buckets[logdist(tab.self.sha, n.sha)]
		} else {
			bucket = tab.consBuckets[logdist(tab.self.sha, n.sha)]
		}

		for i := range bucket.entries {
			if bucket.entries[i].ID == n.ID {
				continue outer
			}
		}
		if len(bucket.entries) < bucketSize {
			bucket.entries = append(bucket.entries, n)
			if tab.nodeAddedHook != nil {
				tab.nodeAddedHook(n)
			}
		}
	}

}

func (tab *Table) delete(node *Node, netType byte) {
	tab.mutex.Lock()
	defer tab.mutex.Unlock()
	var bucket *bucket
	if netType == CommNet {
		bucket = tab.buckets[logdist(tab.self.sha, node.sha)]

	} else {
		bucket = tab.consBuckets[logdist(tab.self.sha, node.sha)]
	}

	for i := range bucket.entries {
		if bucket.entries[i].ID == node.ID {
			bucket.entries = append(bucket.entries[:i], bucket.entries[i+1:]...)
			return
		}
	}
}

func (b *bucket) replace(n *Node, last *Node) bool {

	for i := range b.entries {
		if b.entries[i].ID == n.ID {
			return false
		}
	}

	if len(b.entries) == bucketSize && (last == nil || b.entries[bucketSize-1].ID != last.ID) {
		return false
	}
	if len(b.entries) < bucketSize {
		b.entries = append(b.entries, nil)
	}
	copy(b.entries[1:], b.entries)
	b.entries[0] = n
	return true
}

func (b *bucket) bump(n *Node) bool {
	for i := range b.entries {
		if b.entries[i].ID == n.ID {

			copy(b.entries[1:], b.entries[:i])
			b.entries[0] = n
			return true
		}
	}
	return false
}

type nodesByDistance struct {
	entries []*Node
	target  common.Hash
}

func (h *nodesByDistance) push(n *Node, maxElems int) {
	ix := sort.Search(len(h.entries), func(i int) bool {
		return distcmp(h.target, h.entries[i].sha, n.sha) > 0
	})
	if len(h.entries) < maxElems {
		h.entries = append(h.entries, n)
	}
	if ix == len(h.entries) {

	} else {

		copy(h.entries[ix+1:], h.entries[ix:])
		h.entries[ix] = n
	}
}
