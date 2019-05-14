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

package types

import (
	"github.com/Aurorachain/go-aoa/common"
	"math/big"
)

const (
	// MaxElectDelegate = 101
	BlockInterval = 10
	// DelegateAmount   = (MaxElectDelegate / 3) * 2
)

const DelegatePrefix = "aurora-delegates"

type ShuffleDel struct {
	WorkTime uint64 `json:"work_time"  gencodec:"required"`
	Address  string `json:"address"  gencodec:"required"`
	Vote     uint64 `json:"vote"  gencodec:"required"`
	Nickname string `json:"nickname"  gencodec:"required"`
}

type ShuffleList struct {
	ShuffleDels []ShuffleDel `json:"shuffle_dels" gencodec:"required"`
}

type Candidate struct {
	Address      string `json:"address"`
	Vote         uint64 `json:"vote"`         // 投票数
	Nickname     string `json:"nickname"`     // delegate name
	RegisterTime uint64 `json:"registerTime"` // 注册时间
}

type StoreData struct {
	Votes           []Candidate `json:"votes,omitempty"`
	LastBlockHeight uint64      `json:"lastBlockHeight"`
}

type CandidateSlice []Candidate

func (ca CandidateSlice) Len() int { // 重写 Len() 方法
	return len(ca)
}
func (ca CandidateSlice) Swap(i, j int) { // 重写 Swap() 方法
	ca[i], ca[j] = ca[j], ca[i]
}
func (ca CandidateSlice) Less(i, j int) bool { // 重写 Less() 方法， vote从大到小排序,如果相等按注册时间从小到大排序，最后按address排序
	if ca[j].Vote != ca[i].Vote {
		return ca[j].Vote < ca[i].Vote
	}
	if ca[j].RegisterTime != ca[i].RegisterTime {
		return ca[j].RegisterTime > ca[i].RegisterTime
	}
	return ca[j].Address < ca[i].Address
}

type VoteCandidate struct {
	Address  string
	Vote     uint64 //投票数
	Nickname string // delegate name
	Action   int    // 0-register,1-add vote,2-sub vote,3-cancel delegate
}

type CandidateWrapper struct {
	Candidates  []VoteCandidate
	BlockHeight int64
	BlockTime   int64
}

type ProduceDelegate struct {
	Address  string
	Account  common.Address
	WorkTime uint64
	NickName string
}

type BlockPreConfirmChan struct {
	Sign         string
	SignAddress  string
	BlockHashHex string
	BlockNumber  uint64
	TimeStamp    int64
}

type BlockPreConfirm struct {
	BlockPreConfirmChan chan *BlockPreConfirmChan
	Block               *Block
}

// commit block broadcast struct
type CommitBlock struct {
	Address     common.Address
	Signs       []string
	BlockNumber uint64
	BlockHash   common.Hash
	Signature   []byte
}

type ShuffleDelegateData struct {
	BlockNumber big.Int
	ShuffleTime big.Int //ShuffleTime
}

type VoteSign struct {
	Sign       []byte
	VoteAction uint64
}

type ShuffleData struct {
	ShuffleHash        *common.Hash
	ShuffleBlockNumber *big.Int
}
