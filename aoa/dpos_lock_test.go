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
	"crypto/ecdsa"
	"fmt"
	"github.com/Aurorachain/go-aoa/accounts"
	"github.com/Aurorachain/go-aoa/accounts/keystore"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/crypto"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNew(t *testing.T) {

	scryptN := keystore.StandardScryptN
	scryptP := keystore.StandardScryptP
	keydir := "/Users/name/abtc-workspace/chain-dev/node3/keystore"
	backends := []accounts.Backend{
		keystore.NewKeyStore(keydir, scryptN, scryptP),
	}
	accountManager := accounts.NewManager(backends...)
	delegateWallets := make(map[string]*ecdsa.PrivateKey, 0)

	addresses := make([]common.Address, 0)

	for _, v := range accountManager.Wallets() {
		addresses = append(addresses, v.Accounts()[0].Address)
	}

	d := 300 * time.Second
	password := "name"

	shuffleDels := make([]types.ShuffleDel, 0, len(addresses))
	currentShuffleRound := &types.ShuffleList{ShuffleDels: shuffleDels}
	for _, v := range addresses {
		account := accounts.Account{Address: v}
		err := fetchKeystore(accountManager).TimedUnlock(account, password, d)
		if err != nil {
			continue
		}
		address := strings.ToLower(v.Hex())
		keyJson, err := fetchKeystore(accountManager).Export(account, password, password)
		if err != nil {
			continue
		}
		key, err := keystore.DecryptKey(keyJson, password)
		if err != nil {
			continue
		}
		delegateWallets[address] = key.PrivateKey
		currentShuffleRound.ShuffleDels = append(currentShuffleRound.ShuffleDels, types.ShuffleDel{Address: address})
	}
	fmt.Println("==============load data success============================")

	dposLockManager := newBlockLockManager(nil, delegateWallets)

	header := &types.Header{
		ParentHash: common.HexToHash("0xd2e91d3554d254eb6a3db17ea03bc8d2af305eab483a777a23fd7181ba29b563"),
		Number:     common.Big1,
		GasLimit:   25000,
		Extra:      nil,
		Time:       big.NewInt(1526442558),
		Coinbase:   common.HexToAddress("0x8888f1f195afa192cfee860698584c030f4c9db1"),
		AgentName:  nil,
	}
	block := types.NewBlock(header, nil, nil)

	err := dposLockManager.addPendingBlock(block)
	if err != nil {
		t.Fatalf("add pending block error:%v", err)
	}

	startTime := time.Now().Unix()
	signVotes := make([]types.VoteSign, 0, len(addresses))
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(addresses))
	fmt.Printf("addPendingBlockAndSign|AddSignStart blockNumber:%d startTime:%d\n", block.NumberU64(), startTime)
	action := approveVote
	for _, v := range addresses {
		go func(address string) {
			defer waitGroup.Done()
			tempStart := time.Now().Nanosecond()
			fmt.Printf("addSign|gofunc|start address:%s\n", address)
			address = strings.ToLower(address)
			if _, ok := delegateWallets[address]; !ok {
				return
			}
			privateKey := delegateWallets[address]
			sign, err := crypto.Sign(block.Hash().Bytes()[:32], privateKey)
			if err != nil {
				return
			}
			signVotes = append(signVotes, types.VoteSign{Sign: sign, VoteAction: action})
			dposLockManager.addSignToPendingBlock(block.Hash(), sign, action, currentShuffleRound)

			fmt.Printf("addSign|gofunc|end address:%s cost time(ns):%d\n", address, time.Now().Nanosecond()-tempStart)
		}(v.Hex())
	}
	waitGroup.Wait()
	fmt.Printf("addPendingBlockAndSign finish cost time(s):%d\n", time.Now().Unix()-startTime)
}

// fetchKeystore retrives the encrypted keystore from the account manager.
func fetchKeystore(am *accounts.Manager) *keystore.KeyStore {
	return am.Backends(keystore.KeyStoreType)[0].(*keystore.KeyStore)
}
