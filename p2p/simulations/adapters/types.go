package adapters

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/node"
	"github.com/Aurorachain/go-Aurora/p2p"
	"github.com/Aurorachain/go-Aurora/p2p/discover"
	"github.com/Aurorachain/go-Aurora/rpc"
	"github.com/docker/docker/pkg/reexec"
	"net"
	"os"
)

type Node interface {

	Addr() []byte

	Client() (*rpc.Client, error)

	ServeRPC(net.Conn) error

	Start(snapshots map[string][]byte) error

	Stop() error

	NodeInfo() *p2p.NodeInfo

	Snapshots() (map[string][]byte, error)
}

type NodeAdapter interface {

	Name() string

	NewNode(config *NodeConfig) (Node, error)
}

type NodeConfig struct {

	ID discover.NodeID

	PrivateKey *ecdsa.PrivateKey

	EnableMsgEvents bool

	Name string

	Services []string

	Reachable func(id discover.NodeID) bool
}

type nodeConfigJSON struct {
	ID         string   `json:"id"`
	PrivateKey string   `json:"private_key"`
	Name       string   `json:"name"`
	Services   []string `json:"services"`
}

func (n *NodeConfig) MarshalJSON() ([]byte, error) {
	confJSON := nodeConfigJSON{
		ID:       n.ID.String(),
		Name:     n.Name,
		Services: n.Services,
	}
	if n.PrivateKey != nil {
		confJSON.PrivateKey = hex.EncodeToString(crypto.FromECDSA(n.PrivateKey))
	}
	return json.Marshal(confJSON)
}

func (n *NodeConfig) UnmarshalJSON(data []byte) error {
	var confJSON nodeConfigJSON
	if err := json.Unmarshal(data, &confJSON); err != nil {
		return err
	}

	if confJSON.ID != "" {
		nodeID, err := discover.HexID(confJSON.ID)
		if err != nil {
			return err
		}
		n.ID = nodeID
	}

	if confJSON.PrivateKey != "" {
		key, err := hex.DecodeString(confJSON.PrivateKey)
		if err != nil {
			return err
		}
		privKey, err := crypto.ToECDSA(key)
		if err != nil {
			return err
		}
		n.PrivateKey = privKey
	}

	n.Name = confJSON.Name
	n.Services = confJSON.Services

	return nil
}

func RandomNodeConfig() *NodeConfig {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic("unable to generate key")
	}
	var id discover.NodeID
	pubkey := crypto.FromECDSAPub(&key.PublicKey)
	copy(id[:], pubkey[1:])
	return &NodeConfig{
		ID:         id,
		PrivateKey: key,
	}
}

type ServiceContext struct {
	RPCDialer

	NodeContext *node.ServiceContext
	Config      *NodeConfig
	Snapshot    []byte
}

type RPCDialer interface {
	DialRPC(id discover.NodeID) (*rpc.Client, error)
}

type Services map[string]ServiceFunc

type ServiceFunc func(ctx *ServiceContext) (node.Service, error)

var serviceFuncs = make(Services)

func RegisterServices(services Services) {
	for name, f := range services {
		if _, exists := serviceFuncs[name]; exists {
			panic(fmt.Sprintf("node service already exists: %q", name))
		}
		serviceFuncs[name] = f
	}

	if reexec.Init() {
		os.Exit(0)
	}
}
