package types

import (
	"github.com/Aurorachain/go-Aurora/common"
	"math/big"
)

const (

	BlockInterval    = 10

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
	Vote         uint64 `json:"vote"`         
	Nickname     string `json:"nickname"`     
	RegisterTime uint64 `json:"registerTime"` 
}

type StoreData struct {
	Votes           []Candidate `json:"votes,omitempty"`
	LastBlockHeight uint64      `json:"lastBlockHeight"`
}

type CandidateSlice []Candidate

func (ca CandidateSlice) Len() int { 
	return len(ca)
}
func (ca CandidateSlice) Swap(i, j int) { 
	ca[i], ca[j] = ca[j], ca[i]
}
func (ca CandidateSlice) Less(i, j int) bool { 
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
	Vote     uint64 
	Nickname string 
	Action   int    
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

type CommitBlock struct {
	Address     common.Address
	Signs       []string
	BlockNumber uint64
	BlockHash   common.Hash
	Signature   []byte
}

type ShuffleDelegateData struct {
	BlockNumber big.Int
	ShuffleTime big.Int 
}

type VoteSign struct {
	Sign       []byte
	VoteAction uint64
}

type ShuffleData struct {
	ShuffleHash *common.Hash
	ShuffleBlockNumber *big.Int 
}
