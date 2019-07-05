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

package core

import (
	"bytes"
	"container/heap"
	"encoding/json"
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/common/hexutil"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/metrics"
	"github.com/Aurorachain/go-aoa/rlp"
	"io"
	"io/ioutil"
	"math/big"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

const (
	//url = "http://10.23.1.112:8989"
	url     = "http://127.0.0.1:8545"
	jsonrpc = "2.0"
)

type Resp struct {
	Jsonrpc string      `json:"jsonrpc"`
	Id      int         `json:"id"`
	Error   string      `json:"error,omitempty"`
	Result  interface{} `json:"result"`
}

type trx struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Value  string `json:"value"`
	Action uint   `json:"action"`
}

type fulltrx struct {
	From     string `json:"from"`
	To       string `json:"to,omitempty"`
	Gas      string `json:"gas,omitempty"`
	GasPrice string `json:"gasPrice,omitempty"`
	Value    string `json:"value,omitempty"`
	Data     string `json:"data,omitempty"`
}

type rpcRes struct {
	Jsonrpc string      `json:"jsonrpc"`
	Id      int         `json:"id"`
	Result  interface{} `json:"result"`
}

var wg sync.WaitGroup

func TestNewTxPool(t *testing.T) {
	runtime.GOMAXPROCS(4)
	//result, err := netPeerCount(2)
	//if err == nil {
	//	fmt.Println(result)
	//}else{
	//	fmt.Errorf("error:%v",err)
	//}

	var trx trx
	from := []string{"0x0ac71830f52bda2046583d7cb2df07855922f74a",
		"0x793fae82c7a0e21fcd69e94b2d7403257422a255",
		"0x105376386c77adcf23df75505fc27fcb626ad611",
		"0xb06e33bc647f40d0ba3bddb471f9af8f9e2c6871",
		"0xa6a3e1d6ecea6bf644aefa684c15d7033876c5fe",
		"0xbc8276fca9a6be4967ea854bdee9c9aec0595c12",
		"0x4aef0f9045b5adc395260d8bd04b727471aa59f1",
		"0x8f20cc9d5edf64f3b8e7d25830f1599628465b0e",
		"0x3b106d19b75d52c9d9e1391039a7df7a2d585733"}

	start := time.Now()
	count := 0
	for {
		for i, txFrom := range from {
			trx.From = txFrom
			for k := 0; k < 32; k++ {
				for j, txTo := range from {
					if i == j {
						continue
					} else {
						trx.To = txTo
					}
					value := 10000000000000000 // 1 AOA
					value += count
					trx.Value = fmt.Sprintf("%#x", value)
					trx.Action = 0
					wg.Add(1)
					go aoaSendTransacion(1, trx)
					count++
				}
			}
		}
		wg.Wait()
		end := time.Now()
		useTime := end.Sub(start).Seconds()
		fmt.Println("transaction a second", useTime/float64(count))
	}
}
func aoaSendTransacion(id int, trx trx) {
	reqMap := make(map[string]interface{})
	reqMap["jsonrpc"] = jsonrpc
	reqMap["method"] = "aoa_sendTransaction"
	reqMap["id"] = id
	params := make([]interface{}, 1)
	params[0] = trx
	reqMap["params"] = params
	result, err := commonRequest(reqMap)
	if err == nil {
		fmt.Println(result)
	} else {
		//time.Sleep(10 * time.Second)
		fmt.Errorf("error:%v", err)
	}
	wg.Done()
	return
}

func TestTxPool_AddRemote(t *testing.T) {
	timeStamp, err := strconv.ParseInt("0x5abca2b4", 0, 64)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(timeStamp)
	a, _ := new(big.Int).SetString("2998000000000000000000000000", 10)
	b := fmt.Sprintf("%#x", a)
	fmt.Println(b)
	testCounter := metrics.NewTransactionCounter("txpool/normalTx")
	fmt.Println("counter " + strconv.Itoa(int(testCounter.Count())))
	fmt.Println(reflect.TypeOf(testCounter))
	testCounter.Inc(int64(1))
	fmt.Println("counter " + strconv.Itoa(int(testCounter.Count())))
	testCounter.Inc(int64(1))
	fmt.Println("counter " + strconv.Itoa(int(testCounter.Count())))
	testCounter.Dec(int64(1))
	fmt.Println("counter " + strconv.Itoa(int(testCounter.Count())))
}

func TestContractCallBench(t *testing.T) {
	strFuncSig := "insert(uint256,uint256)"
	sha3FuncSig := crypto.Keccak256([]byte(strFuncSig))

	var kbase *big.Int = new(big.Int)
	kbase.SetUint64(1)

	var wg sync.WaitGroup
	for i := 1; i < 3; i++ {
		wg.Add(1)
		go func(x int64) {
			datas := make([]byte, 68)
			copy(datas[0:4], sha3FuncSig[:4])

			var k common.Hash = common.BigToHash(kbase.Add(kbase, new(big.Int).SetInt64(x)))
			//让 v == k ，直接copy到结果
			copy(datas[4:36], k.Bytes())
			copy(datas[36:], k.Bytes())
			data := hexutil.Encode(datas)
			fmt.Println(data)

			var trx fulltrx
			trx.From = "0xdefee9edbf3a6da3a5bb96d006b86ac884d14f64"
			trx.To = "0x108c73bd4d3e2936de4125ad201bd89b47135929" //合约地址
			trx.Gas = "0x1000000"

			trx.Data = data
			str, err := AoaSendTransaction(int(x), trx)
			fmt.Printf("str: %s \tErr: %v \n", str, err)
			wg.Done()
		}(int64(i))
	}
	wg.Wait()
	fmt.Println("All Done.")
}

func AoaSendTransaction(id int, trx fulltrx) (string, error) {
	reqMap := make(map[string]interface{})
	reqMap["jsonrpc"] = jsonrpc
	reqMap["method"] = "aoa_sendTransaction"
	reqMap["id"] = id
	params := make([]interface{}, 1)
	params[0] = trx
	reqMap["params"] = params
	return commonRequest(reqMap)
}

func commonRequest(requestMap map[string]interface{}) (string, error) {
	reqBytes, err := json.Marshal(requestMap)
	if err != nil {
		fmt.Errorf("req json error:%v\n", err)
		return "", err
	}
	reader := bytes.NewReader(reqBytes)
	request, err := http.NewRequest("POST", url, reader)
	if err != nil {
		fmt.Errorf("create request  error:%v\n", err)
		return "", err
	}
	request.Header.Set("Content-Type", "application/json;charset=UTF-8")
	client := http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		fmt.Errorf("response  error:%v\n", err)
		return "", err
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err.Error())
		return "", err
	}
	resultResp := (*string)(unsafe.Pointer(&respBytes))
	resp.Body.Close()
	return *resultResp, nil
}

func TestCountBlockVote(t *testing.T) {
	result, err := ioutil.ReadFile("D:\\aoa-test.txt")
	if err != nil {
		fmt.Println(err)
		return
	}
	addressList := string(result)
	addressL := strings.Split(addressList, "\n")
	add := make(map[string]interface{}, 0)
	for _, address := range addressL {
		if strings.Contains(address, "0x") {
			without0x := address[2:]
			balance := make(map[string]string, 1)
			balance["balance"] = "0x1a784379d99db42000000"
			add[without0x] = balance
		}
	}
	x, _ := json.Marshal(add)
	file, _ := os.Create("D:\\1.txt")
	n, _ := io.WriteString(file, string(x))
	fmt.Println(n)
}

var contractAddress = "0x4e53a8016a8b25d0740b3ecfe575e769b5509b2f"

func TestDeleteBody(t *testing.T) {
	startBlock := new(big.Int).SetInt64(int64(245))
	for {
		normal := 0
		contract := 0
		b := fmt.Sprintf("%#x", startBlock)
		res, err := aoaGetBlock(1, b)
		if err != nil {
			fmt.Println(err)
			return
		}
		//fmt.Println(res)
		result := new(rpcRes)
		err = json.Unmarshal([]byte(res), result)
		if err != nil {
			fmt.Println(err)
			return
		}
		block := result.Result.(map[string]interface{})
		transactions := block["transactions"].([]interface{})
		timestamp := block["timestamp"].(string)
		timeStamp, err := strconv.ParseInt(timestamp, 0, 64)
		if err != nil {
			fmt.Println("timestamp error", err)
			return
		}
		for _, tx := range transactions {
			transaction := tx.(string)
			r, err := aoaGetTransaction(1, transaction)
			if err != nil {
				fmt.Println(err)
				return
			}
			txRes := new(rpcRes)
			err = json.Unmarshal([]byte(r), txRes)
			if err != nil {
				fmt.Println(err)
				return
			}
			trx := txRes.Result.(map[string]interface{})
			to := trx["to"].(string)
			if strings.ToLower(to) == contractAddress {
				contract++
			} else {
				normal++
			}
		}
		fmt.Println("block num:", startBlock, "timestamp", timeStamp, "normal num", normal, "contract num", contract)
		startBlock.Add(startBlock, new(big.Int).SetInt64(int64(1)))
	}
}

func aoaGetBlock(id int, blockNum string) (string, error) {
	reqMap := make(map[string]interface{})
	reqMap["jsonrpc"] = jsonrpc
	reqMap["method"] = "aoa_getBlockByNumber"
	reqMap["id"] = id
	params := make([]interface{}, 2)
	params[0] = blockNum
	params[1] = false
	reqMap["params"] = params
	return commonRequest(reqMap)
}

func aoaGetTransaction(id int, txHash string) (string, error) {
	reqMap := make(map[string]interface{})
	reqMap["jsonrpc"] = jsonrpc
	reqMap["method"] = "aoa_getTransactionByHash"
	reqMap["id"] = id
	params := make([]interface{}, 1)
	params[0] = txHash
	reqMap["params"] = params
	return commonRequest(reqMap)
}

type shulie []int

func (s *shulie) Push(x interface{}) {
	*s = append(*s, x.(int))
}

func (s *shulie) Pop() interface{} {
	old := *s
	n := len(old)
	x := old[n-1]
	*s = old[0 : n-1]
	return x
}

func (s shulie) Len() int {
	return len(s)
}

func (s shulie) Less(i, j int) bool {
	return s[i] < s[j]
}

func (s shulie) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func TestAddresssByHeartbeat_Len(t *testing.T) {
	ss := &shulie{13, 1, 7, 12, 3, 7, 10, 8}
	fmt.Println(ss)
	heap.Init(ss)
	fmt.Println(ss)
	sort.Sort(ss)
	fmt.Println(ss)
}

func TestGetTransaction(t *testing.T) {
	s := "aoalxyz111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111黎"
	fmt.Println(len(s))
}

func TestAddresssByHeartbeat_Less(t *testing.T) {
	a := "0x4dc92f64342f6a77e30c24d6008637bdfe0cd8526313907c65fbdad4e0f46741"
	x := strings.Replace(a, "0x", "AOA", -1)
	fmt.Println(x)
	b := "AOA4dc92f64342f6a77e30c24d6008637bdfe0cd8526313907c65fbdad4e0f46741"
	c := strings.Replace(b, "0x", "AOA", -1)
	fmt.Println(c)
}

func TestDecode(t *testing.T) {
	tx := new(types.Transaction)
	code := "0xf8748084ee6b280083015f90944840948a2b1322baaa3f83df1aeba51a0e6d17608906a024aa73603dfb8f80808080808080801ca0cfe98244b890a3360e36cad1d3d1a6b9ff2dad2be1049809c397c7bcbc59b14da0337724be4e0a81963422b2a3f7f9d8129a81818a5713e344907eb1e3b65d09b5"
	decode, _ := hexutil.Decode(code)
	_ = rlp.DecodeBytes(decode, tx)
	fmt.Println(tx)
}
