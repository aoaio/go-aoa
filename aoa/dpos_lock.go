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
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/crypto/secp256k1"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/rlp"
	"github.com/Aurorachain/go-aoa/task"
	"golang.org/x/crypto/sha3"
	"sort"
	"strings"
	"sync"
	"time"
)

var approveVote = uint64(0)
var opposeVote = uint64(1)
var (
	pendingBlockExist = errors.New("current block is locking")
)

type lockManager struct {
	mu                 sync.RWMutex
	taskIds            sync.Map
	tw                 *task.TimingWheel
	wheelCtx           context.Context
	cancelFunction     func()
	insertBlockFunc    func(blocks types.Blocks) (int, error)
	broadcastBlockChan chan *types.Block
	signaturesChan     chan *signaturesBlockMsg
	delegateWallets    map[string]*ecdsa.PrivateKey
	lockBlock          *pendBlock
	storeChan          chan *storeSigns
	signMap            *blocksSignMap
}

type pendBlock struct {
	block *types.Block
}

type storeSigns struct {
	blockHash           common.Hash
	sign                []byte
	action              uint64
	currentShuffleRound *types.ShuffleList
	address             string
}

type blockSignMap struct {
	mu           sync.RWMutex
	data         map[string]types.VoteSign
	approveVotes uint64
}

type blocksSignMap struct {
	mu   sync.RWMutex
	data map[string]*blockSignMap
}

type commmitBlockData struct {
	block          *types.Block
	rlpEncodeSigns []byte
}

func newBlockLockManager(insertBlockFunc func(blocks types.Blocks) (int, error), delegateWallets map[string]*ecdsa.PrivateKey) *lockManager {
	// create SimpleBlockPool
	ctx, cancelFunc := context.WithCancel(context.Background())
	timingWheel := task.NewTimingWheel(ctx)
	lockManager := &lockManager{
		tw:                 timingWheel,
		wheelCtx:           ctx,
		cancelFunction:     cancelFunc,
		insertBlockFunc:    insertBlockFunc,
		broadcastBlockChan: make(chan *types.Block, 101),
		signaturesChan:     make(chan *signaturesBlockMsg, 1024),
		delegateWallets:    delegateWallets,
		storeChan:          make(chan *storeSigns, 1024),
		signMap:            newBlocksSignMap(),
	}
	go lockManager.storeBlockSign()
	return lockManager
}

func (l *lockManager) addPendingBlock(b *types.Block) error {
	startTime := time.Now().Nanosecond()
	log.Info("lockManager|add pending block start", "blockNumber", b.Number().Int64())
	if l.isLock() {
		log.Error("lockManager|add pending block fail|is locking", "blockNumber", b.Number().Int64())
		return pendingBlockExist
	}
	l.lock(b)

	blockTimeOutTask := &task.OnTimeOut{
		Callback: func(ctx context.Context) {
			if l.isLock() {
				pendingBlock := l.getPendingBlock()
				if pendingBlock != nil && strings.EqualFold(pendingBlock.block.Hash().Hex(), b.Hash().Hex()) {
					l.unlock()
				}
			}
		},
		Ctx: context.Background(),
	}
	expireTimeUnix := b.Time().Int64() + int64(blockInterval)
	expireTime := time.Unix(expireTimeUnix, 0)
	id := l.tw.AddTimer(expireTime, -1, blockTimeOutTask)
	l.taskIds.Store(b.Hash().Hex(), id)
	log.Info("lockManager|add pending block success", "blockNumber", b.Number().Int64(), "coinbase", b.Coinbase().Hex(), "expireTime", expireTime, "costTime(ns)", time.Now().Nanosecond()-startTime)
	return nil
}

func (l *lockManager) addSignToPendingBlock(blockHash common.Hash, sign []byte, action uint64, currentShuffleRound *types.ShuffleList) error {
	// log.Info("lockManager|addSignToPendingBlock|start","blockHash",blockHash.Hex())
	pubKey, err := secp256k1.RecoverPubkey(blockHash.Bytes()[:32], sign)
	if err != nil {
		log.Error("lockManager|add signToPendingBlock block fail", "blockHash", blockHash.Hex(), "err", err)
		return err
	}
	pubAddress := crypto.PubkeyToAddress(*crypto.ToECDSAPub(pubKey)).Hex()
	var addressIsDelegate bool
	for _, v := range currentShuffleRound.ShuffleDels {
		if strings.EqualFold(pubAddress, v.Address) {
			addressIsDelegate = true
			break
		}
	}
	if !addressIsDelegate {
		return errors.New(fmt.Sprintf("sign address is not current delegatePeers|address:%s", pubAddress))
	}

	// check do not deal with repeat msg
	signMap, ok := l.signMap.get(blockHash.Hex())
	if ok {
		if signMap.exist(pubAddress) {
			return errors.New(fmt.Sprintf("signMap already contains this  address sign blockHash:%s address:%s", blockHash.Hex(), pubAddress))
		}
	}

	l.storeChan <- &storeSigns{blockHash, sign, action, currentShuffleRound, pubAddress}
	// log.Info("lockManager|addSignToPendingBlock|end","blockHash",blockHash.Hex())
	return nil
}

func (l *lockManager) checkBroadcastSignatures(blockHash common.Hash, signs []types.VoteSign, currentShuffleRound *types.ShuffleList) error {
	for _, sign := range signs {
		pubkey, err := secp256k1.RecoverPubkey(blockHash.Bytes()[:32], sign.Sign)
		if err != nil {
			return err
		}
		pubAddress := crypto.PubkeyToAddress(*crypto.ToECDSAPub(pubkey)).Hex()
		var addressIsDelegate bool
		delegateAddresses := make([]string, 0)
		for _, v := range currentShuffleRound.ShuffleDels {
			delegateAddresses = append(delegateAddresses, v.Address)
			if strings.EqualFold(pubAddress, v.Address) {
				addressIsDelegate = true
				break
			}
		}
		if !addressIsDelegate {
			log.Error("lockManager|checkSignError", "sign address", pubAddress, "delegateAddresses", delegateAddresses)
			return errors.New(fmt.Sprintf("sign address is not current delegatePeers|address:%s", pubAddress))
		}
	}
	return nil
}

func (l *lockManager) storeBlockSign() {
	for {
		select {
		case v := <-l.storeChan:
			l.dealBlockSign(v)
		}
	}
}

func (l *lockManager) dealBlockSign(storeSign *storeSigns) {
	blockHash := storeSign.blockHash
	address := storeSign.address
	blockHashHex := blockHash.Hex()
	// log.Info("lockBlockManager|dealBlockSign|receive", "blockHash", blockHashHex, "signAddress", address)
	signMap, ok := l.signMap.get(blockHashHex)
	if !ok {
		signMap = newBlockSignMap()
		signMap.put(address, types.VoteSign{Sign: storeSign.sign, VoteAction: storeSign.action})
		l.signMap.put(blockHashHex, signMap)
		log.Info("lockBlockManager|add sign success", "blockHash", blockHashHex, "signLength", len(signMap.data), "approveVoteCount", signMap.countApproveVote())
		timeOutTask := &task.OnTimeOut{
			Callback: func(ctx context.Context) {
				l.signMap.delete(blockHashHex)
			},
			Ctx: context.Background(),
		}
		expireTimeUnix := time.Now().Unix() + int64(blockInterval*10)
		expireTime := time.Unix(expireTimeUnix, 0)
		l.tw.AddTimer(expireTime, -1, timeOutTask)
	} else {
		if signMap.countApproveVote() > uint64(delegateAmount) {
			return
		}
		if !signMap.exist(address) {
			signMap.put(address, types.VoteSign{Sign: storeSign.sign, VoteAction: storeSign.action})
			// l.signMap.put(blockHashHex, signMap)
			log.Info("lockBlockManager|add sign success", "blockHash", blockHashHex, "signLength", len(signMap.data), "approveVoteCount", signMap.approveVotes)
		}

	}
	if signMap.countApproveVote() > uint64(delegateAmount) {
		log.Info("lockBlockManager|collect 2/3 sign success", "blockHash", blockHashHex, "signLength", len(signMap.data))
		signs := make([]types.VoteSign, 0, delegateAmount+1)
		approveVoteCount := 0
		for _, v := range signMap.data {
			if v.VoteAction == approveVote {
				signs = append(signs, v)
				approveVoteCount++
				if approveVoteCount > delegateAmount {
					break
				}
			}
		}
		// 执行广播的callback
		go l.blockGenerateCallback(signs, blockHashHex)
	}
}

func (l *lockManager) blockGenerateCallback(signs []types.VoteSign, blockHash string) {
	log.Info("lockBlockManager|blockGenerateCallback|start", "blockHash", blockHash, "lenSign", len(signs))
	pendingBlock := l.getPendingBlock()
	// go l.blockSignCallback(signs, blockHash)
	if pendingBlock != nil {
		block := pendingBlock.block
		log.Info("lockManager|blockGenerateSuccess", "blockNumber", block.NumberU64(), "blockHash", blockHash, "coinbase", block.Coinbase().Hex(), "consensus cost time(s)", time.Now().Unix()-block.Time().Int64())
		if strings.EqualFold(block.Hash().Hex(), blockHash) {
			err := l.newBlockCallback(signs, block)
			// cancel expire task
			if err == nil {
				// unlock pending block
				l.unlock()
				if id, ok := l.taskIds.Load(block.Hash().Hex()); ok {
					l.tw.CancelTimer(id.(int64))
				}
			}
		}
	} else {
		log.Error("lockBlockManager|blockGenerateCallback|end|pendingBlock is null", "blockHash", blockHash)
	}
}

func (l *lockManager) blockSignCallback(signs []types.VoteSign, blockHash string) {
	bHash := common.HexToHash(blockHash)
	if len(signs) > 1 {
		sort.Sort(voteSignSlice(signs))
	}
	l.signaturesChan <- &signaturesBlockMsg{Signatures: signs, BlockHash: bHash.Bytes()}
	log.Info("lockManager|blockSignCallback|success", "blockHash", bHash.Hex())
}

func (l *lockManager) newBlockCallback(signs []types.VoteSign, block *types.Block) error {
	rlpEncodeSigns, err := rlp.EncodeToBytes(signs)
	if err != nil {
		log.Error("lockManager|newBlockCallback|end|sign fail", "blockNumber", block.NumberU64(), "err", err)
		return err
	}
	//confirmSign, err := l.signConfirmInfo(block.Header().Coinbase, rlpEncodeSigns)
	//if err != nil {
	//	log.Error("lockManager|newBlockCallback|end|sign fail", "blockNumber", block.NumberU64(), "err", err)
	//	return err
	//}
	var blocks types.Blocks
	block.RlpEncodeSigns = rlpEncodeSigns
	blocks = append(blocks, block)
	result, err := l.insertBlockFunc(blocks)
	log.Info("lockManager|newBlockCallback|insertBlockFunc", "blockNumber", block.NumberU64(), "result", result, "err", err)
	log.Info("lockManager|newBlockCallback|broadcast block start", "blockNumber", block.NumberU64(), "coinbase", block.Coinbase().Hex())
	l.broadcastBlockChan <- block
	return nil

}

// sign confirm signs by coinbase
func (l *lockManager) signConfirmInfo(coinbase common.Address, rlpEncodeSigns []byte) ([]byte, error) {
	address := strings.ToLower(coinbase.Hex())
	if _, ok := l.delegateWallets[address]; !ok {
		errMsg := fmt.Sprintf("SignConfirmInfo fail because can not find pwd in memory address:%s", coinbase.Hex())
		return nil, errors.New(errMsg)
	}
	privateKey := l.delegateWallets[address]
	// rlpEncodeSigns is over 32 bytes
	sha256Rlp := sha3.Sum256(rlpEncodeSigns)
	//sign, err := wallet.SignHash(account, sha256Rlp[:])
	sign, err := crypto.Sign(sha256Rlp[:], privateKey)
	if err != nil {
		return nil, err
	}
	return sign, nil
}

func (l *lockManager) unlock() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lockBlock = nil
}

func (l *lockManager) lock(b *types.Block) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lockBlock = &pendBlock{block: b}
}

func (l *lockManager) getPendingBlock() *pendBlock {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.lockBlock
}

func (l *lockManager) isLock() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.lockBlock != nil
}

func (b *blockSignMap) put(address string, sign types.VoteSign) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.data[address]; !ok {
		b.data[address] = sign
		if sign.VoteAction == approveVote {
			b.approveVotes++
		}
	}
}

func (b *blockSignMap) exist(address string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.data[address]
	return ok
}

func (b *blockSignMap) countApproveVote() uint64 {
	return b.approveVotes
}

func newBlockSignMap() *blockSignMap {
	tempMap := make(map[string]types.VoteSign, 0)
	return &blockSignMap{data: tempMap}
}

func (b *blocksSignMap) get(key string) (*blockSignMap, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	data, ok := b.data[key]
	return data, ok
}

func (b *blocksSignMap) put(key string, data *blockSignMap) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[key] = data
}

func (b *blocksSignMap) delete(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.data, key)
}

func newBlocksSignMap() *blocksSignMap {
	blockMap := make(map[string]*blockSignMap, 0)
	return &blocksSignMap{data: blockMap}
}
