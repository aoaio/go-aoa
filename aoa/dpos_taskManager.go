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
	"fmt"
	"github.com/Aurorachain/go-aoa/accounts"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/consensus/delegatestate"
	"github.com/Aurorachain/go-aoa/core"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/crypto/secp256k1"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/node"
	"github.com/Aurorachain/go-aoa/rlp"
	"github.com/Aurorachain/go-aoa/task"
	"github.com/Aurorachain/go-aoa/util"
	"github.com/pkg/errors"
	"golang.org/x/crypto/sha3"
	"math/big"
	"strings"
	"sync"
	"time"
)

const (
	delegateStorePrefix = "delegateShuffledata"
	secondDuration      = 1000000000
)

type DposTaskManager struct {
	runningTimeIds          []int64
	timingWheel             *task.TimingWheel         // time schedule
	shuffleCallback         func(ctx context.Context) // 洗牌回调方法
	produceBlockCallback    func(ctx context.Context) // 出块回调方法
	blockchain              *core.BlockChain
	accountManager          *accounts.Manager
	lock                    sync.WaitGroup
	shuffleNewRoundChan     chan types.ShuffleList
	currentNewRound         types.ShuffleList       // 保存本轮洗牌的列表
	currentRoundBlockHeight int64                   // 本轮洗牌对应的块高
	currentNewRoundHash     common.Hash             // 本轮洗牌列表的Hex
	shuffleHashChan         chan *types.ShuffleData // use by produce call back
	delegateStoredb         aoadb.Database
	mu                      sync.Mutex
}

var initTaskBeginTime int64
var shuffleCount int64

var maxElectDelegate int
var blockInterval int
var delegateAmount int

func NewDposTaskManager(ctx *node.ServiceContext, blockchain *core.BlockChain, accountManager *accounts.Manager, produceBlockCallback func(ctx context.Context), shuffleHashChan chan *types.ShuffleData) *DposTaskManager {
	genesisConfig := blockchain.Config()
	maxElectDelegate = int(genesisConfig.MaxElectDelegate.Int64())
	blockInterval = int(genesisConfig.BlockInterval.Int64())
	delegateAmount = (maxElectDelegate / 3) * 2
	log.Infof("NewDposTaskManager, maxElectDelegate=%s, blockInterval=%s, delegateAmount=%s", maxElectDelegate, blockInterval, delegateAmount)
	taskManager := &DposTaskManager{
		runningTimeIds:       make([]int64, 0, maxElectDelegate+1),
		timingWheel:          task.NewTimingWheel(context.Background()),
		produceBlockCallback: produceBlockCallback,
		blockchain:           blockchain,
		accountManager:       accountManager,
		shuffleNewRoundChan:  make(chan types.ShuffleList),
		shuffleHashChan:      shuffleHashChan,
	}

	delegateDB, err := createDelegateDB(ctx)
	if err != nil {
		log.Errorf("DposTaskManager fail to create database, err=%s", err)
	}
	taskManager.delegateStoredb = delegateDB

	shuffleCallback := func(ctx context.Context) {
		currentBlock := taskManager.blockchain.CurrentBlock()
		dState, err := taskManager.blockchain.DelegateStateAt(currentBlock.DelegateRoot())
		if err != nil {
			log.Errorf("shuffle create delegateState fail, err=%s", err)
			return
		}
		// cal shuffle time of current round
		shuffleTime := initTaskBeginTime + shuffleCount*int64(maxElectDelegate*blockInterval)
		exist, candidates := taskManager.checkLocalExistDelegateWhenShuffle(dState)
		if !exist {
			log.Infof("DposTaskManager| shuffle end because doesn't exist delegate in this node, blockNumber=%s, len=%s", currentBlock.NumberU64(), len(candidates))
			return
		}
		topDelegates := candidates

		if len(topDelegates) == 0 {
			log.Errorf("dposTaskManager|shuffle doesn't have any delegatePeers,blockchain is stopping")
			return
		}
		if len(topDelegates) > maxElectDelegate {
			topDelegates = topDelegates[:maxElectDelegate]
		}
		for _, v := range candidates {
			log.Infof("shuffle candidate, blockNumber=%d, address=%s, vote=%d, nickName=%s, registerTime=%v", currentBlock.NumberU64(), v.Address, v.Vote, v.Nickname, time.Unix(int64(v.RegisterTime), 0))
		}
		shuffleNewRound := util.ShuffleNewRound(shuffleTime, maxElectDelegate, topDelegates, int64(blockInterval))
		shuffleData := types.ShuffleDelegateData{BlockNumber: *currentBlock.Number(), ShuffleTime: *big.NewInt(shuffleTime)}
		err = taskManager.loadShuffleDataToDB(shuffleData)
		if err != nil {
			log.Errorf("DposTaskManager| shuffle data fail load to db. err=%s", err)
			return
		}
		taskManager.mu.Lock()
		defer taskManager.mu.Unlock()
		taskManager.currentRoundBlockHeight = currentBlock.Number().Int64()
		shuffleList := types.ShuffleList{ShuffleDels: shuffleNewRound}
		taskManager.currentNewRound = shuffleList
		rlpShufflehash := rlpHash(shuffleList)
		taskManager.currentNewRoundHash = rlpShufflehash
		taskManager.shuffleHashChan <- &types.ShuffleData{ShuffleHash: &rlpShufflehash, ShuffleBlockNumber: currentBlock.Number()}
		log.Infof("shuffle, shuffleHash=%s", rlpShufflehash.Hex())
		taskManager.shuffleNewRoundChan <- shuffleList
		shuffleCount++
	}
	taskManager.shuffleCallback = shuffleCallback
	// init dpos task
	taskManager.initTask()
	taskManager.initShuffleDataFromLevelDB()
	go taskManager.update()
	return taskManager
}

func (taskManager *DposTaskManager) update() {
	for {
		select {
		case shuffleList := <-taskManager.shuffleNewRoundChan:
			shuffleNewRound := shuffleList.ShuffleDels
			for _, v := range shuffleNewRound {
				log.Infof("dposTaskManager|delegates in latest turn, delegate=%v", v)
			}
			localDelegates := make([]types.ProduceDelegate, 0)
			for _, v := range shuffleNewRound {
				exist, localDelegate := taskManager.checkNodeInAccounts(v)
				if exist {
					localDelegates = append(localDelegates, localDelegate)
				}
			}
			if len(taskManager.runningTimeIds) > 1 {
				taskManager.runningTimeIds = taskManager.runningTimeIds[:1]
			}
			if len(localDelegates) > 0 {
				for _, v := range localDelegates {
					ctx := context.WithValue(context.Background(), types.DelegatePrefix, v)
					onTimeOut := &task.OnTimeOut{
						Callback: taskManager.produceBlockCallback,
						Ctx:      ctx,
					}
					timeId := taskManager.timingWheel.AddTimer(time.Unix(int64(v.WorkTime), 0), time.Duration(-1), onTimeOut)
					taskManager.runningTimeIds = append(taskManager.runningTimeIds, timeId)
				}
				log.Info("dposTaskManager", "timeIds", taskManager.runningTimeIds)
			}
			taskManager.currentNewRound = shuffleList
		}
	}
}

func (taskManager *DposTaskManager) initTask() {
	for _, timeId := range taskManager.runningTimeIds {
		taskManager.timingWheel.CancelTimer(timeId)
	}
	taskManager.runningTimeIds = make([]int64, 0, maxElectDelegate+1)
	onTimeOut := &task.OnTimeOut{Callback: taskManager.shuffleCallback, Ctx: context.Background()}

	genesisTime := taskManager.blockchain.Genesis().Header().Time.Int64()
	nextRoundBeginTime, _ := generateNextRoundBeginTime(genesisTime)
	initTimeId := taskManager.timingWheel.AddTimer(time.Unix(nextRoundBeginTime, 0), time.Duration(blockInterval*maxElectDelegate*secondDuration), onTimeOut)
	log.Infof("dposTaskManager, initTask|beginTime=%s, initTimeId=%v", time.Unix(nextRoundBeginTime, 0), initTimeId)
	initTaskBeginTime = nextRoundBeginTime
	taskManager.runningTimeIds = append(taskManager.runningTimeIds, initTimeId)
}

// shuffle to create new shuffleList when verify fail,only try one times.
func (taskManager *DposTaskManager) ShuffleWhenVerifyFail(receiveBlockNumber int64, receiveBlockTime int64, shuffleBlockNumber *big.Int) error {
	shuffleBlock := taskManager.blockchain.GetBlockByNumber(shuffleBlockNumber.Uint64())
	if shuffleBlock == nil {
		errMsg := fmt.Sprintf("shuffleBlockNumber not exist shuffleBlockNumber:%d", shuffleBlockNumber.Uint64())
		return errors.New(errMsg)
	}
	delegateState, err := taskManager.blockchain.DelegateStateAt(shuffleBlock.DelegateRoot())
	log.Info("dposTaskManager|ShuffleWhenVerifyFail", "shuffleBlockNumber", shuffleBlock.NumberU64(), "receiveBlockNumber", receiveBlockNumber)
	if err != nil {
		return err
	}
	exist, candidates := taskManager.checkLocalExistDelegateWhenShuffle(delegateState)
	topDelegates := candidates
	if len(topDelegates) == 0 {
		return errors.New("delegate not exist")
	}
	if len(topDelegates) > maxElectDelegate {
		topDelegates = topDelegates[:maxElectDelegate]
	}
	shuffleTime := util.CalShuffleTimeByHeaderTime(initTaskBeginTime, receiveBlockTime, int64(blockInterval), int64(maxElectDelegate))
	shuffleNewRound := util.ShuffleNewRound(shuffleTime, maxElectDelegate, topDelegates, int64(blockInterval))
	log.Debugf("dposTaskManager|verifyFail|shuffleEnd, shuffleTime=%v, blockNumber=%v, lenCandidates=%v, result=%v.", shuffleTime, shuffleBlock.NumberU64(), len(topDelegates), shuffleNewRound)
	shuffleData := types.ShuffleDelegateData{BlockNumber: *shuffleBlock.Number(), ShuffleTime: *big.NewInt(shuffleTime)}
	err = taskManager.loadShuffleDataToDB(shuffleData)
	if err != nil {
		return err
	}
	taskManager.mu.Lock()
	defer taskManager.mu.Unlock()
	taskManager.currentRoundBlockHeight = shuffleBlock.Number().Int64()
	shuffleList := types.ShuffleList{ShuffleDels: shuffleNewRound}
	taskManager.currentNewRound = shuffleList
	rlpShufflehash := rlpHash(shuffleList)
	taskManager.currentNewRoundHash = rlpShufflehash
	taskManager.shuffleHashChan <- &types.ShuffleData{ShuffleHash: &rlpShufflehash, ShuffleBlockNumber: shuffleBlock.Number()}

	if !exist || shuffleTime < time.Now().Unix() { // shuffleTime already expire
		return nil
	}
	taskManager.shuffleNewRoundChan <- shuffleList
	return nil
}

// check local whether exist delegate,return candidates when exist
func (taskManager *DposTaskManager) checkLocalExistDelegateWhenShuffle(d *delegatestate.DelegateDB) (bool, []types.Candidate) {
	candidates := d.GetDelegates()
	log.Debugf("checkLocalExistDelegateWhenShuffle, candidates=%v", candidates)
	for _, v := range candidates {
		if taskManager.checkAddressInAccounts(v.Address) {
			return true, candidates
		}
	}
	return false, candidates
}

func (taskManager *DposTaskManager) checkNodeInAccounts(shuffleDel types.ShuffleDel) (bool, types.ProduceDelegate) {
	var pd types.ProduceDelegate
	for _, wallet := range taskManager.accountManager.Wallets() {
		for _, account := range wallet.Accounts() {
			address2 := account.Address.Hex()
			if strings.EqualFold(shuffleDel.Address, address2) {
				pd.Account = account.Address
				pd.Address = shuffleDel.Address
				pd.WorkTime = shuffleDel.WorkTime
				pd.NickName = shuffleDel.Nickname
				return true, pd
			}
		}
	}
	return false, pd
}

// check address whether exist in local node
func (taskManager *DposTaskManager) checkAddressInAccounts(address string) bool {
	for _, wallet := range taskManager.accountManager.Wallets() {
		for _, account := range wallet.Accounts() {
			if strings.EqualFold(account.Address.Hex(), address) {
				return true
			}
		}
	}
	return false
}

// get all top delegatePeers in current node
func (taskManager *DposTaskManager) GetLocalCurrentRound() []string {
	localDelegates := make([]string, 0)
	for _, v := range taskManager.currentNewRound.ShuffleDels {
	Loop:
		for _, wallet := range taskManager.accountManager.Wallets() {
			for _, account := range wallet.Accounts() {
				if strings.EqualFold(v.Address, account.Address.Hex()) {
					localDelegates = append(localDelegates, v.Address)
					break Loop
				}
			}
		}
	}
	return localDelegates
}

// get coinbase shuffle info by address, only use by delegate node
func (taskManager *DposTaskManager) GetCurrentDelegateByAddress(coinbase string) *types.ShuffleDel {
	for _, v := range taskManager.currentNewRound.ShuffleDels {
		if strings.EqualFold(v.Address, coinbase) {
			return &v
		}
	}
	return nil
}

func (taskManager *DposTaskManager) GetCurrentShuffleRound() *types.ShuffleList {
	return &taskManager.currentNewRound
}

// check address is in current top delegatePeers
func (taskManager *DposTaskManager) checkAddressInCurrentTopAndVerify(address string, sign []byte, blockHash string) bool {
	b := sha3.Sum256(common.FromHex(blockHash))
	pubkey, _ := secp256k1.RecoverPubkey(b[:], sign)
	pubAddress := crypto.PubkeyToAddress(*crypto.ToECDSAPub(pubkey)).Hex()
	for _, v := range taskManager.currentNewRound.ShuffleDels {
		if strings.EqualFold(v.Address, address) {
			if strings.EqualFold(pubAddress, address) {
				return true
			}
		}
	}
	log.Error("dposTaskManager verifySignature error", "publicAddress", pubAddress, "coinbaseAddress", address)
	return false
}

func createDelegateDB(ctx *node.ServiceContext) (aoadb.Database, error) {
	db, err := ctx.OpenDatabase(delegateStorePrefix, 0, 0)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (taskManager *DposTaskManager) readShuffleDataFromDB() (*types.ShuffleDelegateData, error) {
	var sdd types.ShuffleDelegateData
	data, err := taskManager.delegateStoredb.Get([]byte(delegateStorePrefix))
	if err != nil {
		return nil, err
	}
	err = rlp.DecodeBytes(data, &sdd)
	if err != nil {
		return nil, err
	}
	log.Info("read last shuffleBlockHeight from db", "blockNumber", sdd.BlockNumber.Int64())
	return &sdd, nil
}

func (taskManager *DposTaskManager) loadShuffleDataToDB(sdd types.ShuffleDelegateData) error {
	log.Info("dposTaskManager loadShuffle data begin", "data", sdd)
	data, err := rlp.EncodeToBytes(sdd)
	if err != nil {
		log.Error("dposTaskManager", "failed to encode data", err)
		return err
	}
	err = core.WriteDelegateShuffleBlockHeightRLP(taskManager.delegateStoredb, data)

	if err != nil {
		log.Errorf("dposTaskManager, failed to store shuffle block number, %v", err)
		return err
	}
	log.Infof("dposTaskManager|load delegate data to DB success, blockNumber=%v,shuffleTime=%v", sdd.BlockNumber.Int64(), sdd.ShuffleTime.Int64())
	return nil
}

// init shuffle data from level db and delegate db
func (taskManager *DposTaskManager) initShuffleDataFromLevelDB() error {
	sdd, err := taskManager.readShuffleDataFromDB()
	if err != nil {
		log.Errorf("dposTaskManager, fail to read shuffle data from db, %v", err)
		return err
	}
	block := taskManager.blockchain.GetBlockByNumber(sdd.BlockNumber.Uint64())
	delegatedb, err := taskManager.blockchain.DelegateStateAt(block.DelegateRoot())
	if err != nil {
		log.Error("dposTaskManager", "fail to get delegate state by block Number", err)
		return err
	}
	topDelegates := delegatedb.GetDelegates()
	if len(topDelegates) > maxElectDelegate {
		topDelegates = topDelegates[:maxElectDelegate]
	}
	log.Info("dposTaskManager read shuffle data from db success", "blockNumber", sdd.BlockNumber.Int64(), "shuffleTime", sdd.ShuffleTime.Int64())
	if len(topDelegates) == 0 {
		errMsg := "dposTaskManager doesn't have any delegatePeers,blockchain is stopping"
		log.Error(errMsg)
		return errors.New(errMsg)

	}
	shuffleNewRound := util.ShuffleNewRound(sdd.ShuffleTime.Int64(), maxElectDelegate, topDelegates, int64(blockInterval))
	shuffleList := types.ShuffleList{ShuffleDels: shuffleNewRound}
	taskManager.mu.Lock()
	defer taskManager.mu.Unlock()
	taskManager.currentRoundBlockHeight = sdd.BlockNumber.Int64()
	taskManager.currentNewRound = shuffleList
	rlpShuffleHash := rlpHash(shuffleList)
	taskManager.currentNewRoundHash = rlpShuffleHash
	taskManager.shuffleHashChan <- &types.ShuffleData{ShuffleHash: &rlpShuffleHash, ShuffleBlockNumber: &sdd.BlockNumber}
	return nil
}

// cal begin time of next round
func generateNextRoundBeginTime(genesisBlockTime int64) (int64, error) {
	// 当前ntp时间 +(一轮的总时间 - (当前ntp时间 - 创世块时间) % 一轮的总时间)
	currentTime := time.Now().Unix()
	roundTime := int64(maxElectDelegate * blockInterval)
	finishTime := (currentTime - genesisBlockTime) % int64(roundTime)
	return currentTime + (roundTime - finishTime), nil
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewLegacyKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}
