// Copyright 2018 The go-aurora Authors
// This file is part of the go-aurora library.
//
// The go-aurora library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-aurora library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-aurora library. If not, see <http://www.gnu.org/licenses/>.

// Package aoa implements the Aurora protocol.
package aoa

import (
	"fmt"
	"github.com/Aurorachain/go-aoa/accounts"
	"github.com/Aurorachain/go-aoa/aoa/downloader"
	"github.com/Aurorachain/go-aoa/aoa/filters"
	"github.com/Aurorachain/go-aoa/aoa/gasprice"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common/hexutil"
	"github.com/Aurorachain/go-aoa/consensus"
	"github.com/Aurorachain/go-aoa/consensus/dpos"
	"github.com/Aurorachain/go-aoa/core"
	"github.com/Aurorachain/go-aoa/core/bloombits"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/core/vm"
	"github.com/Aurorachain/go-aoa/internal/aoaapi"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/node"
	"github.com/Aurorachain/go-aoa/p2p"
	"github.com/Aurorachain/go-aoa/params"
	"github.com/Aurorachain/go-aoa/rlp"
	"github.com/Aurorachain/go-aoa/rpc"
	"math/big"
	"runtime"
	"sync"
)

type LesServer interface {
	Start(srvr *p2p.Server)
	Stop()
	Protocols() []p2p.Protocol
	SetBloomBitsIndexer(bbIndexer *core.ChainIndexer)
}

// Aurora implements the Aurora full node service.
type Aurora struct {
	config      *Config
	chainConfig *params.ChainConfig

	// Channel for shutting down the service
	shutdownChan  chan bool    // Channel for shutting down the aurora
	stopDbUpgrade func() error // stop chain db sequential key upgrade

	// Handlers
	txPool          *core.TxPool
	blockchain      *core.BlockChain
	protocolManager *ProtocolManager
	lesServer       LesServer

	// DB interfaces
	chainDb   aoadb.Database // Block chain database
	watcherDb aoadb.Database // database for watch internal transactions
	upgradeDb aoadb.Database // database for aoa upgrade

	aoaEngine      consensus.Engine
	accountManager *accounts.Manager

	bloomRequests chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer  *core.ChainIndexer             // Bloom indexer operating during block imports

	ApiBackend *AoaApiBackend

	gasPrice *big.Int

	networkId     uint64
	netRPCService *aoaapi.PublicNetAPI

	lock            sync.RWMutex // Protects the variadic fields (e.g. gas price )
	dposTaskManager *DposTaskManager
	dposMiner       *core.DposMiner
}

func (aurora *Aurora) AddLesServer(ls LesServer) {
	aurora.lesServer = ls
	ls.SetBloomBitsIndexer(aurora.bloomIndexer)
}

func scheduleUpgradeJob(){
	core.UpgradeTimer(core.DoUpgrade)
}

const chainDataName = "chaindata"
// New creates a new Aurora object (including the
// initialisation of the common Aurora object)
func New(ctx *node.ServiceContext, config *Config) (*Aurora, error) {
	if !config.SyncMode.IsValid() {
		return nil, fmt.Errorf("invalid sync mode %d", config.SyncMode)
	}
	chainDb, err := CreateDB(ctx, config, chainDataName)
	if err != nil {
		return nil, err
	}
	var watcherDb aoadb.Database
	if config.EnableInterTxWatching {
		watcherDb, err = CreateDB(ctx, config, "watchdata")
		if err != nil {
			return nil, err
		}
	}

	var upgradeDb aoadb.Database
	upgradeDb, err = CreateDB(ctx, config, "upgradedata")
	if err != nil {
		return nil, err
	}
	core.SetUpgradeDb(upgradeDb)

	stopDbUpgrade := upgradeDeduplicateData(chainDb)
	chainConfig, genesisHash, _, genesisErr := core.SetupGenesisBlock(chainDb, config.Genesis)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		return nil, genesisErr
	}
	log.Info("Initialised chain configuration", "config", chainConfig)

	aoa := &Aurora{
		config:         config,
		chainDb:        chainDb,
		chainConfig:    chainConfig,
		accountManager: ctx.AccountManager,
		shutdownChan:   make(chan bool),
		stopDbUpgrade:  stopDbUpgrade,
		networkId:      config.NetworkId,
		gasPrice:       config.GasPrice,
		bloomRequests:  make(chan chan *bloombits.Retrieval),
		bloomIndexer:   NewBloomIndexer(chainDb, params.BloomBitsBlocks),
		aoaEngine:      CreateAuroraConsensusEngine(),
		watcherDb:      watcherDb,
		upgradeDb:		upgradeDb,
	}

	log.Info("Initialising Aurora protocol", "versions", ProtocolVersions, "network", config.NetworkId)

	if !config.SkipBcVersionCheck {
		bcVersion := core.GetBlockChainVersion(chainDb)
		if bcVersion != core.BlockChainVersion && bcVersion != 0 {
			return nil, fmt.Errorf("Blockchain DB version mismatch (%d / %d). Run aoa upgradedb.\n", bcVersion, core.BlockChainVersion)
		}
		core.WriteBlockChainVersion(chainDb, core.BlockChainVersion)
	}

	vmConfig := vm.Config{EnablePreimageRecording: config.EnablePreimageRecording, WatchInnerTx: config.EnableInterTxWatching}
	aoa.blockchain, err = core.NewBlockChain(chainDb, aoa.chainConfig, aoa.aoaEngine, vmConfig, watcherDb)
	if err != nil {
		return nil, err
	}
	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		aoa.blockchain.SetHead(compat.RewindTo)
		core.WriteChainConfig(chainDb, genesisHash, chainConfig)
	}
	aoa.bloomIndexer.Start(aoa.blockchain)

	if config.TxPool.Journal != "" {
		config.TxPool.Journal = ctx.ResolvePath(config.TxPool.Journal)
	}

	aoa.txPool = core.NewTxPool(config.TxPool, aoa.chainConfig, aoa.blockchain)
	aoa.dposMiner = core.NewDposMiner(aoa.chainConfig, aoa, aoa.aoaEngine)
	aoa.dposTaskManager = NewDposTaskManager(ctx, aoa.blockchain, aoa.accountManager, aoa.dposMiner.GetProduceCallback(), aoa.dposMiner.GetShuffleHashChan())
	if aoa.protocolManager, err = NewProtocolManager(aoa.chainConfig, config.SyncMode, config.NetworkId, aoa.txPool, aoa.aoaEngine, aoa.blockchain, chainDb, aoa.dposTaskManager, aoa.dposMiner.GetProduceBlockChan(), aoa.dposMiner.AddDelegateWalletCallback, aoa.dposMiner.GetDelegateWallets()); err != nil {
		return nil, err
	}

	aoa.ApiBackend = &AoaApiBackend{aoa, nil}
	gpoParams := config.GPO
	if gpoParams.Default == nil {
		gpoParams.Default = config.GasPrice
	}
	aoa.ApiBackend.gpo = gasprice.NewOracle(aoa.ApiBackend, gpoParams)
	scheduleUpgradeJob()
	return aoa, nil
}

func makeExtraData(extra []byte) []byte {
	if len(extra) == 0 {
		// create default extradata
		extra, _ = rlp.EncodeToBytes([]interface{}{
			uint(params.VersionMajor<<16 | params.VersionMinor<<8 | params.VersionPatch),
			"aoa",
			runtime.Version(),
			runtime.GOOS,
		})
	}
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		log.Warn("Miner extra data exceed limit", "extra", hexutil.Bytes(extra), "limit", params.MaximumExtraDataSize)
		extra = nil
	}
	return extra
}

// CreateDB creates the chain database.
func CreateDB(ctx *node.ServiceContext, config *Config, name string) (aoadb.Database, error) {
	db, err := ctx.OpenDatabase(name, config.DatabaseCache, config.DatabaseHandles)
	if err != nil {
		return nil, err
	}
	if db, ok := db.(*aoadb.LDBDatabase); ok && chainDataName == name{
		db.Meter("aoa/db/chaindata/")
	}
	return db, nil
}

func CreateAuroraConsensusEngine() consensus.Engine {
	return dpos.New()
}

// APIs returns the collection of RPC services the aurora package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (aurora *Aurora) APIs() []rpc.API {
	apis := aoaapi.GetAPIs(aurora.ApiBackend)

	// Append any APIs exposed explicitly by the consensus engine
	apis = append(apis, aurora.aoaEngine.APIs(aurora.BlockChain())...)

	// Append all the local APIs and return
	return append(apis, []rpc.API{
		{
			Namespace: "aoa",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(aurora.protocolManager.downloader),
			Public:    true,
		}, {
			Namespace: "aoa",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(aurora.ApiBackend, false),
			Public:    true,
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   NewPrivateAdminAPI(aurora),
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPublicDebugAPI(aurora),
			Public:    true,
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPrivateDebugAPI(aurora.chainConfig, aurora),
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   aurora.netRPCService,
			Public:    true,
		},
	}...)
}

func (aurora *Aurora) ResetWithGenesisBlock(gb *types.Block) {
	aurora.blockchain.ResetWithGenesisBlock(gb)
}

func (aurora *Aurora) AccountManager() *accounts.Manager  { return aurora.accountManager }
func (aurora *Aurora) BlockChain() *core.BlockChain       { return aurora.blockchain }
func (aurora *Aurora) TxPool() *core.TxPool               { return aurora.txPool }
func (aurora *Aurora) Engine() consensus.Engine           { return aurora.aoaEngine }
func (aurora *Aurora) ChainDb() aoadb.Database            { return aurora.chainDb }
func (aurora *Aurora) WatcherDb() aoadb.Database          { return aurora.watcherDb }
func (aurora *Aurora) IsListening() bool                  { return true } // Always listening
func (aurora *Aurora) EthVersion() int                    { return int(aurora.protocolManager.SubProtocols[0].Version) }
func (aurora *Aurora) NetVersion() uint64                 { return aurora.networkId }
func (aurora *Aurora) Downloader() *downloader.Downloader { return aurora.protocolManager.downloader }

// Protocols implements node.Service, returning all the currently configured
// network protocols to start.
func (aurora *Aurora) Protocols() []p2p.Protocol {
	if aurora.lesServer == nil {
		return aurora.protocolManager.SubProtocols
	}
	return append(aurora.protocolManager.SubProtocols, aurora.lesServer.Protocols()...)
}

// Start implements node.Service, starting all internal goroutines needed by the
// Aurora protocol implementation.
func (aurora *Aurora) Start(srvr *p2p.Server) error {
	// Start the bloom bits servicing goroutines
	aurora.startBloomHandlers()

	// Start the RPC service
	aurora.netRPCService = aoaapi.NewPublicNetAPI(srvr, aurora.NetVersion())

	// Figure out a max peers count based on the server limits
	maxPeers := srvr.MaxPeers
	if aurora.config.LightServ > 0 {
		maxPeers -= aurora.config.LightPeers
		if maxPeers < srvr.MaxPeers/2 {
			maxPeers = srvr.MaxPeers / 2
		}
	}
	// Start the networking layer and the light server if requested
	aurora.protocolManager.Start(maxPeers)
	if aurora.lesServer != nil {
		aurora.lesServer.Start(srvr)
	}
	return nil
}

// Stop implements node.Service, terminating all internal goroutines used by the
// Aurora protocol.
func (aurora *Aurora) Stop() error {
	if aurora.stopDbUpgrade != nil {
		aurora.stopDbUpgrade()
	}
	aurora.bloomIndexer.Close()
	aurora.blockchain.Stop()
	aurora.protocolManager.Stop()
	if aurora.lesServer != nil {
		aurora.lesServer.Stop()
	}
	aurora.txPool.Stop()
	aurora.chainDb.Close()
	if aurora.watcherDb != nil {
		aurora.watcherDb.Close()
	}
	if aurora.upgradeDb != nil {
		aurora.upgradeDb.Close()
	}
	close(aurora.shutdownChan)

	return nil
}
