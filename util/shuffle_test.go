package util

import (
	"fmt"
	"github.com/Aurorachain/go-Aurora/core/types"
	"testing"
	"time"
)

func TestShuffle(t *testing.T) {
	var delegateNumber = 4
	for i := 1; i < 100; i++ {
		fmt.Println(Shuffle(int64(i), delegateNumber))
		if i%delegateNumber == 0 {
			fmt.Println("=======================")
		}
	}
}

func TestShuffleNewRound(t *testing.T) {
	var initDelegate = []types.Candidate{
		{Address: "0x70715a2a44255ddce2779d60ba95968b770fc751", Nickname: "node1"},
		{Address: "0xfd48a829397a16b3bc6c319a06a47cd2ce6b3f52", Nickname: "node2"},
		{Address: "0x612d018cc7db4137366a08075333a634c07e31b3", Nickname: "node3"},
		{Address: "0x612d018cc7db4137366a08075333a634c07e31b4", Nickname: "node4"},
		{Address: "0x612d018cc7db4137366a08075333a634c07e31b5", Nickname: "node5"},
		{Address: "0x612d018cc7db4137366a08075333a634c07e31b6", Nickname: "node6"},
		{Address: "0x612d018cc7db4137366a08075333a634c07e31b7", Nickname: "node7"},
		{Address: "0x612d018cc7db4137366a08075333a634c07e31b8", Nickname: "node8"},
		{Address: "0x612d018cc7db4137366a08075333a634c07e31b9", Nickname: "node9"},
		{Address: "0x612d018cc7db4137366a08075333a634c07e31b0", Nickname: "node10"},
	}

	lastBlockTime := time.Now().Unix()
	newRound := ShuffleNewRound(lastBlockTime, 10, initDelegate)
	for _, v := range newRound {
		fmt.Println(v)
	}

}

func TestCalShuffleTimeByHeaderTime(t *testing.T) {

	shuffleTime := CalShuffleTimeByHeaderTime(3030, 2040)
	fmt.Println(shuffleTime)

}
