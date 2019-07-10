package core

import (
	"encoding/binary"
	"fmt"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/rlp"
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

var StartMode = ""

func monitorUpgrade(tx types.Transaction, receipt *types.Receipt) {
	defer func() { // 必须要先声明defer，否则不能捕获到panic异常
		if err := recover(); err != nil {
			log.Error("DoUpgrade, Error in monitorUpgrade, err=", err)
		}
	}()

	////TODO: remove it before release start
	//if tx.TxDataAction() == 5 && !IsMgmtSet {
	//	Upgrade_mgmt_address = common.Address.AoaHex(receipt.ContractAddress)
	//	IsMgmtSet = true
	//	log.Error("=====设置主合约地址为：" + Upgrade_mgmt_address)
	//}
	////TODO: remove it before release end
	if receipt.Status == 0 || receipt.Logs == nil || len(receipt.Logs) == 0 {
		//log.Error("DoUpGrade, receipt error.")
		return
	}
	switch *tx.To() {
	case common.HexToAddress(Upgrade_mgmt_address):
		log.Error("DoUpGrade, mgmt, receipt logs: %v ", receipt.Logs)
		log.Error("DoUpGrade,mgmt logs len = " + strconv.Itoa(len(receipt.Logs)))
		if len(receipt.Logs) == 2 {
			log.Error("DoUpGrade, Upgrade_mgmt_address1 logic address= \n" + transferTopicToString(receipt.Logs[1].Topics[0]))
			log.Error("DoUpGrade, Upgrade_mgmt_address2 \n" + transferTopicToString(receipt.Logs[1].Topics[1]))
			if transferTopicToString(receipt.Logs[1].Topics[0]) == Sha3_mgmt_vote_result {
				log.Error("DoUpgrade, *****Sha3_mgmt_vote_result")
				topic := receipt.Logs[1].Topics[2]
				SetUpgradeContractAddress(common.FromHex(Upgrade_mgmt_address), transferTopicToAddress(topic))
				log.Error("DoUpgrade, set upgrade logic contract=" + common.Address.String(LogicAddress) + ", key=" + Upgrade_mgmt_address)
			}
		}
		return
	case getLogicAddress():
		log.Debugf("DoUpgrade, upgrade logic event received! logic contract=%v", getLogicAddress().String())
		if len(receipt.Logs) == 2 {
			log.Error("DoUpGrade, Sha3_upgrade_vote_result1 \n" + transferTopicToString(receipt.Logs[1].Topics[0]))
			log.Error("DoUpGrade, Sha3_upgrade_vote_result2 \n" + transferTopicToString(receipt.Logs[1].Topics[1]))
			log.Error("DoUpgrade, logic vote logs len = " + strconv.Itoa(len(receipt.Logs)))
			if transferTopicToString(receipt.Logs[1].Topics[0]) == Sha3_upgrade_vote_result {
				log.Error("DoUpgrade, *****Sha3_upgrade_vote_result")
				originalBytes := receipt.Logs[1].Data[:]
				//fmt.Println("*****originalBytes=", originalBytes, "originalBytes len=", len(originalBytes))
				log.Errorf("DoUpGrade,升级信息原始完整string=" + string(originalBytes))
				offsetBytes := originalBytes[0:32]
				log.Errorf("DoUpGrade,*****offsetBytes=%v", offsetBytes)
				lenBytes := originalBytes[56:64]
				log.Errorf("DoUpGrade,*****lenBytes=%v", lenBytes)

				strSize := BytesToInt64(lenBytes)
				log.Errorf("DoUpGrade,*****strSize=%v", strSize)

				contentBytes := originalBytes[64 : 64+strSize]
				log.Errorf("*****contentBytes=%v", string(contentBytes))
				upinfo := string(contentBytes)
				version := strings.Split(upinfo, ";")[0]
				log.Error("DoUpGrade,升级信息截取的string=" + upinfo)
				log.Error("DoUpGrade,substring of version:" + version + ", len=" + strconv.Itoa(len(version)))
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
				SetRequestInfo(getLogicAddress().Bytes(), upgradeInfo)
				printUpdateInfo(upgradeInfo)
			}
		} else if transferTopicToString(receipt.Logs[0].Topics[0]) == Sha3_upgrade_cancel {
			log.Info("DoUpgrade, logic event is Sha3_upgrade_cancel")
			ClearUpgradeInfo(getLogicAddress().Bytes())
			log.Debug("clear upgrade info in database.")
		}
		return
	default:
		return
	}
}

func getLogicAddress() common.Address {
	logic := UpGet(common.FromHex(Upgrade_mgmt_address))
	if logic == nil {
		return LogicAddress
	}
	return common.BytesToAddress(logic)
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
	log.Info("DoUpgrade，set upgrade contract, key=" + Upgrade_mgmt_address + ", address of upgrade contract:" + common.Address(contractAddress).String())
	err := db.Put(key, contractAddress.Bytes())
	log.Info("DoUpgrade，set logic address 1: " + contractAddress.String())
	if err == nil {
		log.Info("DoUpgrade，set logic address 2: " + contractAddress.String())
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

func isUpgradeScheduleRun() bool {
	upgradeScheduleRun := string(UpGetByString("upgrade.schedule"))
	return upgradeScheduleRun == ""
}

func prepareNewAoa(url string, tag string) {
	//download aoa code from github and compile it to new AOA program
	log.Info("DoUpGrade, call prepare_aoa.sh url=" + url + ", tag=" + tag)
	var startPlace = "aoa"
	command := exec.Command("/bin/bash", "/etc/rc.d/init.d/prepare_aoa.sh", url, tag, startPlace, StartMode)
	err := command.Start()
	if err != nil {
		fmt.Println("*****, err=", err.Error())
	} else {
		fmt.Println("*****. err=nil")
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
	log.Errorf("upgrade info: status=%v, version=%v, targetHeight=%v, contractAddress=%v, orderId=%v, url=%v, md5=%v", info.Status, info.Version, info.Height, info.ContractAddress, info.OrderId, info.Url, info.Md5)
}

func DoUpgrade() {
	lock.Lock()
	defer lock.Unlock()
	LogicAddress := UpGet(common.FromHex(Upgrade_mgmt_address))
	log.Debugf("DoUpGrade, mgmt address = %v, logic address= %v ", Upgrade_mgmt_address, common.ToHex(LogicAddress))
	requestInfo := GetRequestInfo(LogicAddress)

	if !isNewVersionHigher(requestInfo) {
		log.Debugf("DoUpGrade, 现版本号 %v，requestInfo.version = %v", aoaVersion, requestInfo.Version)
		return
	}

	log.Info("Start aoa chain upgrade process.")

	status := requestInfo.Status
	fmt.Println(requestInfo)
	if status == 0 {
		// nothing happen
		//log.Error("DoUpGrade,定时任务, status=0")
	} else if status == 1 {
		// query upgrade contract address from Upgrade_mgmt_address
		log.Info("DoUpgrade, status=1, calling prepare_aoa.sh to download source code and compile aoa binary.")
		prepareNewAoa(requestInfo.Url, getTagFromVersion(requestInfo.Version))
		requestInfo.Status = 2
		SetRequestInfo(LogicAddress, requestInfo)
	} else if status == 2 {
		log.Info("DoUpgrade, status=2")
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
				log.Info("DoUpgrade, calling aoad.sh")
				command := exec.Command("/bin/bash", "/etc/rc.d/init.d/aoad.sh")
				err := command.Start()
				if err != nil {
					fmt.Println("DoUpgrade, err=", err.Error())
				}

			} else {
				log.Info("DoUpGrade,ERROR! new AOA binary doesn't exist.")
				prepareNewAoa(requestInfo.Url, getTagFromVersion(requestInfo.Version))
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

func UpgradeTimer(timer func()) {
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
