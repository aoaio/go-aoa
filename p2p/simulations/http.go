package simulations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/Aurorachain/go-Aurora/event"
	"github.com/Aurorachain/go-Aurora/p2p"
	"github.com/Aurorachain/go-Aurora/p2p/discover"
	"github.com/Aurorachain/go-Aurora/p2p/simulations/adapters"
	"github.com/Aurorachain/go-Aurora/rpc"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/websocket"
)

var DefaultClient = NewClient("http://localhost:8888")

type Client struct {
	URL string

	client *http.Client
}

func NewClient(url string) *Client {
	return &Client{
		URL:    url,
		client: http.DefaultClient,
	}
}

func (c *Client) GetNetwork() (*Network, error) {
	network := &Network{}
	return network, c.Get("/", network)
}

func (c *Client) StartNetwork() error {
	return c.Post("/start", nil, nil)
}

func (c *Client) StopNetwork() error {
	return c.Post("/stop", nil, nil)
}

func (c *Client) CreateSnapshot() (*Snapshot, error) {
	snap := &Snapshot{}
	return snap, c.Get("/snapshot", snap)
}

func (c *Client) LoadSnapshot(snap *Snapshot) error {
	return c.Post("/snapshot", snap, nil)
}

type SubscribeOpts struct {

	Current bool

	Filter string
}

func (c *Client) SubscribeNetwork(events chan *Event, opts SubscribeOpts) (event.Subscription, error) {
	url := fmt.Sprintf("%s/events?current=%t&filter=%s", c.URL, opts.Current, opts.Filter)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		response, _ := ioutil.ReadAll(res.Body)
		res.Body.Close()
		return nil, fmt.Errorf("unexpected HTTP status: %s: %s", res.Status, response)
	}

	producer := func(stop <-chan struct{}) error {
		defer res.Body.Close()

		lines := make(chan string)
		errC := make(chan error, 1)
		go func() {
			s := bufio.NewScanner(res.Body)
			for s.Scan() {
				select {
				case lines <- s.Text():
				case <-stop:
					return
				}
			}
			errC <- s.Err()
		}()

		for {
			select {
			case line := <-lines:
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				event := &Event{}
				if err := json.Unmarshal([]byte(data), event); err != nil {
					return fmt.Errorf("error decoding SSE event: %s", err)
				}
				select {
				case events <- event:
				case <-stop:
					return nil
				}
			case err := <-errC:
				return err
			case <-stop:
				return nil
			}
		}
	}

	return event.NewSubscription(producer), nil
}

func (c *Client) GetNodes() ([]*p2p.NodeInfo, error) {
	var nodes []*p2p.NodeInfo
	return nodes, c.Get("/nodes", &nodes)
}

func (c *Client) CreateNode(config *adapters.NodeConfig) (*p2p.NodeInfo, error) {
	node := &p2p.NodeInfo{}
	return node, c.Post("/nodes", config, node)
}

func (c *Client) GetNode(nodeID string) (*p2p.NodeInfo, error) {
	node := &p2p.NodeInfo{}
	return node, c.Get(fmt.Sprintf("/nodes/%s", nodeID), node)
}

func (c *Client) StartNode(nodeID string) error {
	return c.Post(fmt.Sprintf("/nodes/%s/start", nodeID), nil, nil)
}

func (c *Client) StopNode(nodeID string) error {
	return c.Post(fmt.Sprintf("/nodes/%s/stop", nodeID), nil, nil)
}

func (c *Client) ConnectNode(nodeID, peerID string) error {
	return c.Post(fmt.Sprintf("/nodes/%s/conn/%s", nodeID, peerID), nil, nil)
}

func (c *Client) DisconnectNode(nodeID, peerID string) error {
	return c.Delete(fmt.Sprintf("/nodes/%s/conn/%s", nodeID, peerID))
}

func (c *Client) RPCClient(ctx context.Context, nodeID string) (*rpc.Client, error) {
	baseURL := strings.Replace(c.URL, "http", "ws", 1)
	return rpc.DialWebsocket(ctx, fmt.Sprintf("%s/nodes/%s/rpc", baseURL, nodeID), "")
}

func (c *Client) Get(path string, out interface{}) error {
	return c.Send("GET", path, nil, out)
}

func (c *Client) Post(path string, in, out interface{}) error {
	return c.Send("POST", path, in, out)
}

func (c *Client) Delete(path string) error {
	return c.Send("DELETE", path, nil, nil)
}

func (c *Client) Send(method, path string, in, out interface{}) error {
	var body []byte
	if in != nil {
		var err error
		body, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, c.URL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		response, _ := ioutil.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP status: %s: %s", res.Status, response)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return err
		}
	}
	return nil
}

type Server struct {
	router     *httprouter.Router
	network    *Network
	mockerStop chan struct{}
	mockerMtx  sync.Mutex
}

func NewServer(network *Network) *Server {
	s := &Server{
		router:  httprouter.New(),
		network: network,
	}

	s.OPTIONS("/", s.Options)
	s.GET("/", s.GetNetwork)
	s.POST("/start", s.StartNetwork)
	s.POST("/stop", s.StopNetwork)
	s.POST("/mocker/start", s.StartMocker)
	s.POST("/mocker/stop", s.StopMocker)
	s.GET("/mocker", s.GetMockers)
	s.POST("/reset", s.ResetNetwork)
	s.GET("/events", s.StreamNetworkEvents)
	s.GET("/snapshot", s.CreateSnapshot)
	s.POST("/snapshot", s.LoadSnapshot)
	s.POST("/nodes", s.CreateNode)
	s.GET("/nodes", s.GetNodes)
	s.GET("/nodes/:nodeid", s.GetNode)
	s.POST("/nodes/:nodeid/start", s.StartNode)
	s.POST("/nodes/:nodeid/stop", s.StopNode)
	s.POST("/nodes/:nodeid/conn/:peerid", s.ConnectNode)
	s.DELETE("/nodes/:nodeid/conn/:peerid", s.DisconnectNode)
	s.GET("/nodes/:nodeid/rpc", s.NodeRPC)

	return s
}

func (s *Server) GetNetwork(w http.ResponseWriter, req *http.Request) {
	s.JSON(w, http.StatusOK, s.network)
}

func (s *Server) StartNetwork(w http.ResponseWriter, req *http.Request) {
	if err := s.network.StartAll(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) StopNetwork(w http.ResponseWriter, req *http.Request) {
	if err := s.network.StopAll(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) StartMocker(w http.ResponseWriter, req *http.Request) {
	s.mockerMtx.Lock()
	defer s.mockerMtx.Unlock()
	if s.mockerStop != nil {
		http.Error(w, "mocker already running", http.StatusInternalServerError)
		return
	}
	mockerType := req.FormValue("mocker-type")
	mockerFn := LookupMocker(mockerType)
	if mockerFn == nil {
		http.Error(w, fmt.Sprintf("unknown mocker type %q", mockerType), http.StatusBadRequest)
		return
	}
	nodeCount, err := strconv.Atoi(req.FormValue("node-count"))
	if err != nil {
		http.Error(w, "invalid node-count provided", http.StatusBadRequest)
		return
	}
	s.mockerStop = make(chan struct{})
	go mockerFn(s.network, s.mockerStop, nodeCount)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) StopMocker(w http.ResponseWriter, req *http.Request) {
	s.mockerMtx.Lock()
	defer s.mockerMtx.Unlock()
	if s.mockerStop == nil {
		http.Error(w, "stop channel not initialized", http.StatusInternalServerError)
		return
	}
	close(s.mockerStop)
	s.mockerStop = nil

	w.WriteHeader(http.StatusOK)
}

func (s *Server) GetMockers(w http.ResponseWriter, req *http.Request) {

	list := GetMockerList()
	s.JSON(w, http.StatusOK, list)
}

func (s *Server) ResetNetwork(w http.ResponseWriter, req *http.Request) {
	s.network.Reset()

	w.WriteHeader(http.StatusOK)
}

func (s *Server) StreamNetworkEvents(w http.ResponseWriter, req *http.Request) {
	events := make(chan *Event)
	sub := s.network.events.Subscribe(events)
	defer sub.Unsubscribe()

	var clientGone <-chan bool
	if cn, ok := w.(http.CloseNotifier); ok {
		clientGone = cn.CloseNotify()
	}

	write := func(event, data string) {
		fmt.Fprintf(w, "event: %s\n", event)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if fw, ok := w.(http.Flusher); ok {
			fw.Flush()
		}
	}
	writeEvent := func(event *Event) error {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		write("network", string(data))
		return nil
	}
	writeErr := func(err error) {
		write("error", err.Error())
	}

	var filters MsgFilters
	if filterParam := req.URL.Query().Get("filter"); filterParam != "" {
		var err error
		filters, err = NewMsgFilters(filterParam)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "\n\n")
	if fw, ok := w.(http.Flusher); ok {
		fw.Flush()
	}

	if req.URL.Query().Get("current") == "true" {
		snap, err := s.network.Snapshot()
		if err != nil {
			writeErr(err)
			return
		}
		for _, node := range snap.Nodes {
			event := NewEvent(&node.Node)
			if err := writeEvent(event); err != nil {
				writeErr(err)
				return
			}
		}
		for _, conn := range snap.Conns {
			event := NewEvent(&conn)
			if err := writeEvent(event); err != nil {
				writeErr(err)
				return
			}
		}
	}

	for {
		select {
		case event := <-events:

			if event.Msg != nil && !filters.Match(event.Msg) {
				continue
			}
			if err := writeEvent(event); err != nil {
				writeErr(err)
				return
			}
		case <-clientGone:
			return
		}
	}
}

func NewMsgFilters(filterParam string) (MsgFilters, error) {
	filters := make(MsgFilters)
	for _, filter := range strings.Split(filterParam, "-") {
		protoCodes := strings.SplitN(filter, ":", 2)
		if len(protoCodes) != 2 || protoCodes[0] == "" || protoCodes[1] == "" {
			return nil, fmt.Errorf("invalid message filter: %s", filter)
		}
		proto := protoCodes[0]
		for _, code := range strings.Split(protoCodes[1], ",") {
			if code == "*" || code == "-1" {
				filters[MsgFilter{Proto: proto, Code: -1}] = struct{}{}
				continue
			}
			n, err := strconv.ParseUint(code, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid message code: %s", code)
			}
			filters[MsgFilter{Proto: proto, Code: int64(n)}] = struct{}{}
		}
	}
	return filters, nil
}

type MsgFilters map[MsgFilter]struct{}

func (m MsgFilters) Match(msg *Msg) bool {

	if _, ok := m[MsgFilter{Proto: msg.Protocol, Code: -1}]; ok {
		return true
	}

	if _, ok := m[MsgFilter{Proto: msg.Protocol, Code: int64(msg.Code)}]; ok {
		return true
	}

	return false
}

type MsgFilter struct {

	Proto string

	Code int64
}

func (s *Server) CreateSnapshot(w http.ResponseWriter, req *http.Request) {
	snap, err := s.network.Snapshot()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.JSON(w, http.StatusOK, snap)
}

func (s *Server) LoadSnapshot(w http.ResponseWriter, req *http.Request) {
	snap := &Snapshot{}
	if err := json.NewDecoder(req.Body).Decode(snap); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.network.Load(snap); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.JSON(w, http.StatusOK, s.network)
}

func (s *Server) CreateNode(w http.ResponseWriter, req *http.Request) {
	config := adapters.RandomNodeConfig()
	err := json.NewDecoder(req.Body).Decode(config)
	if err != nil && err != io.EOF {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	node, err := s.network.NewNodeWithConfig(config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.JSON(w, http.StatusCreated, node.NodeInfo())
}

func (s *Server) GetNodes(w http.ResponseWriter, req *http.Request) {
	nodes := s.network.GetNodes()

	infos := make([]*p2p.NodeInfo, len(nodes))
	for i, node := range nodes {
		infos[i] = node.NodeInfo()
	}

	s.JSON(w, http.StatusOK, infos)
}

func (s *Server) GetNode(w http.ResponseWriter, req *http.Request) {
	node := req.Context().Value("node").(*Node)

	s.JSON(w, http.StatusOK, node.NodeInfo())
}

func (s *Server) StartNode(w http.ResponseWriter, req *http.Request) {
	node := req.Context().Value("node").(*Node)

	if err := s.network.Start(node.ID()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.JSON(w, http.StatusOK, node.NodeInfo())
}

func (s *Server) StopNode(w http.ResponseWriter, req *http.Request) {
	node := req.Context().Value("node").(*Node)

	if err := s.network.Stop(node.ID()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.JSON(w, http.StatusOK, node.NodeInfo())
}

func (s *Server) ConnectNode(w http.ResponseWriter, req *http.Request) {
	node := req.Context().Value("node").(*Node)
	peer := req.Context().Value("peer").(*Node)

	if err := s.network.Connect(node.ID(), peer.ID()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.JSON(w, http.StatusOK, node.NodeInfo())
}

func (s *Server) DisconnectNode(w http.ResponseWriter, req *http.Request) {
	node := req.Context().Value("node").(*Node)
	peer := req.Context().Value("peer").(*Node)

	if err := s.network.Disconnect(node.ID(), peer.ID()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.JSON(w, http.StatusOK, node.NodeInfo())
}

func (s *Server) Options(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) NodeRPC(w http.ResponseWriter, req *http.Request) {
	node := req.Context().Value("node").(*Node)

	handler := func(conn *websocket.Conn) {
		node.ServeRPC(conn)
	}

	websocket.Server{Handler: handler}.ServeHTTP(w, req)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.router.ServeHTTP(w, req)
}

func (s *Server) GET(path string, handle http.HandlerFunc) {
	s.router.GET(path, s.wrapHandler(handle))
}

func (s *Server) POST(path string, handle http.HandlerFunc) {
	s.router.POST(path, s.wrapHandler(handle))
}

func (s *Server) DELETE(path string, handle http.HandlerFunc) {
	s.router.DELETE(path, s.wrapHandler(handle))
}

func (s *Server) OPTIONS(path string, handle http.HandlerFunc) {
	s.router.OPTIONS("/*path", s.wrapHandler(handle))
}

func (s *Server) JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) wrapHandler(handler http.HandlerFunc) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

		ctx := context.Background()

		if id := params.ByName("nodeid"); id != "" {
			var node *Node
			if nodeID, err := discover.HexID(id); err == nil {
				node = s.network.GetNode(nodeID)
			} else {
				node = s.network.GetNodeByName(id)
			}
			if node == nil {
				http.NotFound(w, req)
				return
			}
			ctx = context.WithValue(ctx, "node", node)
		}

		if id := params.ByName("peerid"); id != "" {
			var peer *Node
			if peerID, err := discover.HexID(id); err == nil {
				peer = s.network.GetNode(peerID)
			} else {
				peer = s.network.GetNodeByName(id)
			}
			if peer == nil {
				http.NotFound(w, req)
				return
			}
			ctx = context.WithValue(ctx, "peer", peer)
		}

		handler(w, req.WithContext(ctx))
	}
}
