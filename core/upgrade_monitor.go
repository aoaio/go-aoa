package core

import (
	"encoding/binary"
	"fmt"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/rlp"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var Upgrade_mgmt_address = "AOA38961110cf484bf534813be9a51c135a6f5d629f"

const (
	Sha3_mgmt_vote_result        = "0x720becaeed8504f32ada91bf2766ae87265165ae7e015ba48c54b26bc21b7dfc"
	Sha3_upgrade_vote_result     = "0x8dd5d825b18e5b987c21d6fe8f353ca17e8ae91065ffcfa3309d8852240844f4"
	Sha3_vote_upgrade_attr_event = "0x98d9d2655d697c2872d9ab58576f2e8d5e2ebee133091a346d20fb954df7bd8f"
	Sha3_upgrade_cancel          = "0xf9e4632d130f5fdbf29c4813de62f05ed8d8c83dcf319d2d3fd1b80c8b37f767"
)

var IsMgmtSet = false

var StartMode = ""

//var aurora *aoa.Aurora

//func SetAoa( newAoa *aoa.Aurora){
//	aurora = newAoa
//}

func monitorUpgrade(tx types.Transaction, receipt *types.Receipt) {
	defer func() { // 必须要先声明defer，否则不能捕获到panic异常
		if err := recover(); err != nil {
			log.Errorf("DoUpgrade, Error in monitorUpgrade, err=%v", err)
		}
	}()

	if receipt.Status == 0 || receipt.Logs == nil || len(receipt.Logs) == 0 {
		log.Error("DoUpGrade, receipt error.")
		return
	}
	switch *tx.To() {
	case common.HexToAddress(Upgrade_mgmt_address):
		log.Debugf("DoUpgrade, mgmt event received! mgmt contract=%v", Upgrade_mgmt_address)
		if len(receipt.Logs) == 2 {
			if transferTopicToString(receipt.Logs[1].Topics[0]) == Sha3_mgmt_vote_result {
				log.Info("DoUpgrade, mgmt event is Sha3_mgmt_vote_result")
				topic := receipt.Logs[1].Topics[2]
				SetUpgradeContractAddress(common.FromHex(Upgrade_mgmt_address), transferTopicToAddress(topic))
				log.Info("DoUpgrade, set upgrade logic contract=%v,  mgmt contract=%v", common.Address.String(LogicAddress), Upgrade_mgmt_address)
			}
		}
		return
	case LogicAddress:
		log.Debugf("DoUpgrade, upgrade logic event received! logic contract=%v", common.Address.String(LogicAddress))
		if len(receipt.Logs) == 2 {
			if transferTopicToString(receipt.Logs[1].Topics[0]) == Sha3_upgrade_vote_result {
				log.Info("DoUpgrade, logic event is Sha3_upgrade_vote_result")
				originalBytes := receipt.Logs[1].Data[:]
				log.Debugf("DoUpGrade, data of logic upgrade vote event result, string=%v", string(originalBytes))
				lenBytes := originalBytes[56:64]
				strSize := BytesToInt64(lenBytes)
				contentBytes := originalBytes[64 : 64+strSize]
				upinfo := string(contentBytes)
				version := strings.Split(upinfo, ";")[0]
				url := strings.Split(upinfo, ";")[1]
				md5 := strings.Split(upinfo, ";")[2]
				upgradeInfo := RequestInfo{
					//vote result event
					//event VoteUpgradeResultEvent(uint indexed orderId, uint indexed upgradeHeight, string upgradeInfo);
					transferTopicToString(receipt.Logs[1].Topics[1]),
					*tx.To(),
					version,
					url,
					receipt.Logs[1].Topics[2].Big().Uint64(),
					md5,
					1,
				}
				SetRequestInfo(LogicAddress.Bytes(), upgradeInfo)
				printUpdateInfo(upgradeInfo)
			}
		} else if transferTopicToString(receipt.Logs[0].Topics[0]) == Sha3_upgrade_cancel {
			log.Info("DoUpgrade, logic event is Sha3_upgrade_cancel")
			ClearUpgradeInfo(LogicAddress.Bytes())
			log.Debug("clear upgrade info in database.")
		}
		return
	default:
		return
	}
}

func Int64ToBytes(i int64) []byte {
	var buf = make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(i))
	return buf
}

func BytesToInt64(buf []byte) int64 {
	return int64(binary.BigEndian.Uint64(buf))
}

//处理topic到Address
func transferTopicToAddress(topic common.Hash) common.Address {
	data := make([]byte, 20)
	copy(data, topic[len(topic)-20:])
	return common.BytesToAddress(data)
}

//topic到string
func transferTopicToString(topic common.Hash) string {
	return common.ToHex(topic[:])
}

var LogicAddress = common.HexToAddress("0x0")

var lock sync.Mutex

var curHeight uint64

var UpgradeDb aoadb.Database

func SetUpgradeContractAddress(key []byte, contractAddress common.Address) {
	db := GetUpgradeDb()
	log.Info("DoUpgrade，SetUpgradeContractAddress")
	err := db.Put(key, contractAddress.Bytes())
	if err == nil {
		log.Infof("DoUpgrade，set logic address %v to mgmt address %v.", contractAddress.String(), Upgrade_mgmt_address)
		LogicAddress = contractAddress
	}
}

func SetRequestInfo(key []byte, requestInfo RequestInfo) {
	db := GetUpgradeDb()
	requestBytes, _ := rlp.EncodeToBytes(requestInfo)
	_ = db.Put(key, requestBytes)
	log.Info("DoUpgrade，save upgrade info, version=" + requestInfo.Version + ", url=" + requestInfo.Url + ", md5=" + requestInfo.Md5 + ", height=" + strconv.Itoa(int(requestInfo.Height)))
}

func ClearUpgradeInfo(logicAddress []byte) {
	db := GetUpgradeDb()
	_ = db.Delete(logicAddress)
}

func GetRequestInfo(key []byte) RequestInfo {
	db := GetUpgradeDb()
	//log.Error("DoUpgrade，get upgrade info，key=" + common.Bytes2Hex(key))
	value, _ := db.Get(key)
	var requestInfo RequestInfo
	_ = rlp.DecodeBytes(value, &requestInfo)
	return requestInfo
}

type RequestInfo struct {
	OrderId         string
	ContractAddress common.Address
	Version         string
	Url             string
	Height          uint64
	Md5             string
	Status          uint
}

func SetUpgradeDb(upDb aoadb.Database) {
	UpgradeDb = upDb
}

//func InitUpgradeDb(datadir string, databaseCache int, databaseHandles int) *aoadb.LDBDatabase {
//	if UpgradeDb == nil {
//		UpgradeDb, _ = aoadb.NewLDBDatabase( datadir, databaseCache, databaseHandles)
//	}
//	return UpgradeDb
//}

func GetUpgradeDb() aoadb.Database {
	return UpgradeDb
}

func UpPutString(key string, value string) {
	db := GetUpgradeDb()
	_ = db.Put([]byte(key), []byte(value))
}

func UpPut(key []byte, value []byte) {
	db := GetUpgradeDb()
	_ = db.Put(key, value)
}

func UpGetByString(key string) []byte {
	value, _ := GetUpgradeDb().Get([]byte(key))
	return value
}

func UpGet(key []byte) []byte {
	value, _ := GetUpgradeDb().Get(key)
	return value
}

func prepareNewAoa(url string, tag string, md5 string) {
	//download aoa code from github and compile it to new AOA program
	log.Infof("DoUpGrade, call prepare_aoa.sh from aoa chain, url=%v, tag=%v, md5=%v", url, tag, md5)
	var startPlace = "aoa"
	command := exec.Command("/bin/bash", "/etc/rc.d/init.d/prepare_aoa.sh", url, tag, startPlace, StartMode, md5)
	err := command.Start()
	if err != nil {
		log.Errorf("DoUpgrade, error in prepareNewAoa, err=%v", err.Error())
	}
}

func isNewVersionHigher(requestInfo RequestInfo) bool {
	newVersion := requestInfo.Version
	curVersion := aoaVersion
	return strings.Compare(newVersion, curVersion) == 1
}

func getTagFromVersion(version string) string {
	return "release_" + strings.Replace(version, ".", "_", -1)
}

func printUpdateInfo(info RequestInfo) {
	log.Debugf("upgrade info: status=%v, version=%v, targetHeight=%v, contractAddress=%v, orderId=%v, url=%v, md5=%v", info.Status, info.Version, info.Height, info.ContractAddress, info.OrderId, info.Url, info.Md5)
}

func DoUpgrade() {
	lock.Lock()
	defer lock.Unlock()
	LogicAddress := UpGet(common.FromHex(Upgrade_mgmt_address))
	log.Debugf("DoUpGrade, mgmt address = %v, logic address= %v ", Upgrade_mgmt_address, common.ToHex(LogicAddress))
	requestInfo := GetRequestInfo(LogicAddress)

	if !isNewVersionHigher(requestInfo) {
		log.Debugf("DoUpGrade, current version= %v，upgrade target version= %v, will not upgrade.", aoaVersion, requestInfo.Version)
		return
	}

	status := requestInfo.Status
	fmt.Println(requestInfo)
	if status == 0 {
		// nothing happen, initial status
	} else if status == 1 {
		// query upgrade contract address from Upgrade_mgmt_address
		log.Info("DoUpgrade, upgrade status=1, calling prepare_aoa.sh to download source code and compile aoa binary. set upgrade status=2")
		prepareNewAoa(requestInfo.Url, getTagFromVersion(requestInfo.Version), requestInfo.Md5)
		requestInfo.Status = 2
		SetRequestInfo(LogicAddress, requestInfo)
	} else if status == 2 {
		log.Info("DoUpgrade, upgrade status=2, try to check aoa binary in /home/aoachain/new")
		upgradeHeight := requestInfo.Height
		currentHeight := GetCurrentHeight()
		log.Infof("DoUpgrade, target block height=%v, current block height=%v", upgradeHeight, currentHeight)
		if uint64(upgradeHeight) <= currentHeight {
			//check new version
			new_aoa := "/home/aoachain/new/aoa"
			if Exists(new_aoa) {
				//clear upgrade db
				ClearUpgradeInfo(LogicAddress)
				//start daemon script
				log.Info("DoUpgrade, aoa binary is redeay, call aoad.sh to stop aoa chain process and start the new version aoa chain.")
				command := exec.Command("/bin/bash", "/etc/rc.d/init.d/aoad.sh")
				err := command.Start()
				if err != nil {
					log.Errorf("DoUpgrade, call aoad.sh failed, err=%v", err.Error())
				}

			} else {
				log.Info("DoUpGrade, AOA binary doesn't exist in /home/aoachain/new, try to call prepare_aoa.sh.")
				prepareNewAoa(requestInfo.Url, getTagFromVersion(requestInfo.Version), requestInfo.Md5)
			}

		}
	}
}
func SetCurrentHeight(curHeightValue uint64) {
	curHeight = curHeightValue
}
func GetCurrentHeight() uint64 {

	return curHeight
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		return false
	}
	return true
}

func UpgradeTimer(curHeightValue *big.Int, timer func()) {
	curHeight = curHeightValue.Uint64()
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				timer()
			}
		}
	}()
}
