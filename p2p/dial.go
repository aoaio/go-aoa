package p2p

import (
	"container/heap"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/p2p/discover"
	"github.com/Aurorachain/go-Aurora/p2p/netutil"
)

const (

	dialHistoryExpiration = 30 * time.Second

	lookupInterval = 4 * time.Second

	fallbackInterval = 20 * time.Second

	initialResolveDelay = 60 * time.Second
	maxResolveDelay     = time.Hour
)

type NodeDialer interface {
	Dial(*discover.Node) (net.Conn, error)
}

type TCPDialer struct {
	*net.Dialer
}

func (t TCPDialer) Dial(dest *discover.Node) (net.Conn, error) {
	addr := &net.TCPAddr{IP: dest.IP, Port: int(dest.TCP)}
	return t.Dialer.Dial("tcp", addr.String())
}

type dialstate struct {
	maxDynDials int

	ntab discoverTable

	netrestrict *netutil.Netlist

	commonLookupRunning bool

	topLookupRunning bool

	commonDialing map[discover.NodeID]connFlag

	commonLookupBuf []*discover.Node

	topLookupBuf []*discover.Node

	commonRandomNodes []*discover.Node

	consRandomNodes []*discover.Node

	static map[discover.NodeID]*dialTask

	commonHist *dialHistory
	topHist    *dialHistory

	start time.Time

	bootnodes []*discover.Node
}

type discoverTable interface {
	Self() *discover.Node
	Close()
	Resolve(target discover.NodeID) *discover.Node
	Delete(target discover.NodeID)
	Lookup(target discover.NodeID, netType byte) []*discover.Node
	ReadRandomNodes(nodes []*discover.Node, netType byte) int
	OpenTopNet()
}

type dialHistory []pastDial

type pastDial struct {
	id  discover.NodeID
	exp time.Time
}

type task interface {
	Do(*Server)
	GetNetType() byte
}

type dialTask struct {
	flags        connFlag
	ds           *dialstate
	netType      byte
	dest         *discover.Node
	lastResolved time.Time
	resolveDelay time.Duration
}

type discoverTask struct {
	results []*discover.Node
	netType byte
}

type waitExpireTask struct {
	time.Duration
	netType byte
}

func newDialState(static []*discover.Node, bootnodes []*discover.Node, ntab discoverTable, maxdyn int, netrestrict *netutil.Netlist) *dialstate {
	s := &dialstate{
		maxDynDials: maxdyn,
		ntab:        ntab,
		netrestrict: netrestrict,

		static: make(map[discover.NodeID]*dialTask),

		commonDialing:     make(map[discover.NodeID]connFlag),
		bootnodes:         make([]*discover.Node, len(bootnodes)),
		commonRandomNodes: make([]*discover.Node, maxdyn/2),
		consRandomNodes:   make([]*discover.Node, maxdyn/2),
		commonHist:        new(dialHistory),
		topHist:           new(dialHistory),
	}
	copy(s.bootnodes, bootnodes)
	for _, n := range static {
		s.addStatic(n)
	}
	return s
}

func (s *dialstate) addStatic(n *discover.Node) {

	s.static[n.ID] = &dialTask{flags: staticDialedConn, dest: n, ds: s}
}

func (s *dialstate) removeStatic(n *discover.Node) {

	delete(s.static, n.ID)
}

func (s *dialstate) newTasks(nRunning int, peers map[discover.NodeID]*Peer, now time.Time, openTopNet bool) []task {
	if s.start.IsZero() {
		s.start = now
	}

	var newtasks []task
	addDial := func(flag connFlag, n *discover.Node) bool {
		if err := s.checkDial(n, peers); err != nil {
			log.Trace("Skipping dial candidate", "id", n.ID, "addr", &net.TCPAddr{IP: n.IP, Port: int(n.TCP)}, "err", err)
			return false
		}

		s.commonDialing[n.ID] = flag

		newtasks = append(newtasks, &dialTask{flags: flag, dest: n, ds: s})
		return true
	}

	needDynDials := s.maxDynDials
	for _, p := range peers {
		if p.rw.is(dynDialedConn) {
			needDynDials--
		}
	}

	for _, flag := range s.commonDialing {
		if flag&dynDialedConn != 0 {
			needDynDials--
		}
	}
	s.commonHist.expire(now)

	for id, t := range s.static {
		err := s.checkDial(t.dest, peers)
		switch err {
		case errNotWhitelisted, errSelf:
			log.Warn("Removing static dial candidate", "id", t.dest.ID, "addr", &net.TCPAddr{IP: t.dest.IP, Port: int(t.dest.TCP)}, "err", err)
			delete(s.static, t.dest.ID)
		case nil:
			s.commonDialing[id] = t.flags
			newtasks = append(newtasks, t)
		}

	}

	if len(peers) == 0 && len(s.bootnodes) > 0 && needDynDials > 0 && now.Sub(s.start) > fallbackInterval {
		bootnode := s.bootnodes[0]
		s.bootnodes = append(s.bootnodes[:0], s.bootnodes[1:]...)
		s.bootnodes = append(s.bootnodes, bootnode)

		if addDial(dynDialedConn, bootnode) {
			needDynDials--
		}
	}

	randomCandidates := needDynDials / 2
	if openTopNet {
		if randomCandidates > 0 {
			n := s.ntab.ReadRandomNodes(s.commonRandomNodes, discover.ConsNet)
			for i := 0; i < randomCandidates && i < n; i++ {
				if addDial(dynDialedConn, s.commonRandomNodes[i]) {
					needDynDials--
				}
			}
		}
	}

	if randomCandidates > 0 {
		n := s.ntab.ReadRandomNodes(s.commonRandomNodes, discover.CommNet)
		for i := 0; i < randomCandidates && i < n; i++ {
			if addDial(dynDialedConn, s.commonRandomNodes[i]) {
				needDynDials--
			}
		}
	}

	i := 0
	for ; i < len(s.commonLookupBuf) && needDynDials > 0; i++ {
		if addDial(dynDialedConn, s.commonLookupBuf[i]) {
			s.commonLookupBuf = append(s.commonLookupBuf[:i], s.commonLookupBuf[i+1:]...)
			needDynDials--
		}

	}

	if len(s.commonLookupBuf) < needDynDials && !s.commonLookupRunning {
		s.commonLookupRunning = true
		newtasks = append(newtasks, &discoverTask{netType: discover.CommNet})
	}

	if nRunning == 0 && len(newtasks) == 0 && s.commonHist.Len() > 0 {
		t := &waitExpireTask{s.commonHist.min().exp.Sub(now), discover.CommNet}
		newtasks = append(newtasks, t)
	}
	if openTopNet {
		i := 0
		for ; i < len(s.topLookupBuf) && needDynDials > 0; i++ {
			if addDial(dynDialedConn, s.topLookupBuf[i]) {
				needDynDials--
			}
		}

		if len(s.topLookupBuf) < needDynDials && !s.topLookupRunning {
			s.topLookupRunning = true
			newtasks = append(newtasks, &discoverTask{netType: discover.ConsNet})
		}

		if nRunning == 0 && len(newtasks) == 0 && s.topHist.Len() > 0 {
			t := &waitExpireTask{s.topHist.min().exp.Sub(now), discover.ConsNet}
			newtasks = append(newtasks, t)
		}

	}

	return newtasks
}

var (
	errSelf             = errors.New("is self")
	errAlreadyDialing   = errors.New("already dialing")
	errAlreadyConnected = errors.New("already connected")
	errRecentlyDialed   = errors.New("recently dialed")
	errNotWhitelisted   = errors.New("not contained in netrestrict whitelist")
)

func (s *dialstate) checkDial(n *discover.Node, peers map[discover.NodeID]*Peer) error {

	_, dialing := s.commonDialing[n.ID]
	switch {
	case dialing:
		return errAlreadyDialing
	case peers[n.ID] != nil:
		return errAlreadyConnected
	case s.ntab != nil && n.ID == s.ntab.Self().ID:
		return errSelf
	case s.netrestrict != nil && !s.netrestrict.Contains(n.IP):
		return errNotWhitelisted
	case s.commonHist.contains(n.ID):
		return errRecentlyDialed
	}
	return nil

}

func (s *dialstate) taskDone(t task, now time.Time) {
	switch t := t.(type) {
	case *dialTask:
		delete(s.commonDialing, t.dest.ID)
	case *discoverTask:
		if t.netType == discover.CommNet {
			s.commonLookupRunning = false
			s.commonLookupBuf = append(s.commonLookupBuf, t.results...)
		} else {
			s.topLookupRunning = false
			s.topLookupBuf = append(s.topLookupBuf, t.results...)
		}

	}
}

func (t *dialTask) Do(srv *Server) {
	log.Debug("start remote node job", "node", t.dest, "netType", t.netType)
	if t.dest.Incomplete() {
		if !t.resolve(srv) {
			return
		}
	}
	err := t.dial(srv, t.dest)
	if err != nil {
		if t.ds.ntab != nil {
			t.ds.ntab.Delete(t.dest.ID)
		}

		log.Info("failed to connect remote peers", "err", err.Error())

	}

}

func (t *dialTask) GetNetType() byte {
	return t.netType

}

func (t *dialTask) resolve(srv *Server) bool {
	if srv.ntab == nil {
		log.Debug("Can't resolve node", "id", t.dest.ID, "err", "discovery is disabled")
		return false
	}
	if t.resolveDelay == 0 {
		t.resolveDelay = initialResolveDelay
	}
	if time.Since(t.lastResolved) < t.resolveDelay {
		return false
	}
	resolved := srv.ntab.Resolve(t.dest.ID)
	t.lastResolved = time.Now()
	if resolved == nil {
		t.resolveDelay *= 2
		if t.resolveDelay > maxResolveDelay {
			t.resolveDelay = maxResolveDelay
		}
		log.Debug("Resolving node failed", "id", t.dest.ID, "newdelay", t.resolveDelay)
		return false
	}

	t.resolveDelay = initialResolveDelay
	t.dest = resolved
	log.Debug("Resolved node", "id", t.dest.ID, "addr", &net.TCPAddr{IP: t.dest.IP, Port: int(t.dest.TCP)})
	return true
}

type dialError struct {
	error
}

func (t *dialTask) dial(srv *Server, dest *discover.Node) error {
	fd, err := srv.Dialer.Dial(dest)

	if err != nil {
		log.Info("tcp connect failed", "tcpconnectionErr", err)
		return &dialError{err}
	}
	mfd := newMeteredConn(fd, false)
	return srv.SetupConn(mfd, t.flags, dest, t.netType)
}

func (t *dialTask) String() string {
	return fmt.Sprintf("%v %x %v:%d", t.flags, t.dest.ID[:8], t.dest.IP, t.dest.TCP)
}

func (t *discoverTask) Do(srv *Server) {

	next := srv.lastLookup.Add(lookupInterval)
	if now := time.Now(); now.Before(next) {
		time.Sleep(next.Sub(now))
	}
	srv.lastLookup = time.Now()
	var target discover.NodeID
	rand.Read(target[:])
	t.results = srv.ntab.Lookup(target, t.netType)
}

func (t *discoverTask) GetNetType() byte {
	return t.netType
}

func (t *discoverTask) String() string {
	s := "discovery lookup"
	if len(t.results) > 0 {
		s += fmt.Sprintf(" (%d results)", len(t.results))
	}
	return s
}

func (t waitExpireTask) Do(*Server) {
	time.Sleep(t.Duration)
}

func (t waitExpireTask) GetNetType() byte {
	return t.netType
}
func (t waitExpireTask) String() string {
	return fmt.Sprintf("wait for dial hist expire (%v)", t.Duration)
}

func (h dialHistory) min() pastDial {
	return h[0]
}
func (h *dialHistory) add(id discover.NodeID, exp time.Time) {
	heap.Push(h, pastDial{id, exp})
}
func (h dialHistory) contains(id discover.NodeID) bool {
	for _, v := range h {
		if v.id == id {
			return true
		}
	}
	return false
}
func (h *dialHistory) expire(now time.Time) {
	for h.Len() > 0 && h.min().exp.Before(now) {
		heap.Pop(h)
	}
}

func (h dialHistory) Len() int           { return len(h) }
func (h dialHistory) Less(i, j int) bool { return h[i].exp.Before(h[j].exp) }
func (h dialHistory) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *dialHistory) Push(x interface{}) {
	*h = append(*h, x.(pastDial))
}
func (h *dialHistory) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
