package adapters

import (
	"errors"
	"fmt"
	"math"
	"net"
	"sync"

	"github.com/Aurorachain/go-Aurora/event"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/node"
	"github.com/Aurorachain/go-Aurora/p2p"
	"github.com/Aurorachain/go-Aurora/p2p/discover"
	"github.com/Aurorachain/go-Aurora/rpc"
)

type SimAdapter struct {
	mtx      sync.RWMutex
	nodes    map[discover.NodeID]*SimNode
	services map[string]ServiceFunc
}

func NewSimAdapter(services map[string]ServiceFunc) *SimAdapter {
	return &SimAdapter{
		nodes:    make(map[discover.NodeID]*SimNode),
		services: services,
	}
}

func (s *SimAdapter) Name() string {
	return "sim-adapter"
}

func (s *SimAdapter) NewNode(config *NodeConfig) (Node, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	id := config.ID
	if _, exists := s.nodes[id]; exists {
		return nil, fmt.Errorf("node already exists: %s", id)
	}

	if len(config.Services) == 0 {
		return nil, errors.New("node must have at least one service")
	}
	for _, service := range config.Services {
		if _, exists := s.services[service]; !exists {
			return nil, fmt.Errorf("unknown node service %q", service)
		}
	}

	n, err := node.New(&node.Config{
		P2P: p2p.Config{
			PrivateKey:      config.PrivateKey,
			MaxPeers:        math.MaxInt32,
			NoDiscovery:     true,
			Dialer:          s,
			EnableMsgEvents: true,
		},
		NoUSB:  true,
		Logger: log.New("node.id", id.String()),
	})
	if err != nil {
		return nil, err
	}

	simNode := &SimNode{
		ID:      id,
		config:  config,
		node:    n,
		adapter: s,
		running: make(map[string]node.Service),
	}
	s.nodes[id] = simNode
	return simNode, nil
}

func (s *SimAdapter) Dial(dest *discover.Node) (conn net.Conn, err error) {
	node, ok := s.GetNode(dest.ID)
	if !ok {
		return nil, fmt.Errorf("unknown node: %s", dest.ID)
	}
	srv := node.Server()
	if srv == nil {
		return nil, fmt.Errorf("node not running: %s", dest.ID)
	}
	pipe1, pipe2 := net.Pipe()
	go srv.SetupConn(pipe1, 0, nil,discover.CommNet)
	return pipe2, nil
}

func (s *SimAdapter) DialRPC(id discover.NodeID) (*rpc.Client, error) {
	node, ok := s.GetNode(id)
	if !ok {
		return nil, fmt.Errorf("unknown node: %s", id)
	}
	handler, err := node.node.RPCHandler()
	if err != nil {
		return nil, err
	}
	return rpc.DialInProc(handler), nil
}

func (s *SimAdapter) GetNode(id discover.NodeID) (*SimNode, bool) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	node, ok := s.nodes[id]
	return node, ok
}

type SimNode struct {
	lock         sync.RWMutex
	ID           discover.NodeID
	config       *NodeConfig
	adapter      *SimAdapter
	node         *node.Node
	running      map[string]node.Service
	client       *rpc.Client
	registerOnce sync.Once
}

func (self *SimNode) Addr() []byte {
	return []byte(self.Node().String())
}

func (self *SimNode) Node() *discover.Node {
	return discover.NewNode(self.ID, net.IP{127, 0, 0, 1}, 30303, 30303)
}

func (self *SimNode) Client() (*rpc.Client, error) {
	self.lock.RLock()
	defer self.lock.RUnlock()
	if self.client == nil {
		return nil, errors.New("node not started")
	}
	return self.client, nil
}

func (self *SimNode) ServeRPC(conn net.Conn) error {
	handler, err := self.node.RPCHandler()
	if err != nil {
		return err
	}
	handler.ServeCodec(rpc.NewJSONCodec(conn), rpc.OptionMethodInvocation|rpc.OptionSubscriptions)
	return nil
}

func (self *SimNode) Snapshots() (map[string][]byte, error) {
	self.lock.RLock()
	services := make(map[string]node.Service, len(self.running))
	for name, service := range self.running {
		services[name] = service
	}
	self.lock.RUnlock()
	if len(services) == 0 {
		return nil, errors.New("no running services")
	}
	snapshots := make(map[string][]byte)
	for name, service := range services {
		if s, ok := service.(interface {
			Snapshot() ([]byte, error)
		}); ok {
			snap, err := s.Snapshot()
			if err != nil {
				return nil, err
			}
			snapshots[name] = snap
		}
	}
	return snapshots, nil
}

func (self *SimNode) Start(snapshots map[string][]byte) error {
	newService := func(name string) func(ctx *node.ServiceContext) (node.Service, error) {
		return func(nodeCtx *node.ServiceContext) (node.Service, error) {
			ctx := &ServiceContext{
				RPCDialer:   self.adapter,
				NodeContext: nodeCtx,
				Config:      self.config,
			}
			if snapshots != nil {
				ctx.Snapshot = snapshots[name]
			}
			serviceFunc := self.adapter.services[name]
			service, err := serviceFunc(ctx)
			if err != nil {
				return nil, err
			}
			self.running[name] = service
			return service, nil
		}
	}

	var regErr error
	self.registerOnce.Do(func() {
		for _, name := range self.config.Services {
			if err := self.node.Register(newService(name)); err != nil {
				regErr = err
				return
			}
		}
	})
	if regErr != nil {
		return regErr
	}

	if err := self.node.Start(); err != nil {
		return err
	}

	handler, err := self.node.RPCHandler()
	if err != nil {
		return err
	}

	self.lock.Lock()
	self.client = rpc.DialInProc(handler)
	self.lock.Unlock()

	return nil
}

func (self *SimNode) Stop() error {
	self.lock.Lock()
	if self.client != nil {
		self.client.Close()
		self.client = nil
	}
	self.lock.Unlock()
	return self.node.Stop()
}

func (self *SimNode) Services() []node.Service {
	self.lock.RLock()
	defer self.lock.RUnlock()
	services := make([]node.Service, 0, len(self.running))
	for _, service := range self.running {
		services = append(services, service)
	}
	return services
}

func (self *SimNode) Server() *p2p.Server {
	return self.node.Server()
}

func (self *SimNode) SubscribeEvents(ch chan *p2p.PeerEvent) event.Subscription {
	srv := self.Server()
	if srv == nil {
		panic("node not running")
	}
	return srv.SubscribeEvents(ch)
}

func (self *SimNode) NodeInfo() *p2p.NodeInfo {
	server := self.Server()
	if server == nil {
		return &p2p.NodeInfo{
			ID:    self.ID.String(),
			Enode: self.Node().String(),
		}
	}
	return server.NodeInfo()
}
