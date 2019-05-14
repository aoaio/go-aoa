package core

import (
	"fmt"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/rlp"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var Upgrade_mgmt_address = "AOAa4077f583e4e79cb9d656af3e9214c7e3e46dec2"

const (
	Sha3_mgmt_vote_result        = "0xd2a99ad1ece9b6ed01ce2da14ef2b439d1bd38aaf0485cab021d5155ddbe17f8"
	Sha3_upgrade_vote_result     = "0xbd2da977db9a7f6550e7cb1cc4a453a32998b627fed9f1d8df432c390ad0a096"
	Sha3_upgrade_cancel          = "0xf9e4632d130f5fdbf29c4813de62f05ed8d8c83dcf319d2d3fd1b80c8b37f767"
	Sha3_vote_upgrade_attr_event = "0x3b52497122f6b039da6027f38e43c4e181d07dc4279379239bb56a39a0b08ebd"
)

var IsMgmtSet = false
//var aurora *aoa.Aurora

//func SetAoa( newAoa *aoa.Aurora){
//	aurora = newAoa
//}

func monitorUpgrade(tx types.Transaction, receipt *types.Receipt) {
	//TODO: remove it before release start
	if tx.TxDataAction() == 5 && !IsMgmtSet {
		Upgrade_mgmt_address = common.Address.AoaHex(receipt.ContractAddress)
		fmt.Println("aaaaa")
		fmt.Println(Upgrade_mgmt_address)
		IsMgmtSet = true
	}
	//TODO: remove it before release end

	if receipt.Status == 0 || receipt.Logs == nil || len(receipt.Logs) == 0 {
		return
	}
	switch *tx.To() {
	case common.HexToAddress(Upgrade_mgmt_address):
		if transferTopicToString(receipt.Logs[0].Topics[0]) == Sha3_mgmt_vote_result {
			topic := receipt.Logs[0].Topics[1]
			SetUpgradeContractAddress(common.FromHex(Upgrade_mgmt_address), transferTopicToAddress(topic))
		}
		return
	case LogicAddress:
		if len(receipt.Logs) == 2 {
			if transferTopicToString(receipt.Logs[0].Topics[0]) == Sha3_upgrade_vote_result && transferTopicToString(receipt.Logs[1].Topics[0]) == Sha3_vote_upgrade_attr_event {
				upgradeInfo := RequestInfo{
					//vote result event
					//event voteOrderIdAndUpgradeHeightEvent(uint indexed orderId, uint indexed upgradeHeight);
					//event voteUpgradeResultEvent(string indexed version, string indexed url, string indexed md5);
					transferTopicToString(receipt.Logs[0].Topics[1]),
					*tx.To(),
					transferTopicToString(receipt.Logs[1].Topics[1]),
					transferTopicToString(receipt.Logs[1].Topics[2]),
					receipt.Logs[0].Topics[2].Big().Uint64(),
					transferTopicToString(receipt.Logs[1].Topics[3]),
					1,
				}
				SetRequestInfo(LogicAddress.Bytes(), upgradeInfo)
			}
		} else if transferTopicToString(receipt.Logs[0].Topics[0]) == Sha3_upgrade_cancel {
			ClearUpgradeInfo(LogicAddress.Bytes())
		}
		return
	default:
		return
	}
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
	err := db.Put(key, contractAddress.Bytes())
	if err != nil {
		LogicAddress = contractAddress
	}
}

func SetRequestInfo(key []byte, requestInfo RequestInfo) {
	db := GetUpgradeDb()
	requestBytes, _ := rlp.EncodeToBytes(requestInfo)
	_ = db.Put(key, requestBytes)
}

func ClearUpgradeInfo(logicAddress []byte) {
	db := GetUpgradeDb()
	_ = db.Delete(logicAddress)
}

func GetRequestInfo(key []byte) RequestInfo {
	db := GetUpgradeDb()
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
	println("===== Upgrade download =====, call prepare_aoa.sh url=" + url + ", tag=" + tag)
	var startMod = "dev"
	runShell("./prepare_aoa.sh " + url + " " + tag + " " + startMod)
}

func isNewVersionHigher(requestInfo RequestInfo) bool {
	newVersion := requestInfo.Version
	curVersion := aoaVersion
	return strings.Compare(newVersion, curVersion) == 1
}

func getTagFromVersion(version string) string {
	return "release_" + strings.Replace(version, ".", "_", -1)
}

func DoUpgrade() {
	lock.Lock()
	defer lock.Unlock()
	LogicAddress := UpGetByString(Upgrade_mgmt_address)
	var requestInfo RequestInfo
	_ = rlp.DecodeBytes(UpGet(LogicAddress), &requestInfo)

	if !isNewVersionHigher(requestInfo) {
		return
	}

	status := requestInfo.Status
	if status == 0 {
		// nothing happen
	} else if status == 1 {
		// query upgrade contract address from Upgrade_mgmt_address
		prepareNewAoa(requestInfo.Url, getTagFromVersion(requestInfo.Version))
		requestInfo.Status = 2
		SetRequestInfo(LogicAddress, requestInfo)
	} else if status == 2 {
		upgradeHeight := requestInfo.Height
		currentHeight := GetCurrentHeight()
		if uint64(upgradeHeight) == currentHeight {
			//check new version
			new_aoa_dir := "/data/aoa/new"
			new_aoa_bin := ""
			if Exists(new_aoa_dir + "/" + new_aoa_bin) {
				//clear upgrade db
				ClearUpgradeInfo(LogicAddress)
				//start daemon script
				runShell("./aoad.sh")

			} else {
				fmt.Println("ERROR! new AOA binary doesn't exist.")
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

func runShell(shellCmd string) {
	exec.Command("/bin/bash", "-c", shellCmd)
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
