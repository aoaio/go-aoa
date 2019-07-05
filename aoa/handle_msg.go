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
	"encoding/json"
	"github.com/Aurorachain/go-aoa/aoa/downloader"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/p2p"
	"github.com/Aurorachain/go-aoa/rlp"
	"math/big"
	"strings"
	"sync/atomic"
	"time"
)

type broadcastMsgHandle interface {
	// Status messages should never arrive after the handshake
	dealStatusMsg(msg p2p.Msg, p *peer) error
	// Block header query, collect the requested headers and reply
	dealGetBlockHeadersMsg(msg p2p.Msg, p *peer) error
	dealBlockHeadersMsg(msg p2p.Msg, p *peer) error
	dealGetBlockBodiesMsg(msg p2p.Msg, p *peer) error
	dealBlockBodiesMsg(msg p2p.Msg, p *peer) error
	dealGetNodeDataMsg(msg p2p.Msg, p *peer) error
	dealNodeDataMsg(msg p2p.Msg, p *peer) error
	dealGetReceiptsMsg(msg p2p.Msg, p *peer) error
	dealReceiptsMsg(msg p2p.Msg, p *peer) error
	dealNewBlockHashesMsg(msg p2p.Msg, p *peer) error
	dealTxMsg(msg p2p.Msg, p *peer) error

	dealNewBlockMsg(msg p2p.Msg, p *peer) error
	dealPreBlockMsg(msg p2p.Msg, p *peer) error
	dealSignaturesBlockMsg(msg p2p.Msg, p *peer) error
}

func (pm *ProtocolManager) dealStatusMsg(msg p2p.Msg, p *peer) error {
	return errResp(ErrExtraStatusMsg, "uncontrolled status message")
}
func (pm *ProtocolManager) dealGetBlockHeadersMsg(msg p2p.Msg, p *peer) error {
	// Decode the complex header query
	var query getBlockHeadersData
	if err := msg.Decode(&query); err != nil {
		return errResp(ErrDecode, "%v: %v", msg, err)
	}
	hashMode := query.Origin.Hash != (common.Hash{})

	// Gather headers until the fetch or network limits is reached
	var (
		bytes   common.StorageSize
		headers []*types.Header
		unknown bool
	)
	for !unknown && len(headers) < int(query.Amount) && bytes < softResponseLimit && len(headers) < downloader.MaxHeaderFetch {
		// Retrieve the next header satisfying the query
		var origin *types.Header
		if hashMode {
			origin = pm.blockchain.GetHeaderByHash(query.Origin.Hash)
		} else {
			origin = pm.blockchain.GetHeaderByNumber(query.Origin.Number)
		}
		if origin == nil {
			break
		}
		number := origin.Number.Uint64()
		headers = append(headers, origin)
		bytes += estHeaderRlpSize

		// Advance to the next header of the query
		switch {
		case query.Origin.Hash != (common.Hash{}) && query.Reverse:
			// Hash based traversal towards the genesis block
			for i := 0; i < int(query.Skip)+1; i++ {
				if header := pm.blockchain.GetHeader(query.Origin.Hash, number); header != nil {
					query.Origin.Hash = header.ParentHash
					number--
				} else {
					unknown = true
					break
				}
			}
		case query.Origin.Hash != (common.Hash{}) && !query.Reverse:
			// Hash based traversal towards the leaf block
			var (
				current = origin.Number.Uint64()
				next    = current + query.Skip + 1
			)
			if next <= current {
				infos, _ := json.MarshalIndent(p.Peer.Info(), "", "  ")
				log.Warn("GetBlockHeaders skip overflow attack", "current", current, "skip", query.Skip, "next", next, "attacker", infos)
				unknown = true
			} else {
				if header := pm.blockchain.GetHeaderByNumber(next); header != nil {
					if pm.blockchain.GetBlockHashesFromHash(header.Hash(), query.Skip+1)[query.Skip] == query.Origin.Hash {
						query.Origin.Hash = header.Hash()
					} else {
						unknown = true
					}
				} else {
					unknown = true
				}
			}
		case query.Reverse:
			// Number based traversal towards the genesis block
			if query.Origin.Number >= query.Skip+1 {
				query.Origin.Number -= query.Skip + 1
			} else {
				unknown = true
			}

		case !query.Reverse:
			// Number based traversal towards the leaf block
			query.Origin.Number += query.Skip + 1
		}
	}
	return p.SendBlockHeaders(headers)
}

func (pm *ProtocolManager) dealBlockHeadersMsg(msg p2p.Msg, p *peer) error {
	// A batch of headers arrived to one of our previous requests
	var headers []*types.Header
	if err := msg.Decode(&headers); err != nil {
		return errResp(ErrDecode, "msg %v: %v", msg, err)
	}
	// If no headers were received, but we're expending a DAO fork check, maybe it's that
	if len(headers) == 0 && p.forkDrop != nil {
		// Possibly an empty reply to the fork header checks, sanity check TDs

		// If we already have a DAO header, we can check the peer's TD against it. If
		// the peer's ahead of this, it too must have a reply to the DAO check

		// If we're seemingly on the same chain, disable the drop timer

	}
	// Filter out any explicitly requested headers, deliver the rest to the downloader
	filter := len(headers) == 1
	if filter {
		// If it's a potential DAO fork check, validate against the rules
		//if p.forkDrop != nil && pm.chainconfig.DAOForkBlock.Cmp(headers[0].Number) == 0 {
		//	// Disable the fork drop timer
		//	p.forkDrop.Stop()
		//	p.forkDrop = nil
		//
		//	// Validate the header and either drop the peer or continue
		//	if err := misc.VerifyDAOHeaderExtraData(pm.chainconfig, headers[0]); err != nil {
		//		log.Info("Verified to be on the other side of the DAO fork, dropping")
		//		return err
		//	}
		//	log.Info("Verified to be on the same side of the DAO fork")
		//	return nil
		//}
		// Irrelevant of the fork checks, send the header to the fetcher just in case
		headers = pm.fetcher.FilterHeaders(p.id, headers, time.Now())
	}
	if len(headers) > 0 || !filter {
		err := pm.downloader.DeliverHeaders(p.id, headers)
		if err != nil {
			log.Info("Failed to deliver headers", "err", err)
		}
	}
	return nil
}

func (pm *ProtocolManager) dealGetBlockBodiesMsg(msg p2p.Msg, p *peer) error {
	// Decode the retrieval message
	msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))
	if _, err := msgStream.List(); err != nil {
		return err
	}
	// Gather blocks until the fetch or network limits is reached
	var (
		hash   common.Hash
		bytes  int
		bodies []rlp.RawValue
	)
	for bytes < softResponseLimit && len(bodies) < downloader.MaxBlockFetch {
		// Retrieve the hash of the next block
		if err := msgStream.Decode(&hash); err == rlp.EOL {
			break
		} else if err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		// Retrieve the requested block body, stopping if enough was found
		if data := pm.blockchain.GetBodyRLP(hash); len(data) != 0 {
			bodies = append(bodies, data)
			bytes += len(data)
		}
	}
	return p.SendBlockBodiesRLP(bodies)
}

func (pm *ProtocolManager) dealBlockBodiesMsg(msg p2p.Msg, p *peer) error {
	// A batch of block bodies arrived to one of our previous requests
	var request blockBodiesData
	if err := msg.Decode(&request); err != nil {
		return errResp(ErrDecode, "msg %v: %v", msg, err)
	}
	// Deliver them all to the downloader for queuing
	trasactions := make([][]*types.Transaction, len(request))
	uncles := make([][]*types.Header, len(request))

	for i, body := range request {
		trasactions[i] = body.Transactions
	}
	// Filter out any explicitly requested bodies, deliver the rest to the downloader
	filter := len(trasactions) > 0 || len(uncles) > 0
	if filter {
		trasactions, uncles = pm.fetcher.FilterBodies(p.id, trasactions, uncles, time.Now())
	}
	if len(trasactions) > 0 || len(uncles) > 0 || !filter {
		err := pm.downloader.DeliverBodies(p.id, trasactions, uncles)
		if err != nil {
			log.Info("Failed to deliver bodies", "err", err)
		}
	}
	return nil
}

func (pm *ProtocolManager) dealGetNodeDataMsg(msg p2p.Msg, p *peer) error {
	// Decode the retrieval message
	msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))
	if _, err := msgStream.List(); err != nil {
		return err
	}
	// Gather state data until the fetch or network limits is reached
	var (
		hash  common.Hash
		bytes int
		data  [][]byte
	)
	for bytes < softResponseLimit && len(data) < downloader.MaxStateFetch {
		// Retrieve the hash of the next state entry
		if err := msgStream.Decode(&hash); err == rlp.EOL {
			break
		} else if err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		// Retrieve the requested state entry, stopping if enough was found
		if entry, err := pm.chaindb.Get(hash.Bytes()); err == nil {
			data = append(data, entry)
			bytes += len(entry)
		}
	}
	return p.SendNodeData(data)
}

func (pm *ProtocolManager) dealNodeDataMsg(msg p2p.Msg, p *peer) error {
	// A batch of node state data arrived to one of our previous requests
	var data [][]byte
	if err := msg.Decode(&data); err != nil {
		return errResp(ErrDecode, "msg %v: %v", msg, err)
	}
	// Deliver all to the downloader
	if err := pm.downloader.DeliverNodeData(p.id, data); err != nil {
		log.Info("Failed to deliver node state data", "err", err)
	}
	return nil
}

func (pm *ProtocolManager) dealGetReceiptsMsg(msg p2p.Msg, p *peer) error {
	// Decode the retrieval message
	msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))
	if _, err := msgStream.List(); err != nil {
		return err
	}
	// Gather state data until the fetch or network limits is reached
	var (
		hash     common.Hash
		bytes    int
		receipts []rlp.RawValue
	)
	for bytes < softResponseLimit && len(receipts) < downloader.MaxReceiptFetch {
		// Retrieve the hash of the next block
		if err := msgStream.Decode(&hash); err == rlp.EOL {
			break
		} else if err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		// Retrieve the requested block's receipts, skipping if unknown to us
		results := core.GetBlockReceipts(pm.chaindb, hash, core.GetBlockNumber(pm.chaindb, hash))
		if results == nil {
			if header := pm.blockchain.GetHeaderByHash(hash); header == nil || header.ReceiptHash != types.EmptyRootHash {
				continue
			}
		}
		// If known, encode and queue for response packet
		if encoded, err := rlp.EncodeToBytes(results); err != nil {
			log.Error("Failed to encode receipt", "err", err)
		} else {
			receipts = append(receipts, encoded)
			bytes += len(encoded)
		}
	}
	return p.SendReceiptsRLP(receipts)
}

func (pm *ProtocolManager) dealReceiptsMsg(msg p2p.Msg, p *peer) error {
	// A batch of receipts arrived to one of our previous requests
	var receipts [][]*types.Receipt
	if err := msg.Decode(&receipts); err != nil {
		return errResp(ErrDecode, "msg %v: %v", msg, err)
	}
	// Deliver all to the downloader
	if err := pm.downloader.DeliverReceipts(p.id, receipts); err != nil {
		log.Info("Failed to deliver receipts", "err", err)
	}
	return nil
}

func (pm *ProtocolManager) dealNewBlockHashesMsg(msg p2p.Msg, p *peer) error {
	log.Info("NewBlockHashesMsg start")
	var announces newBlockHashesData
	if err := msg.Decode(&announces); err != nil {
		return errResp(ErrDecode, "%v: %v", msg, err)
	}
	// Mark the hashes as present at the remote node
	for _, block := range announces {
		p.MarkBlock(block.Hash)
	}
	// Schedule all the unknown hashes for retrieval
	unknown := make(newBlockHashesData, 0, len(announces))
	for _, block := range announces {
		if !pm.blockchain.HasBlock(block.Hash, block.Number) {
			unknown = append(unknown, block)
		}
	}
	for _, block := range unknown {
		pm.fetcher.Notify(p.id, block.Hash, block.Number, time.Now(), p.RequestOneHeader, p.RequestBodies)
	}
	return nil
}

func (pm *ProtocolManager) dealNewBlockMsg(msg p2p.Msg, p *peer) error {
	// Retrieve and decode the propagated block
	var request newBlockData

	if err := msg.Decode(&request); err != nil {
		return errResp(ErrDecode, "%v: %v", msg, err)
	}
	request.Block.ReceivedAt = msg.ReceivedAt
	request.Block.ReceivedFrom = p
	block := request.Block
	rlpEncodeSigns := request.RlpEncodeSigns
	block.RlpEncodeSigns = rlpEncodeSigns
	currentBlock := pm.blockchain.CurrentBlock()
	td := pm.blockchain.GetTd(currentBlock.Hash(), currentBlock.NumberU64())
	log.Infof("New block msg receive, blockNumber=%v, localNumber=%v, localTd=%v, rlpSignsLen=%v", block.NumberU64(), currentBlock.NumberU64(), td, len(rlpEncodeSigns))
	if block.NumberU64() <= currentBlock.NumberU64() {
		log.Errorf("New block msg end|block is exist, currentBlockNumber=%v", currentBlock.NumberU64())
		return nil
	}
	// lost block
	if block.NumberU64() > currentBlock.NumberU64()+1 {
		go pm.synchroniseWithHigherPeers(p, block)
		return nil
	}

	err := pm.engine.VerifyBlockGenerate(pm.blockchain, block, pm.taskManager.GetCurrentShuffleRound(), blockInterval)
	if err != nil {
		log.Error("New block Msg|verify block fail, blockNumber=%v, %v", block.NumberU64(), err)
		pm.shuffleIfVerify(block)
		err = pm.engine.VerifyBlockGenerate(pm.blockchain, block, pm.taskManager.GetCurrentShuffleRound(), blockInterval)
		if err != nil {
			log.Error("New block Msg|verify block fail again, blockNumber=%v, %v", block.NumberU64(), err)
			return nil
		}
	}
	// agent node
	lockBlock := pm.lockBlockManager.getPendingBlock()
	if lockBlock != nil && lockBlock.block.NumberU64() == block.NumberU64() {
		pm.lockBlockManager.unlock()
	}
	// Mark the peer as owning the block and schedule it for import
	p.MarkBlock(request.Block.Hash())
	pm.fetcher.Enqueue(p.id, request.Block)

	var (
		trueHead = block.ParentHash()
		// TODO already remove difficult,add 1
		trueTD = new(big.Int).Sub(request.TD, types.BlockDifficult)
	)
	if _, td := p.Head(); trueTD.Cmp(td) > 0 {
		p.SetHead(trueHead, trueTD)
		currentBlock := currentBlock
		if trueTD.Cmp(pm.blockchain.GetTd(currentBlock.Hash(), currentBlock.NumberU64())) > 0 {
			go pm.synchronise(p)
		}
	}

	go pm.BroadcastBlock(block, true)
	return nil
}

// only execute when unlock,if verify success,add block to memory.otherwise,broadcast oppose votes
func (pm *ProtocolManager) dealPreBlockMsg(msg p2p.Msg, p *peer) error {
	var request preBlockData
	if err := msg.Decode(&request); err != nil {
		return errResp(ErrDecode, "%v: %v", msg, err)
	}
	block := request.Block
	log.Info("PreBlockMsg receive", "blockNumber", block.NumberU64(), "blockHash", block.Hash().Hex(), "coinbase", block.Coinbase().Hex())
	// lost block
	currentBlock := pm.blockchain.CurrentBlock()
	if block.NumberU64() <= currentBlock.NumberU64() {
		log.Errorf("PreBlockMsg end|block is exist, currentBlockNumber=%v", currentBlock.NumberU64())
		return nil
	}
	if block.NumberU64() > currentBlock.NumberU64()+1 {
		log.Infof("PreBlockMsg end|because of lost block begin to sync, currentBlockNumber=%v, receiveBlockNumber=%v", currentBlock.NumberU64(), block.NumberU64())
		go pm.synchroniseWithHigherPeers(p, block)
		return nil
	}
	// 分叉 block number - current block number = 1 但是 block parent hash 不是 current block hash
	if block.ParentHash() != currentBlock.Hash() {
		// currentBlock rollback
		// pm.blockchain.RollbackBFT(currentBlock)
		// go pm.synchronise(p)
		// return nil
		log.Errorf("dealPreBlockMsg block hash is not same, blockNumber=%v, localBlockHash%v, remoteBlockHash%v", currentBlock.NumberU64(), currentBlock.Hash().Hex(), block.ParentHash().Hex())
	}
	// 根据delegatedb重新计算洗牌列表，再校验一次，最多校验两次，第二次如果校验成功，则更新洗牌列表
	verifyBlock := true
	err := pm.engine.VerifyHeaderAndSign(pm.blockchain, block, pm.taskManager.GetCurrentShuffleRound(), blockInterval)
	if err != nil {
		log.Error("PreBlockMsg verify block fail", "blockNumber", block.NumberU64(), "err", err)
		verifyBlock = false
	}
	p.MarkPreBlock(block.Hash())
	if verifyBlock { // verify success
		go pm.PreBroadcastBlock(block)
		pm.lockBlockManager.unlock()
		err = pm.lockBlockManager.addPendingBlock(block)
		if err != nil {
			log.Errorf("PreBlockMsg|add pendingBlock fail, blockNumber=%v, err=%v", block.NumberU64(), err)
			return nil
		}
		go pm.addSignToPendingBlock(block, approveVote)
	} else {
		log.Info("PreBlockMsg|push oppose vote start, blockHash=%v", block.Hash().Hex())
		go pm.addSignToPendingBlock(block, opposeVote)
	}
	return nil
}

func (pm *ProtocolManager) dealSignaturesBlockMsg(msg p2p.Msg, p *peer) error {
	var signBlockMsg signaturesBlockMsg
	if err := msg.Decode(&signBlockMsg); err != nil {
		log.Errorf("dealSignaturesBlockMsg|err, err=%v", err)
		return errResp(ErrDecode, "%v: %v", msg, err)
	}
	blockHash := common.BytesToHash(signBlockMsg.BlockHash)
	blockHashHex := common.BytesToHash(signBlockMsg.BlockHash).Hex()
	log.Infof("SignaturesBlockMsg receive, blockHash=%v, signLen=%v", blockHashHex, len(signBlockMsg.Signatures))
	if p.HasReceiveSignatures(signBlockMsg.Signatures, signBlockMsg.BlockHash) {
		log.Infof("SignaturesBlockMsg already receive, blockHash=%v", blockHashHex)
		return nil
	}
	p.MarkBlockSignatures(signBlockMsg.Signatures, signBlockMsg.BlockHash)
	currentShuffleRound := pm.taskManager.GetCurrentShuffleRound()
	err := pm.lockBlockManager.checkBroadcastSignatures(blockHash, signBlockMsg.Signatures, currentShuffleRound)
	if err != nil {
		log.Errorf("dealSignaturesBlockMsg|receive error sign msg, blockHash=%v, peerId=%v, err=%v", blockHashHex, p.id, err)
		return nil
	}
	go pm.BroadcastBlockSignatures(signBlockMsg.BlockHash, signBlockMsg.Signatures)
	for _, signVote := range signBlockMsg.Signatures {
		go pm.lockBlockManager.addSignToPendingBlock(blockHash, signVote.Sign, signVote.VoteAction, currentShuffleRound)
	}
	return nil
}

func (pm *ProtocolManager) dealTxMsg(msg p2p.Msg, p *peer) error {
	// log.Info("收到广播出来的交易", "message", msg)
	// Transactions arrived, make sure we have a valid and fresh chain to handle them
	if atomic.LoadUint32(&pm.acceptTxs) == 0 {
		return nil
	}
	// Transactions can be processed, parse all of them and deliver to the pool
	var txs []*types.Transaction
	if err := msg.Decode(&txs); err != nil {
		return errResp(ErrDecode, "msg %v: %v", msg, err)
	}

	for i, tx := range txs {
		// Validate and mark the remote transaction
		if tx == nil {
			return errResp(ErrDecode, "transaction %d is nil", i)
		}
		p.MarkTransaction(tx.Hash())

	}
	pm.txpool.AddRemotes(txs)
	return nil
}

// generate correct shuffleList when verify fail,only try once
func (pm *ProtocolManager) shuffleIfVerify(block *types.Block) {
	pm.taskManager.ShuffleWhenVerifyFail(block.Number().Int64(), block.Time().Int64(), block.Header().ShuffleBlockNumber)
}

func (pm *ProtocolManager) signBlockWithLocalDelegates(block *types.Block) [][]byte {
	localExistdelegates := pm.taskManager.GetLocalCurrentRound()
	signs := make([][]byte, 0)
	for _, address := range localExistdelegates {
		address = strings.ToLower(address)
		if _, ok := pm.delegateWallets[address]; !ok {
			continue
		}
		privateKey := pm.delegateWallets[address]
		sign, err := crypto.Sign(block.Hash().Bytes()[:32], privateKey)
		if err != nil {
			continue
		}
		signs = append(signs, sign)
	}
	return signs
}
