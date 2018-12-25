// +build none

package main

import (
	"fmt"
	"github.com/Aurorachain/go-Aurora/core/types"
	"strconv"
	"github.com/Aurorachain/go-Aurora/rlp"
)

type genesisAgents []types.Candidate

const (
	firstDelegateVoteNumber  = uint64(1000000000)
	secondDelegateVoteNumber = uint64(300000000)
	thirdDelegateVoteNumber  = uint64(100000000)
	fourthDelegateVoteNumber = uint64(10000000)
	fifthDelegateVoteNumber  = uint64(5000000)
	sixthDelegateVoteNumber  = uint64(1000000)
)

func main() {
	var list genesisAgents
	candidateList := mainNetAgents()
	list = append(list, candidateList...)

	data, err := rlp.EncodeToBytes(list)
	if err != nil {
		panic(err)
	}
	result := strconv.QuoteToASCII(string(data))
	fmt.Println("const agentData =", result)
}

func mainNetAgents() []types.Candidate {
	return []types.Candidate{
	}
}

func mainTestNetAgents() []types.Candidate {
	candidateList := []types.Candidate{
	}
	return candidateList
}
