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
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/p2p"
	"github.com/Aurorachain/go-aoa/rlp"
	"gopkg.in/fatih/set.v0"
	"math/big"
	"sync"
	"time"
)

var (
	errClosed            = errors.New("peer set is closed")
	errAlreadyRegistered = errors.New("peer is already registered")
	errNotRegistered     = errors.New("peer is not registered")
)

const (
	maxKnownTxs        = 32768 // Maximum transactions hashes to keep in the known list (prevent DOS)
	maxKnownBlocks     = 1024  // Maximum block hashes to keep in the known list (prevent DOS)
	maxKnownPrepare    = 32768
	maxKnownSignatures = 32768
	maxKnownPreBlocks  = 1024
	handshakeTimeout   = 5 * time.Second
)

// PeerInfo represents a short summary of the Aurora sub-protocol metadata known
// about a connected peer.
type PeerInfo struct {
	Version    int      `json:"version"` // Aurora protocol version negotiated
	difficulty *big.Int // `json:"difficulty"` // Total difficulty of the peer's blockchain
	Head       string   `json:"head"` // SHA3 hash of the peer's best owned block
}

type peer struct {
	id string

	*p2p.Peer
	rw p2p.MsgReadWriter

	version  int         // Protocol version negotiated
	forkDrop *time.Timer // Timed connection dropper if forks aren't validated in time

	head common.Hash
	td   *big.Int
	lock sync.RWMutex

	knownTxs *set.Set // Set of transaction hashes known to be known by this peer

	knownBlocks *set.Set // Set of block hashes known to be known by this peer

	knownPreBlocks *set.Set // Set of block hashes known to be known by this peer only when pre

	knownSignatures *set.Set

	KnownPrepare *set.Set

	netType byte
}

func newPeer(version int, p *p2p.Peer, rw p2p.MsgReadWriter) *peer {
	id := p.ID()

	return &peer{
		Peer:            p,
		rw:              rw,
		version:         version,
		id:              fmt.Sprintf("%x", id[:8]),
		knownTxs:        set.New(),
		knownBlocks:     set.New(),
		KnownPrepare:    set.New(),
		knownPreBlocks:  set.New(),
		knownSignatures: set.New(),
		netType:         p.GetNetType(),
	}
}

// Info gathers and returns a collection of metadata known about a peer.
func (p *peer) Info() *PeerInfo {
	hash, _ := p.Head()

	return &PeerInfo{
		Version: p.version,
		// difficulty: td,
		Head: hash.Hex(),
	}
}

// Head retrieves a copy of the current head hash and total difficulty of the
// peer.
func (p *peer) Head() (hash common.Hash, td *big.Int) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	copy(hash[:], p.head[:])
	return hash, new(big.Int).Set(p.td)
}

// SetHead updates the head hash and total difficulty of the peer.
func (p *peer) SetHead(hash common.Hash, td *big.Int) {
	p.lock.Lock()
	defer p.lock.Unlock()

	copy(p.head[:], hash[:])
	p.td.Set(td)
}

// MarkBlock marks a block as known for the peer, ensuring that the block will
// never be propagated to this particular peer.
func (p *peer) MarkBlock(hash common.Hash) {
	// If we reached the memory allowance, drop a previously known block hash
	for p.knownBlocks.Size() >= maxKnownBlocks {
		p.knownBlocks.Pop()
	}
	p.knownBlocks.Add(hash)
}

// MarkBlock marks a block as known for the peer, ensuring that the block will
// never be propagated to this particular peer.
func (p *peer) Markprepare(sign []byte) {
	// If we reached the memory allowance, drop a previously known block hash
	for p.KnownPrepare.Size() >= maxKnownPrepare {
		p.KnownPrepare.Pop()
	}
	p.KnownPrepare.Add(common.ToHex(sign))
}

// mark signs ,ensuring that the block will never be propagated to this particular peer.
func (p *peer) MarkBlockSignatures(signs []types.VoteSign, blockHash []byte) {
	for p.knownSignatures.Size() >= maxKnownSignatures {
		p.knownSignatures.Pop()
	}
	var b []byte
	b = append(b, blockHash...)
	for _, v := range signs {
		var temp []byte
		temp = append(temp, v.Sign...)
		temp = append(temp, byte(v.VoteAction))
		b = append(b, temp...)
	}
	signHex := common.ToHex(b)
	p.knownSignatures.Add(signHex)
}

func (p *peer) HasReceiveSignatures(signs []types.VoteSign, blockHash []byte) bool {
	var b []byte
	b = append(b, blockHash...)
	for _, v := range signs {
		var temp []byte
		temp = append(temp, v.Sign...)
		temp = append(temp, byte(v.VoteAction))
		b = append(b, temp...)
	}
	signHex := common.ToHex(b)
	return p.knownSignatures.Has(signHex)
}

func (p *peer) MarkPreBlock(hash common.Hash) {
	for p.knownPreBlocks.Size() >= maxKnownPreBlocks {
		p.knownPreBlocks.Pop()
	}
	p.knownPreBlocks.Add(hash)
}

// MarkTransaction marks a transaction as known for the peer, ensuring that it
// will never be propagated to this particular peer.
func (p *peer) MarkTransaction(hash common.Hash) {
	// If we reached the memory allowance, drop a previously known transaction hash
	for p.knownTxs.Size() >= maxKnownTxs {
		p.knownTxs.Pop()
	}
	p.knownTxs.Add(hash)
}

// SendTransactions sends transactions to the peer and includes the hashes
// in its transaction hash set for future reference.
func (p *peer) SendTransactions(txs types.Transactions) error {
	for _, tx := range txs {
		p.knownTxs.Add(tx.Hash())
	}
	return p2p.Send(p.rw, TxMsg, txs)
}

// SendNewBlockHashes announces the availability of a number of blocks through
// a hash notification.
func (p *peer) SendNewBlockHashes(hashes []common.Hash, numbers []uint64) error {
	for _, hash := range hashes {
		p.knownBlocks.Add(hash)
	}
	request := make(newBlockHashesData, len(hashes))
	for i := 0; i < len(hashes); i++ {
		request[i].Hash = hashes[i]
		request[i].Number = numbers[i]
	}
	return p2p.Send(p.rw, NewBlockHashesMsg, request)
}

// pbft broadcast
// SendNewBlockHashWithSigns announces the availability of a number of block through
// a hash notification
func (p *peer) SendNewBlockHashWithSigns(block types.CommitBlock) error {
	p.knownBlocks.Add(block.BlockHash)
	return p2p.Send(p.rw, NewBlockMsg, block)
}

func (p *peer) SendSignaturesBlockMsg(signs signaturesBlockMsg) error {
	p.MarkBlockSignatures(signs.Signatures, signs.BlockHash)
	return p2p.Send(p.rw, SignaturesBlockMsg, signs)
}

// SendNewBlock propagates an entire block to a remote peer.
func (p *peer) SendNewBlock(block *types.Block, td *big.Int) error {
	p.knownBlocks.Add(block.Hash())
	return p2p.Send(p.rw, NewBlockMsg, []interface{}{block, td, block.RlpEncodeSigns})
}

func (p *peer) SendNewPreBlock(block *types.Block) error {
	p.knownPreBlocks.Add(block.Hash())
	return p2p.Send(p.rw, PreBlockMsg, []interface{}{block})
}

// SendBlockHeaders sends a batch of block headers to the remote peer.
func (p *peer) SendBlockHeaders(headers []*types.Header) error {
	return p2p.Send(p.rw, BlockHeadersMsg, headers)
}

// SendBlockBodies sends a batch of block contents to the remote peer.
func (p *peer) SendBlockBodies(bodies []*blockBody) error {
	return p2p.Send(p.rw, BlockBodiesMsg, blockBodiesData(bodies))
}

// SendBlockBodiesRLP sends a batch of block contents to the remote peer from
// an already RLP encoded format.
func (p *peer) SendBlockBodiesRLP(bodies []rlp.RawValue) error {
	return p2p.Send(p.rw, BlockBodiesMsg, bodies)
}

// SendNodeDataRLP sends a batch of arbitrary internal data, corresponding to the
// hashes requested.
func (p *peer) SendNodeData(data [][]byte) error {
	return p2p.Send(p.rw, NodeDataMsg, data)
}

// SendReceiptsRLP sends a batch of transaction receipts, corresponding to the
// ones requested from an already RLP encoded format.
func (p *peer) SendReceiptsRLP(receipts []rlp.RawValue) error {
	return p2p.Send(p.rw, ReceiptsMsg, receipts)
}

// RequestOneHeader is a wrapper around the header query functions to fetch a
// single header. It is used solely by the fetcher.
func (p *peer) RequestOneHeader(hash common.Hash) error {
	log.Info("Fetching single header", "hash", hash)
	return p2p.Send(p.rw, GetBlockHeadersMsg, &getBlockHeadersData{Origin: hashOrNumber{Hash: hash}, Amount: uint64(1), Skip: uint64(0), Reverse: false})
}

// RequestHeadersByHash fetches a batch of blocks' headers corresponding to the
// specified header query, based on the hash of an origin block.
func (p *peer) RequestHeadersByHash(origin common.Hash, amount int, skip int, reverse bool) error {
	log.Infof("Fetching batch of headers, count=%v, fromhash=%v, origin=%v, skip=%v, reverse=%v", amount, origin.String(), skip, reverse)
	return p2p.Send(p.rw, GetBlockHeadersMsg, &getBlockHeadersData{Origin: hashOrNumber{Hash: origin}, Amount: uint64(amount), Skip: uint64(skip), Reverse: reverse})
}

// RequestHeadersByNumber fetches a batch of blocks' headers corresponding to the
// specified header query, based on the number of an origin block.
func (p *peer) RequestHeadersByNumber(origin uint64, amount int, skip int, reverse bool) error {
	log.Infof("Fetching batch of headers, count=%v, fromNum=%v, skip=%v, reverse=%v", amount, origin, skip, reverse)
	return p2p.Send(p.rw, GetBlockHeadersMsg, &getBlockHeadersData{Origin: hashOrNumber{Number: origin}, Amount: uint64(amount), Skip: uint64(skip), Reverse: reverse})
}

// RequestBodies fetches a batch of blocks' bodies corresponding to the hashes
// specified.
func (p *peer) RequestBodies(hashes []common.Hash) error {
	log.Infof("Fetching batch of block bodies, count=%v", len(hashes))
	return p2p.Send(p.rw, GetBlockBodiesMsg, hashes)
}

// RequestNodeData fetches a batch of arbitrary data from a node's known state
// data, corresponding to the specified hashes.
func (p *peer) RequestNodeData(hashes []common.Hash) error {
	log.Infof("Fetching batch of state data, count=%v", len(hashes))
	return p2p.Send(p.rw, GetNodeDataMsg, hashes)
}

// RequestReceipts fetches a batch of transaction receipts from a remote node.
func (p *peer) RequestReceipts(hashes []common.Hash) error {
	log.Infof("Fetching batch of receipts, count=%v", len(hashes))
	return p2p.Send(p.rw, GetReceiptsMsg, hashes)
}

// Handshake executes the aoa protocol handshake, negotiating version number,
// network IDs, difficulties, head and genesis blocks.
func (p *peer) Handshake(network uint64, td *big.Int, head common.Hash, genesis common.Hash) error {
	// Send out own handshake in a new thread
	errc := make(chan error, 2)
	var status statusData // safe to read after two values have been received from errc

	go func() {
		errc <- p2p.Send(p.rw, StatusMsg, &statusData{
			ProtocolVersion: uint32(p.version),
			NetworkId:       network,
			TD:              td,
			CurrentBlock:    head,
			GenesisBlock:    genesis,
		})
	}()
	go func() {
		errc <- p.readStatus(network, &status, genesis)
	}()
	timeout := time.NewTimer(handshakeTimeout)
	defer timeout.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				return err
			}
		case <-timeout.C:
			return p2p.DiscReadTimeout
		}
	}
	p.td, p.head = status.TD, status.CurrentBlock
	return nil
}

func (p *peer) readStatus(network uint64, status *statusData, genesis common.Hash) (err error) {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != StatusMsg {
		return errResp(ErrNoStatusMsg, "first msg has code %x (!= %x)", msg.Code, StatusMsg)
	}
	if msg.Size > ProtocolMaxMsgSize {
		return errResp(ErrMsgTooLarge, "%v > %v", msg.Size, ProtocolMaxMsgSize)
	}
	// Decode the handshake and make sure everything matches
	if err := msg.Decode(&status); err != nil {
		return errResp(ErrDecode, "msg %v: %v", msg, err)
	}
	if status.GenesisBlock != genesis {
		return errResp(ErrGenesisBlockMismatch, "%x (!= %x)", status.GenesisBlock[:8], genesis[:8])
	}
	if status.NetworkId != network {
		return errResp(ErrNetworkIdMismatch, "%d (!= %d)", status.NetworkId, network)
	}
	if int(status.ProtocolVersion) != p.version {
		return errResp(ErrProtocolVersionMismatch, "%d (!= %d)", status.ProtocolVersion, p.version)
	}
	return nil
}

// String implements fmt.Stringer.
func (p *peer) String() string {
	return fmt.Sprintf("Peer %s [%s]", p.id,
		fmt.Sprintf("aoa/%2d", p.version),
	)
}

// peerSet represents the collection of active peers currently participating in
// the Aurora sub-protocol.
type peerSet struct {
	peers  map[string]*peer
	lock   sync.RWMutex
	closed bool
}

// newPeerSet creates a new peer set to track the active participants.
func newPeerSet() *peerSet {
	return &peerSet{
		peers: make(map[string]*peer),
	}
}

// Register injects a new peer into the working set, or returns an error if the
// peer is already known.
func (ps *peerSet) Register(p *peer) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	if ps.closed {
		return errClosed
	}
	if _, ok := ps.peers[p.id]; ok {
		return errAlreadyRegistered
	}
	ps.peers[p.id] = p
	return nil
}

// Unregister removes a remote peer from the active set, disabling any further
// actions to/from that particular entity.
func (ps *peerSet) Unregister(id string) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	if _, ok := ps.peers[id]; !ok {
		return errNotRegistered
	}
	delete(ps.peers, id)
	return nil
}

// Peer retrieves the registered peer with the given id.
func (ps *peerSet) Peer(id string) *peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	return ps.peers[id]
}

// Len returns if the current number of peers in the set.
func (ps *peerSet) Len() int {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	return len(ps.peers)
}

// PeersWithoutBlock retrieves a list of peers that do not have a given block in
// their set of known hashes.
func (ps *peerSet) PeersWithoutBlock(hash common.Hash) []*peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()
	list := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.knownBlocks.Has(hash) {
			list = append(list, p)
		}
	}
	return list
}

func (ps *peerSet) PeersWithoutPreBlock(hash common.Hash) []*peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()
	list := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.knownPreBlocks.Has(hash) {
			list = append(list, p)
		}
	}
	return list
}

func (ps *peerSet) PeersWithoutSignatures(signs []types.VoteSign, blockHash []byte) []*peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()
	list := make([]*peer, 0, len(ps.peers))
	var b []byte
	b = append(b, blockHash...)
	for _, v := range signs {
		var temp []byte
		temp = append(temp, v.Sign...)
		temp = append(temp, byte(v.VoteAction))
		b = append(b, temp...)
	}
	signHax := common.ToHex(b)
	for _, p := range ps.peers {
		if !p.knownSignatures.Has(signHax) {
			list = append(list, p)
		}
	}
	return list
}

// PeersWithBlock retrieves a list of peers that have a given block in
// their set of known hashes
func (ps *peerSet) PeersWithBlock(hash common.Hash) []*peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()
	list := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if p.knownBlocks.Has(hash) {
			list = append(list, p)
		}
	}
	return list
}

func (ps *peerSet) PeersWithPrepare(sign string) []*peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()
	list := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if p.KnownPrepare.Has(sign) {
			list = append(list, p)
		}
	}
	return list
}

// PeersWithoutTx retrieves a list of peers that do not have a given transaction
// in their set of known hashes.
func (ps *peerSet) PeersWithoutTx(hash common.Hash) []*peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	list := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.knownTxs.Has(hash) {
			list = append(list, p)
		}
	}
	return list
}

func (ps *peerSet) PeersWithoutPrepare(sign string) []*peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	list := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.KnownPrepare.Has(sign) {
			list = append(list, p)
		}
	}
	return list
}

// BestPeer retrieves the known peer with the currently highest total difficulty.
func (ps *peerSet) BestPeer() *peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	var (
		bestPeer *peer
		bestTd   *big.Int
	)
	for _, p := range ps.peers {
		if _, td := p.Head(); bestPeer == nil || td.Cmp(bestTd) > 0 {
			bestPeer, bestTd = p, td
		}
	}
	return bestPeer
}

// Close disconnects all peers.
// No new peers can be registered after Close has returned.
func (ps *peerSet) Close() {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	for _, p := range ps.peers {
		p.Disconnect(p2p.DiscQuitting)
	}
	ps.closed = true
}
