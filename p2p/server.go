package p2p

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/mclock"
	"github.com/Aurorachain/go-Aurora/event"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/p2p/discover"

	"github.com/Aurorachain/go-Aurora/p2p/nat"
	"github.com/Aurorachain/go-Aurora/p2p/netutil"
)

const (
	defaultDialTimeout      = 15 * time.Second
	refreshPeersInterval    = 30 * time.Second
	staticPeerCheckInterval = 15 * time.Second

	maxAcceptConns = 50

	maxActiveDialTasks = 1000

	frameReadTimeout = 30 * time.Second

	frameWriteTimeout = 20 * time.Second
)

var errServerStopped = errors.New("server stopped")

type Config struct {

	PrivateKey *ecdsa.PrivateKey `toml:"-"`

	DiscoveryV5 bool `toml:",omitempty"`

	MaxPeers int

	OpenTopNet bool

	MaxPendingPeers int `toml:",omitempty"`

	NoDiscovery bool

	Name string `toml:"-"`

	BootstrapNodes []*discover.Node

	StaticNodes []*discover.Node

	TrustedNodes []*discover.Node

	NetRestrict *netutil.Netlist `toml:",omitempty"`

	NodeDatabase string `toml:",omitempty"`

	Protocols []Protocol `toml:"-"`

	ListenAddr string

	NAT nat.Interface `toml:",omitempty"`

	Dialer NodeDialer `toml:"-"`

	NoDial bool `toml:",omitempty"`

	EnableMsgEvents bool

	Logger log.Logger
}

type Server struct {

	Config

	newTransport func(net.Conn) transport
	newPeerHook  func(*Peer)

	lock    sync.Mutex
	running bool

	ntab         discoverTable
	listener     net.Listener
	ourHandshake *protoHandshake
	lastLookup   time.Time

	peerOp     chan peerOpFunc
	peerOpDone chan struct{}

	openTopNetCh  chan struct{}
	quit          chan struct{}
	addstatic     chan *discover.Node
	removestatic  chan *discover.Node
	posthandshake chan *conn
	addpeer       chan *conn
	delpeer       chan peerDrop
	loopWG        sync.WaitGroup
	peerFeed      event.Feed
	log           log.Logger
}

type peerOpFunc func(map[discover.NodeID]*Peer)

type peerDrop struct {
	*Peer
	err       error
	requested bool
}

type connFlag int

const (
	dynDialedConn connFlag = 1 << iota
	staticDialedConn
	inboundConn
	trustedConn
)

type conn struct {
	fd net.Conn
	transport
	flags   connFlag
	cont    chan error
	id      discover.NodeID
	netType byte
	caps    []Cap
	name    string
}

type transport interface {

	doEncHandshake(prv *ecdsa.PrivateKey, dialDest *discover.Node) (discover.NodeID, error)
	doProtoHandshake(our *protoHandshake) (*protoHandshake, error)

	MsgReadWriter

	close(err error)
}

func (c *conn) String() string {
	s := c.flags.String()
	if (c.id != discover.NodeID{}) {
		s += " " + c.id.String()
	}
	s += " " + c.fd.RemoteAddr().String()
	return s
}

func (f connFlag) String() string {
	s := ""
	if f&trustedConn != 0 {
		s += "-trusted"
	}
	if f&dynDialedConn != 0 {
		s += "-dyndial"
	}
	if f&staticDialedConn != 0 {
		s += "-staticdial"
	}
	if f&inboundConn != 0 {
		s += "-inbound"
	}
	if s != "" {
		s = s[1:]
	}
	return s
}

func (c *conn) is(f connFlag) bool {
	return c.flags&f != 0
}

func (srv *Server) Peers() []*Peer {
	var ps []*Peer
	select {

	case srv.peerOp <- func(peers map[discover.NodeID]*Peer) {
		for _, p := range peers {
			ps = append(ps, p)
		}
	}:
		<-srv.peerOpDone
	case <-srv.quit:
	}
	return ps
}

func (srv *Server) PeerCount() (int, int) {
	var commCount int
	var topCount int
	select {
	case srv.peerOp <- func(ps map[discover.NodeID]*Peer) {
		for _, p := range ps {
			if p.netType == discover.CommNet {
				commCount++
			} else {
				topCount++
			}

		}
	}:
		<-srv.peerOpDone
	case <-srv.quit:
	}
	return commCount, topCount
}

func (srv *Server) AddPeer(node *discover.Node) {
	select {
	case srv.addstatic <- node:
	case <-srv.quit:
	}
}

func (srv *Server) OpenTopNet() bool {

	srv.openTopNetCh <- struct{}{}
	return true
}

func (srv *Server) RemovePeer(node *discover.Node) {
	select {
	case srv.removestatic <- node:
	case <-srv.quit:
	}
}

func (srv *Server) SubscribeEvents(ch chan *PeerEvent) event.Subscription {
	return srv.peerFeed.Subscribe(ch)
}

func (srv *Server) Self() *discover.Node {
	srv.lock.Lock()
	defer srv.lock.Unlock()

	if !srv.running {
		return &discover.Node{IP: net.ParseIP("0.0.0.0")}
	}
	return srv.makeSelf(srv.listener, srv.ntab)
}

func (srv *Server) makeSelf(listener net.Listener, ntab discoverTable) *discover.Node {

	if ntab == nil {

		if listener == nil {
			return &discover.Node{IP: net.ParseIP("0.0.0.0"), ID: discover.PubkeyID(&srv.PrivateKey.PublicKey)}
		}

		addr := listener.Addr().(*net.TCPAddr)
		return &discover.Node{
			ID:  discover.PubkeyID(&srv.PrivateKey.PublicKey),
			IP:  addr.IP,
			TCP: uint16(addr.Port),
		}
	}

	return ntab.Self()
}

func (srv *Server) Stop() {
	srv.lock.Lock()
	defer srv.lock.Unlock()
	if !srv.running {
		return
	}
	srv.running = false
	if srv.listener != nil {

		srv.listener.Close()
	}
	close(srv.quit)
	srv.loopWG.Wait()
}

type sharedUDPConn struct {
	*net.UDPConn
	unhandled chan discover.ReadPacket
}

func (s *sharedUDPConn) ReadFromUDP(b []byte) (n int, addr *net.UDPAddr, err error) {
	packet, ok := <-s.unhandled
	if !ok {
		return 0, nil, fmt.Errorf("Connection was closed")
	}
	l := len(packet.Data)
	if l > len(b) {
		l = len(b)
	}
	copy(b[:l], packet.Data[:l])
	return l, packet.Addr, nil
}

func (s *sharedUDPConn) Close() error {
	return nil
}

func (srv *Server) Start() (err error) {

	srv.lock.Lock()
	defer srv.lock.Unlock()
	if srv.running {
		return errors.New("server already running")
	}
	srv.running = true
	srv.log = srv.Config.Logger
	if srv.log == nil {
		srv.log = log.New()
	}
	srv.log.Info("Starting P2P networking")

	if srv.PrivateKey == nil {
		return fmt.Errorf("Server.PrivateKey must be set to a non-nil key")
	}
	if srv.newTransport == nil {
		srv.newTransport = newRLPX
	}
	if srv.Dialer == nil {
		srv.Dialer = TCPDialer{&net.Dialer{Timeout: defaultDialTimeout}}
	}
	srv.quit = make(chan struct{})
	srv.addpeer = make(chan *conn)
	srv.delpeer = make(chan peerDrop)
	srv.posthandshake = make(chan *conn)
	srv.addstatic = make(chan *discover.Node)
	srv.removestatic = make(chan *discover.Node)
	srv.peerOp = make(chan peerOpFunc)
	srv.peerOpDone = make(chan struct{})
	srv.openTopNetCh = make(chan struct{})
	var (
		conn *net.UDPConn

		realaddr  *net.UDPAddr
		unhandled chan discover.ReadPacket
	)

	if !srv.NoDiscovery {
		addr, err := net.ResolveUDPAddr("udp", srv.ListenAddr)
		if err != nil {
			return err
		}
		conn, err = net.ListenUDP("udp", addr)
		if err != nil {
			return err
		}

		realaddr = conn.LocalAddr().(*net.UDPAddr)
		if srv.NAT != nil {
			if !realaddr.IP.IsLoopback() {
				go nat.Map(srv.NAT, srv.quit, "udp", realaddr.Port, realaddr.Port, "aurora discovery")
			}

			if ext, err := srv.NAT.ExternalIP(); err == nil {
				realaddr = &net.UDPAddr{IP: ext, Port: realaddr.Port}
			}
		}
	}

	if !srv.NoDiscovery {
		ntab, err := discover.ListenUDP(srv.PrivateKey, conn, realaddr, unhandled, srv.NodeDatabase, srv.NetRestrict, srv.Config.OpenTopNet)
		if err != nil {
			return err
		}
		if err := ntab.SetFallbackNodes(srv.BootstrapNodes); err != nil {
			return err
		}
		srv.ntab = ntab
	}

	dynPeers := (srv.MaxPeers + 1) / 2
	if srv.NoDiscovery {
		dynPeers = 0
	}
	dialer := newDialState(srv.StaticNodes, srv.BootstrapNodes, srv.ntab, dynPeers, srv.NetRestrict)

	srv.ourHandshake = &protoHandshake{Version: baseProtocolVersion, Name: srv.Name, ID: discover.PubkeyID(&srv.PrivateKey.PublicKey)}
	for _, p := range srv.Protocols {
		srv.ourHandshake.Caps = append(srv.ourHandshake.Caps, p.cap())
	}

	if srv.ListenAddr != "" {
		if err := srv.startListening(); err != nil {
			return err
		}
	}
	if srv.NoDial && srv.ListenAddr == "" {
		srv.log.Warn("P2P server will be useless, neither dialing nor listening")
	}

	srv.loopWG.Add(1)
	go srv.run(dialer)
	srv.running = true
	return nil
}

func (srv *Server) startListening() error {

	listener, err := net.Listen("tcp", srv.ListenAddr)
	if err != nil {
		return err
	}
	laddr := listener.Addr().(*net.TCPAddr)
	srv.ListenAddr = laddr.String()
	srv.listener = listener
	srv.loopWG.Add(1)
	go srv.listenLoop()

	if !laddr.IP.IsLoopback() && srv.NAT != nil {
		srv.loopWG.Add(1)
		go func() {
			nat.Map(srv.NAT, srv.quit, "tcp", laddr.Port, laddr.Port, "aurora p2p")
			srv.loopWG.Done()
		}()
	}
	return nil
}

type dialer interface {
	newTasks(running int, peers map[discover.NodeID]*Peer, now time.Time, openTopNet bool) []task
	taskDone(task, time.Time)
	addStatic(*discover.Node)
	removeStatic(*discover.Node)
}

func (srv *Server) run(dialstate dialer) {

	defer srv.loopWG.Done()
	var (
		peers = make(map[discover.NodeID]*Peer)

		trusted  = make(map[discover.NodeID]bool, len(srv.TrustedNodes))
		taskdone = make(chan task, maxActiveDialTasks)

		runningTasks []task
		queuedTasks  []task
	)

	for _, n := range srv.TrustedNodes {
		trusted[n.ID] = true
	}

	delTask := func(t task) {
		for i := range runningTasks {
			if runningTasks[i] == t {
				runningTasks = append(runningTasks[:i], runningTasks[i+1:]...)
				break
			}
		}
	}

	startTasks := func(ts []task) (rest []task) {

		i := 0
		for ; len(runningTasks) < maxActiveDialTasks && i < len(ts); i++ {
			t := ts[i]

			go func() { t.Do(srv); taskdone <- t }()
			runningTasks = append(runningTasks, t)
		}
		return ts[i:]
	}
	scheduleTasks := func() {

		queuedTasks = append(queuedTasks[:0], startTasks(queuedTasks)...)

		if len(runningTasks) < maxActiveDialTasks {
			nt := dialstate.newTasks(len(runningTasks)+len(queuedTasks), peers, time.Now(), srv.Config.OpenTopNet)
			/*if srv.Config.OpenTopNet {
				opNt := dialstate.newTasks(len(runningTasks)+len(queuedTasks), consPeers, time.Now(), discover.ConsNet)
				nt = append(nt, opNt...)
			}*/
			queuedTasks = append(queuedTasks, startTasks(nt)...)

		}
	}

	tick := time.NewTicker(3 * time.Second)

running:
	for {

		select {
		case <-tick.C:
			scheduleTasks()
		case <-srv.openTopNetCh:
			srv.Config.OpenTopNet = true
			for _, peer := range peers {
				peer.Disconnect(DiscRequested)
			}

			peers = make(map[discover.NodeID]*Peer)
			scheduleTasks()
			srv.ntab.OpenTopNet()
		case <-srv.quit:

			break running
		case n := <-srv.addstatic:

			log.Info("Adding static node", "node", n)
			dialstate.addStatic(n)
		case n := <-srv.removestatic:

			srv.log.Debug("Removing static node", "node", n)
			dialstate.removeStatic(n)

			if p, ok := peers[n.ID]; ok {
				p.Disconnect(DiscRequested)
			}

		case op := <-srv.peerOp:

			op(peers)
			srv.peerOpDone <- struct{}{}
		case t := <-taskdone:

			srv.log.Trace("Dial task done", "task", t)
			dialstate.taskDone(t, time.Now())
			delTask(t)
		case c := <-srv.posthandshake:

			if trusted[c.id] {

				c.flags |= trustedConn
			}

			select {
			case c.cont <- srv.encHandshakeChecks(peers, c):
			case <-srv.quit:
				break running
			}
		case c := <-srv.addpeer:

			log.Debug("add connected peers", "addpeer", c)
			var err error

			err = srv.protoHandshakeChecks(peers, c)
			if err == nil {

				p := newPeer(c, srv.Protocols)

				if srv.EnableMsgEvents {
					p.events = &srv.peerFeed
				}
				name := truncateName(c.name)
				srv.log.Debug("Adding p2p peer", "name", name, "addr", c.fd.RemoteAddr(), "peers", len(peers)+1)

				peers[c.id] = p
				go srv.runPeer(p)
			}

			select {
			case c.cont <- err:
			case <-srv.quit:
				break running
			}
		case pd := <-srv.delpeer:

			d := common.PrettyDuration(mclock.Now() - pd.created)
			srv.ntab.Delete(pd.ID())

			pd.log.Debug("Removing p2p peer", "duration", d, "peers", len(peers)-1, "req", pd.requested, "err", pd.err)
			delete(peers, pd.ID())

		}
	}

	srv.log.Trace("P2P networking is spinning down")
	tick.Stop()

	if srv.ntab != nil {
		srv.ntab.Close()
	}

	for _, p := range peers {
		p.Disconnect(DiscQuitting)
	}

	for len(peers) > 0 {
		p := <-srv.delpeer
		p.log.Trace("<-delpeer (spindown)", "remainingTasks", len(runningTasks))
		delete(peers, p.ID())
	}

}

func (srv *Server) protoHandshakeChecks(peers map[discover.NodeID]*Peer, c *conn) error {

	if len(srv.Protocols) > 0 && countMatchingProtocols(srv.Protocols, c.caps) == 0 {
		return DiscUselessPeer
	}

	return srv.encHandshakeChecks(peers, c)
}

func (srv *Server) encHandshakeChecks(peers map[discover.NodeID]*Peer, c *conn) error {
	switch {
	case !c.is(trustedConn|staticDialedConn) && len(peers) >= srv.MaxPeers:
		return DiscTooManyPeers
	case peers[c.id] != nil:
		return DiscAlreadyConnected
	case c.id == srv.Self().ID:
		return DiscSelf
	default:
		return nil
	}
}

type tempError interface {
	Temporary() bool
}

func (srv *Server) listenLoop() {
	defer srv.loopWG.Done()
	srv.log.Info("RLPx listener up", "self", srv.makeSelf(srv.listener, srv.ntab))

	tokens := maxAcceptConns
	if srv.MaxPendingPeers > 0 {
		tokens = srv.MaxPendingPeers
	}
	slots := make(chan struct{}, tokens)
	for i := 0; i < tokens; i++ {
		slots <- struct{}{}
	}

	for {

		<-slots

		var (
			fd  net.Conn
			err error
		)
		for {
			fd, err = srv.listener.Accept()

			if tempErr, ok := err.(tempError); ok && tempErr.Temporary() {
				srv.log.Debug("Temporary read error", "err", err)
				continue
			} else if err != nil {
				srv.log.Debug("Read error", "err", err)
				return
			}
			break
		}

		if srv.NetRestrict != nil {
			if tcp, ok := fd.RemoteAddr().(*net.TCPAddr); ok && !srv.NetRestrict.Contains(tcp.IP) {
				srv.log.Debug("Rejected conn (not whitelisted in NetRestrict)", "addr", fd.RemoteAddr())
				fd.Close()
				slots <- struct{}{}
				continue
			}
		}

		fd = newMeteredConn(fd, true)
		srv.log.Trace("Accepted connection", "addr", fd.RemoteAddr())

		go func() {
			srv.SetupConn(fd, inboundConn, nil, discover.CommNet)
			slots <- struct{}{}
		}()
	}
}

func (srv *Server) SetupConn(fd net.Conn, flags connFlag, dialDest *discover.Node, netType byte) error {

	self := srv.Self()
	if self == nil {
		return errors.New("shutdown")
	}
	c := &conn{fd: fd, transport: srv.newTransport(fd), flags: flags, cont: make(chan error), netType: netType}
	err := srv.setupConn(c, flags, dialDest)
	if err != nil {
		c.close(err)
		srv.log.Error("Setting up connection failed", "id", c.id, "err", err, "dialDest", dialDest)
	}
	return err
}

func (srv *Server) setupConn(c *conn, flags connFlag, dialDest *discover.Node) error {

	srv.lock.Lock()
	running := srv.running
	srv.lock.Unlock()
	if !running {
		return errServerStopped
	}

	var err error
	if c.id, err = c.doEncHandshake(srv.PrivateKey, dialDest); err != nil {
		srv.log.Error("Failed RLPx handshake", "addr", c.fd.RemoteAddr(), "conn", c.flags, "err", err)
		return err
	}
	clog := srv.log.New("id", c.id, "addr", c.fd.RemoteAddr(), "conn", c.flags)

	if dialDest != nil && c.id != dialDest.ID {
		log.Error("Dialed identity mismatch", "want", c, dialDest.ID)
		return DiscUnexpectedIdentity
	}
	err = srv.checkpoint(c, srv.posthandshake)
	if err != nil {
		log.Error("Rejected peer before protocol handshake", "err", err)
		return err
	}

	if srv.Config.OpenTopNet == true {
		srv.ourHandshake.NetType = discover.ConsNet
	}
	phs, err := c.doProtoHandshake(srv.ourHandshake)

	if err != nil {
		clog.Debug("Failed proto handshake", "err", err)
		return err
	}
	c.netType = phs.NetType
	if phs.ID != c.id {
		clog.Info("Wrong devp2p handshake identity", "err", phs.ID)
		return DiscUnexpectedIdentity
	}
	c.caps, c.name = phs.Caps, phs.Name
	err = srv.checkpoint(c, srv.addpeer)
	if err != nil {
		clog.Debug("Rejected peer", "err", err)
		return err
	}

	clog.Trace("connection set up", "inbound", dialDest == nil)
	return nil
}

func truncateName(s string) string {
	if len(s) > 20 {
		return s[:20] + "..."
	}
	return s
}

func (srv *Server) checkpoint(c *conn, stage chan<- *conn) error {
	select {
	case stage <- c:
	case <-srv.quit:
		return errServerStopped
	}
	select {
	case err := <-c.cont:
		return err
	case <-srv.quit:
		return errServerStopped
	}
}

func (srv *Server) runPeer(p *Peer) {
	if srv.newPeerHook != nil {
		srv.newPeerHook(p)
	}

	srv.peerFeed.Send(&PeerEvent{
		Type: PeerEventTypeAdd,
		Peer: p.ID(),
	})

	remoteRequested, err := p.run()

	srv.peerFeed.Send(&PeerEvent{
		Type:  PeerEventTypeDrop,
		Peer:  p.ID(),
		Error: err.Error(),
	})

	srv.delpeer <- peerDrop{p, err, remoteRequested}
}

type NodeInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Enode string `json:"enode"`
	IP    string `json:"ip"`
	Ports struct {
		Discovery int `json:"discovery"`
		Listener  int `json:"listener"`
	} `json:"ports"`
	ListenAddr string                 `json:"listenAddr"`
	Protocols  map[string]interface{} `json:"protocols"`
}

func (srv *Server) NodeInfo() *NodeInfo {
	node := srv.Self()

	info := &NodeInfo{
		Name:       srv.Name,
		Enode:      node.String(),
		ID:         node.ID.String(),
		IP:         node.IP.String(),
		ListenAddr: srv.ListenAddr,
		Protocols:  make(map[string]interface{}),
	}
	info.Ports.Discovery = int(node.UDP)
	info.Ports.Listener = int(node.TCP)

	for _, proto := range srv.Protocols {
		if _, ok := info.Protocols[proto.Name]; !ok {
			nodeInfo := interface{}("unknown")
			if query := proto.NodeInfo; query != nil {
				nodeInfo = proto.NodeInfo()
			}
			info.Protocols[proto.Name] = nodeInfo
		}
	}
	return info
}

func (srv *Server) PeersInfo() []*PeerInfo {

	c, t := srv.PeerCount()
	infos := make([]*PeerInfo, 0, c+t)
	for _, peer := range srv.Peers() {
		if peer != nil {
			infos = append(infos, peer.Info())
		}
	}

	for i := 0; i < len(infos); i++ {
		for j := i + 1; j < len(infos); j++ {
			if infos[i].ID > infos[j].ID {
				infos[i], infos[j] = infos[j], infos[i]
			}
		}
	}
	return infos
}
