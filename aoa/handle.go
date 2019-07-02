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

package aoa

import (
	"errors"
	"fmt"
	"github.com/Aurorachain/go-aoa/aoa/downloader"
	"github.com/Aurorachain/go-aoa/aoa/fetcher"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/consensus"
	"github.com/Aurorachain/go-aoa/core"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/event"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/p2p"
	"github.com/Aurorachain/go-aoa/p2p/discover"
	"github.com/Aurorachain/go-aoa/params"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"crypto/ecdsa"
	aa "github.com/Aurorachain/go-aoa/accounts/walletType"
	"github.com/Aurorachain/go-aoa/crypto"
	"math"
	"sort"
	"strings"
)

const (
	softResponseLimit = 2 * 1024 * 1024 // Target maximum size of returned blocks, headers or node data.
	estHeaderRlpSize  = 500             // Approximate size of an RLP encoded block header

	// txChanSize is the size of channel listening to TxPreEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 4096
)

// errIncompatibleConfig is returned if the requested protocols and configs are
// not compatible (low protocol version restrictions and high requirements).
var errIncompatibleConfig = errors.New("incompatible configuration")

func errResp(code errCode, format string, v ...interface{}) error {
	return fmt.Errorf("%v - %v", code, fmt.Sprintf(format, v...))
}

type ProtocolManager struct {
	networkId     uint64
	fastSync      uint32 // Flag whether fast sync is enabled (gets disabled if we already have blocks)
	acceptTxs     uint32 // Flag whether we're considered synchronised (enables transaction processing)
	txpool        txPool
	blockchain    *core.BlockChain
	chaindb       aoadb.Database
	chainconfig   *params.ChainConfig
	maxPeers      int
	peers         *peerSet
	delegatePeers *peerSet

	SubProtocols []p2p.Protocol

	// channels for fetcher, syncer, txsyncLoop
	newPeerCh   chan *peer
	txsyncCh    chan *txsync
	quitSync    chan struct{}
	noMorePeers chan struct{}

	// wait group is used for graceful shutdowns during downloading
	// and processing
	wg sync.WaitGroup

	downloader *downloader.Downloader
	fetcher    *fetcher.Fetcher

	txCh                      chan core.TxPreEvent
	txSub                     event.Subscription
	taskManager               *DposTaskManager
	blockChan                 chan *types.Block
	lockBlockManager          *lockManager
	engine                    consensus.Engine
	addDelegateWalletCallback func(data *aa.DelegateWalletInfo)
	delegateWallets           map[string]*ecdsa.PrivateKey
}

// NewProtocolManager returns a new aurora sub protocol manager. The Aurora sub protocol manages peers capable
// with the aurora network.
func NewProtocolManager(config *params.ChainConfig, mode downloader.SyncMode, networkId uint64, txpool txPool, engine consensus.Engine, blockchain *core.BlockChain, chaindb aoadb.Database, taskManager *DposTaskManager, blockChan chan *types.Block, addDelegateWalletCallback func(data *aa.DelegateWalletInfo), delegateWallets map[string]*ecdsa.PrivateKey) (*ProtocolManager, error) {
	// Create the protocol manager with the base fields

	manager := &ProtocolManager{
		networkId:                 networkId,
		txpool:                    txpool,
		blockchain:                blockchain,
		chaindb:                   chaindb,
		chainconfig:               config,
		peers:                     newPeerSet(),
		delegatePeers:             newPeerSet(),
		newPeerCh:                 make(chan *peer),
		noMorePeers:               make(chan struct{}),
		txsyncCh:                  make(chan *txsync),
		quitSync:                  make(chan struct{}),
		taskManager:               taskManager,
		engine:                    engine,
		blockChan:                 blockChan,
		addDelegateWalletCallback: addDelegateWalletCallback,
		delegateWallets:           delegateWallets,
	}

	// Figure out whether to allow fast sync or not
	if mode == downloader.FastSync && blockchain.CurrentBlock().NumberU64() > 0 {
		log.Warn("Blockchain not empty, fast sync disabled")
		mode = downloader.FullSync
	}
	if mode == downloader.FastSync {
		manager.fastSync = uint32(1)
	}
	// Initiate a sub-protocol for every implemented version we can handle
	manager.SubProtocols = make([]p2p.Protocol, 0, len(ProtocolVersions))
	for i, version := range ProtocolVersions {
		// Skip protocol version if incompatible with the mode of operation
		if mode == downloader.FastSync && version < aoa02 {
			continue
		}
		// Compatible; initialise the sub-protocol
		version := version // Closure for the runxw
		manager.SubProtocols = append(manager.SubProtocols, p2p.Protocol{
			Name:    ProtocolName,
			Version: version,
			Length:  ProtocolLengths[i],
			Run: func(p *p2p.Peer, rw p2p.MsgReadWriter, netType byte) error {
				peer := manager.newPeer(int(version), p, rw)
				select {
				case manager.newPeerCh <- peer:
					manager.wg.Add(1)
					defer manager.wg.Done()
					return manager.handle(peer)
				case <-manager.quitSync:
					return p2p.DiscQuitting
				}
			},
			NodeInfo: func() interface{} {
				return manager.NodeInfo()
			},
			PeerInfo: func(id discover.NodeID) interface{} {
				if p := manager.peers.Peer(fmt.Sprintf("%x", id[:8])); p != nil {
					return p.Info()
				}
				return nil
			},
		})
	}
	if len(manager.SubProtocols) == 0 {
		return nil, errIncompatibleConfig
	}
	// Construct the different synchronisation mechanisms
	manager.downloader = downloader.New(mode, chaindb, blockchain, nil, manager.removePeer)

	//validator := func(block *types.Block) error {
	//	return manager.blockchain.PreInsertChain(block)
	//}
	validator := func(block *types.Block) error {
		err := manager.engine.VerifyHeaderAndSign(manager.blockchain, block, manager.taskManager.GetCurrentShuffleRound(), blockInterval)
		if err != nil {
			manager.shuffleIfVerify(block)
			return manager.engine.VerifyHeaderAndSign(manager.blockchain, block, manager.taskManager.GetCurrentShuffleRound(), blockInterval)
		}
		return nil
	}

	heighter := func() uint64 {
		return blockchain.CurrentBlock().NumberU64()
	}

	insertBlockfunc := func(blocks types.Blocks) (int, error) {
		// If fast sync is running, deny importing weird blocks
		if atomic.LoadUint32(&manager.fastSync) == 1 {
			log.Warn("Discarded bad propagated block", "number", blocks[0].Number(), "hash", blocks[0].Hash())
			return 0, nil
		}
		atomic.StoreUint32(&manager.acceptTxs, 1) // Mark initial sync done on any fetcher import
		return manager.blockchain.InsertChain(blocks)
	}

	manager.fetcher = fetcher.New(blockchain.GetBlockByHash, validator, manager.BroadcastBlock, heighter, insertBlockfunc, manager.removePeer)
	manager.lockBlockManager = newBlockLockManager(insertBlockfunc, delegateWallets)
	return manager, nil
}

// handle is the callback invoked to manage the life cycle of an aoa peer. When
// this function terminates, the peer is disconnected.
func (pm *ProtocolManager) handle(p *peer) error {
	if pm.peers.Len() >= pm.maxPeers {
		return p2p.DiscTooManyPeers
	}
	log.Infof("Aurora peer connected, name=%v", p.Name())

	// Execute the Aurora handshake
	td, head, genesis := pm.blockchain.Status()
	if err := p.Handshake(pm.networkId, td, head, genesis); err != nil {
		log.Infof("Aurora handshake failed, %v", err)
		return err
	}
	if rw, ok := p.rw.(*meteredMsgReadWriter); ok {
		rw.Init(p.version)
	}
	// Register the peer locally
	if err := pm.peers.Register(p); err != nil {
		log.Errorf("Aurora peer registration failed, %v", err)
		return err
	}

	// delegate p2p network
	if p.netType == discover.ConsNet && pm.delegatePeers.Peer(p.id) == nil {
		if err := pm.delegatePeers.Register(p); err != nil {
			log.Error("Aurora delegate peer registration failed", "err", err)
			return err
		}
	}

	defer pm.removePeer(p.id)

	// Register the peer in the downloader. If the downloader considers it banned, we disconnect
	if err := pm.downloader.RegisterPeer(p.id, p.version, p); err != nil {
		log.Info("downloader register peer", "err", err)
		return err
	}
	// Propagate existing transactions. new transactions appearing
	// after this will be sent via broadcasts.
	pm.syncTransactions(p)

	// main loop. handle incoming messages.
	for {
		log.Infof("receive message from peer, ID=%v, LocalAddr=%v, remoteAddr=%v", p.id, p.Peer.LocalAddr().String(), p.Peer.RemoteAddr().String())
		if err := pm.handleMsg(p); err != nil {
			log.Infof("Aurora message handling failed, %v", err)
			return err
		}
	}
}

// handleMsg is invoked whenever an inbound message is received from a remote
// peer. The remote connection is torn down upon returning any error.
func (pm *ProtocolManager) handleMsg(p *peer) error {
	// Read the next message from the remote peer, and ensure it's fully consumed
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Size > ProtocolMaxMsgSize {
		return errResp(ErrMsgTooLarge, "%v > %v", msg.Size, ProtocolMaxMsgSize)
	}
	defer msg.Discard()

	// Handle the message depending on its contents
	switch {
	case msg.Code == StatusMsg:
		return pm.dealStatusMsg(msg, p)
	case msg.Code == GetBlockHeadersMsg:
		return pm.dealGetBlockHeadersMsg(msg, p)
	case msg.Code == BlockHeadersMsg:
		return pm.dealBlockHeadersMsg(msg, p)
	case msg.Code == GetBlockBodiesMsg:
		return pm.dealGetBlockBodiesMsg(msg, p)
	case msg.Code == BlockBodiesMsg:
		return pm.dealBlockBodiesMsg(msg, p)
	case p.version >= aoa02 && msg.Code == GetNodeDataMsg:
		return pm.dealGetNodeDataMsg(msg, p)
	case p.version >= aoa02 && msg.Code == NodeDataMsg:
		return pm.dealNodeDataMsg(msg, p)
	case p.version >= aoa02 && msg.Code == GetReceiptsMsg:
		return pm.dealGetReceiptsMsg(msg, p)
	case p.version >= aoa02 && msg.Code == ReceiptsMsg:
		return pm.dealReceiptsMsg(msg, p)
	case msg.Code == NewBlockHashesMsg:
		return pm.dealNewBlockHashesMsg(msg, p)
	case msg.Code == NewBlockMsg:
		return pm.dealNewBlockMsg(msg, p)
	case msg.Code == TxMsg:
		return pm.dealTxMsg(msg, p)
	case msg.Code == PreBlockMsg:
		return pm.dealPreBlockMsg(msg, p)
	case msg.Code == SignaturesBlockMsg:
		return pm.dealSignaturesBlockMsg(msg, p)
	default:
		return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	}
	return nil
}

func (pm *ProtocolManager) newPeer(pv int, p *p2p.Peer, rw p2p.MsgReadWriter) *peer {
	return newPeer(pv, p, newMeteredMsgWriter(rw))
}

func (pm *ProtocolManager) removePeer(id string) {
	// Short circuit if the peer was already removed
	peer := pm.peers.Peer(id)
	if peer == nil {
		return
	}
	log.Info("Removing Aurora peer", "peer", id)

	// Unregister the peer from the downloader and Aurora peer set
	pm.downloader.UnregisterPeer(id)
	if err := pm.peers.Unregister(id); err != nil {
		log.Error("Peer removal failed", "peer", id, "err", err)
	}
	// Hard disconnect at the networking layer
	if peer != nil {
		peer.Peer.Disconnect(p2p.DiscUselessPeer)
	}
	delegatePeer := pm.delegatePeers.Peer(id)
	if delegatePeer == nil {
		return
	}
	if err := pm.delegatePeers.Unregister(id); err != nil {
		log.Error("delegate Peer removal failed", "peer", id, "err", err)
	}
}

//
func (pm *ProtocolManager) localProduceBlockLoop() {

	for {
		select {
		case block := <-pm.blockChan:
			log.Info("dpos|preBroadcast block", "blockNumber", block.NumberU64(), "coinbase", block.Coinbase().Hex())
			pm.lockBlockManager.unlock()
			err := pm.lockBlockManager.addPendingBlock(block)
			if err == nil {
				log.Info("localProduceBlockLoop|preBlockMsg start", "blockHash", block.Hash().Hex())
				go pm.PreBroadcastBlock(block)
				go pm.addSignToPendingBlock(block, approveVote)
			} else {
				log.Error("dpos|localProduceBlockLoop|fail", "err", err)
			}
		}
	}
}

func (pm *ProtocolManager) broadcastBlockOrSignaturesLoop() {
	for {
		select {
		case block := <-pm.lockBlockManager.broadcastBlockChan:
			pm.BroadcastBlock(block, true)  // first propagete block to peers
			pm.BroadcastBlock(block, false) // only then announce to the rest
		case signaturesMsg := <-pm.lockBlockManager.signaturesChan:
			pm.BroadcastBlockSignatures(signaturesMsg.BlockHash, signaturesMsg.Signatures)
		}

	}
}

func (pm *ProtocolManager) addSignToPendingBlock(block *types.Block, action uint64) {
	localExistDelegates := pm.taskManager.GetLocalCurrentRound()
	currentShuffleRound := pm.taskManager.GetCurrentShuffleRound()
	startTime := time.Now().Nanosecond()
	log.Info("ProtocolManager|addSignToPendingBlock|start", "blockNumber", block.NumberU64(), "blockHash", block.Hash().Hex(), "localDelegatesCount", len(localExistDelegates))
	if len(localExistDelegates) == 0 {
		log.Info("protocolManager|addSignToPendingBlock|end|local have no delegates", "blockNumber", block.NumberU64(), "cost time(ns)", time.Now().Nanosecond()-startTime)
		return
	}
	if len(localExistDelegates) == 1 {
		pm.singleSignToPendingBlock(block, action, localExistDelegates, currentShuffleRound)
		log.Info("protocolManager|addSignToPendingBlock|end", "blockNumber", block.NumberU64(), "cost time(ns)", time.Now().Nanosecond()-startTime)
		return
	}
	pm.signsToPendingBlock(block, action, localExistDelegates, currentShuffleRound)
	log.Info("protocolManager|addSignToPendingBlock|end", "blockNumber", block.NumberU64(), "cost time(ns)", time.Now().Nanosecond()-startTime)
}

// only one delegate in local
func (pm *ProtocolManager) singleSignToPendingBlock(block *types.Block, action uint64, localExistDelegates []string, currentShuffleRound *types.ShuffleList) {
	signVotes := make([]types.VoteSign, 0, 1)
	address := strings.ToLower(localExistDelegates[0])
	if _, ok := pm.delegateWallets[address]; !ok {
		return
	}
	privateKey := pm.delegateWallets[address]
	sign, err := crypto.Sign(block.Hash().Bytes()[:32], privateKey)
	if err != nil {
		return
	}
	signVotes = append(signVotes, types.VoteSign{Sign: sign, VoteAction: action})
	tempErr := pm.lockBlockManager.addSignToPendingBlock(block.Hash(), sign, action, currentShuffleRound)
	if tempErr != nil {
		log.Info("ProtocolManager|addSignToPendingBlock fail", "err", tempErr)
	}
	if len(signVotes) > 1 {
		sort.Sort(voteSignSlice(signVotes))
	}
	go pm.BroadcastBlockSignatures(block.Hash().Bytes(), signVotes)
}

func (pm *ProtocolManager) signsToPendingBlock(block *types.Block, action uint64, localExistDelegates []string, currentShuffleRound *types.ShuffleList) {
	signCount := int64(len(localExistDelegates))
	voteSignChan := make(chan types.VoteSign, signCount)
	signVotes := make([]types.VoteSign, 0, signCount)

	var waitGroup sync.WaitGroup
	waitGroup.Add(len(localExistDelegates))
	for _, address := range localExistDelegates {
		go func(address string) {
			// signVotes := make([]VoteSign, 0, 1)
			defer waitGroup.Done()
			address = strings.ToLower(address)
			if _, ok := pm.delegateWallets[address]; !ok {
				atomic.AddInt64(&signCount, -1)
				return
			}
			privateKey := pm.delegateWallets[address]
			sign, err := crypto.Sign(block.Hash().Bytes()[:32], privateKey)
			if err != nil {
				atomic.AddInt64(&signCount, -1)
				return
			}
			voteSignChan <- types.VoteSign{Sign: sign, VoteAction: action}
			tempErr := pm.lockBlockManager.addSignToPendingBlock(block.Hash(), sign, action, currentShuffleRound)
			if tempErr != nil {
				log.Info("ProtocolManager|addSignToPendingBlock fail", "err", tempErr)
			}
		}(address)
	}

	waitGroup.Wait()
	close(voteSignChan)
	for v := range voteSignChan {
		if &v != nil && v.Sign != nil && len(v.Sign) > 0 {
			signVotes = append(signVotes, v)
		}
	}
	if len(signVotes) > 1 {
		sort.Sort(voteSignSlice(signVotes))
	}
	go pm.BroadcastBlockSignatures(block.Hash().Bytes(), signVotes)
}

// get all top net delegates who not know block hash , get  some common net
func (pm *ProtocolManager) getTopPeersFromCommonPeers(hash common.Hash) []*peer {
	ps := pm.peers
	delegatePeers := pm.delegatePeers
	ps.lock.RLock()
	defer ps.lock.RUnlock()
	delegatePeers.lock.RLock()
	defer delegatePeers.lock.RUnlock()

	delegateList := make([]*peer, 0, len(delegatePeers.peers))
	for _, p := range delegatePeers.peers {
		if !p.knownBlocks.Has(hash) {
			delegateList = append(delegateList, p)
		}
	}

	list := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.knownBlocks.Has(hash) && delegatePeers.peers[p.id] == nil {
			list = append(list, p)
		}
	}
	transfer := list[:int(math.Sqrt(float64(len(list))))]
	delegateList = append(delegateList, transfer...)
	return delegateList
}

// BroadcastBlock will either propagate a block to a subset of it's peers, or
// will only announce it's availability (depending what's requested).
func (pm *ProtocolManager) BroadcastBlock(block *types.Block, propagate bool) {
	hash := block.Hash()
	// 获取到不知道该块到节点列表
	// peers := pm.peers.PeersWithoutBlock(hash)
	peers := pm.getTopPeersFromCommonPeers(hash)
	// Add log
	log.Infof("broadcastNewBlockMsg start, blockNumber=%v, unknownPeersNumber=%v", block.NumberU64(), len(peers))

	// If propagation is requested, send to a subset of the peer
	if propagate {
		// Calculate the TD of the block (it's not imported yet, so block.Td is not valid)
		var td *big.Int
		if parent := pm.blockchain.GetBlock(block.ParentHash(), block.NumberU64()-1); parent != nil {
			td = new(big.Int).Add(types.BlockDifficult, pm.blockchain.GetTd(block.ParentHash(), block.NumberU64()-1))
		} else {
			log.Errorf("Propagating dangling block, number=%v, hash=%v", block.Number(), hash)
			return
		}
		// Send the block to a subset of our peers
		// transfer := peers[:int(math.Sqrt(float64(len(peers))))]
		for _, peer := range peers {
			peer.SendNewBlock(block, td)
		}
		log.Debugf("Propagated block, hash=%v, recipients=%v, duration=%v", hash, len(peers), common.PrettyDuration(time.Since(block.ReceivedAt)))
		return
	}
	// Otherwise if the block is indeed in out own chain, announce it
	if pm.blockchain.HasBlock(hash, block.NumberU64()) {
		for _, peer := range peers {
			peer.SendNewBlockHashes([]common.Hash{hash}, []uint64{block.NumberU64()})
		}
		log.Debugf("Announced block, hash=%v, recipients=%v, duration=%v", hash, len(peers), common.PrettyDuration(time.Since(block.ReceivedAt)))
	}
	// Add log
	log.Infof("broadcastNewBlockMsg end, blockInfo=%v", block.NumberU64())
}

// broadcast to agent p2p network
func (pm *ProtocolManager) PreBroadcastBlock(block *types.Block) {
	hash := block.Hash()
	peers := pm.delegatePeers.PeersWithoutPreBlock(hash)
	// transfer := peers[:int(math.Sqrt(float64(len(peers))))]
	log.Infof("PreBroadcastBlock|start, blockNumber=%v, blockHash=%v, unKnownPeer=%v, topPeerCount=%v", block.NumberU64(), block.Hash().Hex(), len(peers), len(pm.delegatePeers.peers))
	for _, peer := range peers {
		peer.SendNewPreBlock(block)
	}
}

func (pm *ProtocolManager) BroadcastBlockSignatures(blockHash []byte, signs []types.VoteSign) {
	peers := pm.delegatePeers.PeersWithoutSignatures(signs, blockHash)
	// transfer := peers[:int(math.Sqrt(float64(len(peers))))]
	log.Infof("BroadcastBlockSignatures|start, blockHash=%v, unKnownPeer=%v, topPeerCount=%v", common.BytesToHash(blockHash).Hex(), len(peers), len(pm.delegatePeers.peers))
	for _, peer := range peers {
		peer.SendSignaturesBlockMsg(signaturesBlockMsg{BlockHash: blockHash, Signatures: signs})
	}

}

// Broadcast commit block
func (pm *ProtocolManager) BroadcastCommitBlock(block types.CommitBlock) {
	peers := pm.peers.PeersWithBlock(block.BlockHash)
	// if the block is indeed in out own chain, announce it
	if pm.blockchain.HasBlock(block.BlockHash, block.BlockNumber) {
		log.Infof("broadcast commit block, hash=%v, peerCount=%v", block.BlockHash.Hex(), len(peers))
		for _, peer := range peers {
			peer.SendNewBlockHashWithSigns(block)
		}
	}
}

func (pm *ProtocolManager) Start(maxPeers int) {
	pm.maxPeers = maxPeers

	// broadcast transactions
	pm.txCh = make(chan core.TxPreEvent, txChanSize)
	pm.txSub = pm.txpool.SubscribeTxPreEvent(pm.txCh)
	go pm.txBroadcastLoop()

	// broadcast mined blocks
	go pm.localProduceBlockLoop()
	go pm.broadcastBlockOrSignaturesLoop()

	// start sync handlers
	go pm.syncer()
	go pm.txsyncLoop()
}

func (pm *ProtocolManager) Stop() {
	log.Info("Stopping Aurorachain protocol")

	pm.txSub.Unsubscribe() // quits txBroadcastLoop
	// Quit the sync loop.
	// After this send has completed, no new peers will be accepted.
	pm.noMorePeers <- struct{}{}

	// Quit fetcher, txsyncLoop.
	close(pm.quitSync)

	// Disconnect existing sessions.
	// This also closes the gate for any new registrations on the peer set.
	// sessions which are already established but not added to pm.peers yet
	// will exit when they try to register.
	pm.peers.Close()
	pm.delegatePeers.Close()
	// Wait for all peer handler goroutines and the loops to come down.
	pm.wg.Wait()

	log.Info("Aurorachain protocol stopped")
}

func (pm *ProtocolManager) txBroadcastLoop() {
	for {
		select {
		case txPreEvent := <-pm.txCh:
			// Add log
			pm.BroadcastTx(txPreEvent.Tx.Hash(), txPreEvent.Tx)

			// Err() channel will be closed when unsubscribing.
		case <-pm.txSub.Err():
			return
		}
	}
}

// BroadcastTx will propagate a transaction to all peers which are not known to
// already have the given transaction.
func (pm *ProtocolManager) BroadcastTx(hash common.Hash, tx *types.Transaction) {
	// Broadcast transaction to a batch of peers not knowing about it
	// 获取到不知道该笔交易到节点列表
	peers := pm.peers.PeersWithoutTx(hash)
	//FIXME include this again: peers = peers[:int(math.Sqrt(float64(len(peers))))]
	for _, peer := range peers {
		peer.SendTransactions(types.Transactions{tx})
	}
	log.Debugf("Broadcast transaction, hash=%v, recipients=%v", hash, len(peers))
}

func (pm *ProtocolManager) GetAddDelegateWalletCallback() func(data *aa.DelegateWalletInfo) {
	return pm.addDelegateWalletCallback
}

type NodeInfo struct {
	Network uint64              `json:"network"` // Aurorachain network ID (1=Frontier)
	Genesis common.Hash         `json:"genesis"` // SHA3 hash of the host's genesis block
	Config  *params.ChainConfig `json:"config"`
	Head    common.Hash         `json:"head"` // SHA3 hash of the host's best owned block
}

// NodeInfo retrieves some protocol metadata about the running host node.
func (pm *ProtocolManager) NodeInfo() *NodeInfo {
	currentBlock := pm.blockchain.CurrentBlock()
	return &NodeInfo{
		Network: pm.networkId,
		Genesis: pm.blockchain.Genesis().Hash(),
		Config:  pm.blockchain.Config(),
		Head:    currentBlock.Hash(),
	}
}
