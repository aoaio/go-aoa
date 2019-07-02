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

package util

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/log"
	"math"
	"strconv"
	"time"
)

// dpos乱序方法,同一个轮回里面传入不同的高度，排序列表是一样的
// delegateNumber:代理节点数量
// height:本次产块的块高
func Shuffle(height int64, delegateNumber int) []int {
	var truncDelegateList []int

	for i := 0; i < delegateNumber; i++ {
		truncDelegateList = append(truncDelegateList, i)
	}

	seed := math.Floor(float64(height / int64(delegateNumber)))
	//seed := strconv.FormatFloat(math.Floor(float64(height/101)), 'E', -1, 64)

	if height%int64(delegateNumber) > 0 {
		seed += 1
	}
	seedSource := strconv.FormatFloat(seed, 'E', -1, 64)
	var buf bytes.Buffer
	buf.WriteString(seedSource)
	hash := sha256.New()
	hash.Write(buf.Bytes())
	md := hash.Sum(nil)
	currentSend := hex.EncodeToString(md)

	delCount := len(truncDelegateList)
	for i := 0; i < delCount; i++ {
		for x := 0; x < 4 && i < delCount; i++ {
			newIndex := int(currentSend[x]) % delCount
			// 元素互换
			truncDelegateList[newIndex], truncDelegateList[i] = truncDelegateList[i], truncDelegateList[newIndex]
			x++
		}
	}
	return truncDelegateList

}

// beginTime: current shuffle time
// delegateNumber: Max delegate number
// currentDposList: current top delegates
func ShuffleNewRound(beginTime int64, maxElectDelegate int, currentDposList []types.Candidate, blockInterval int64) []types.ShuffleDel {
	if len(currentDposList) < maxElectDelegate {
		maxElectDelegate = len(currentDposList)
	}
	log.Infof("shuffle beginTime=%v, current delegate amount=%v, delegateNumber=%d", time.Unix(beginTime, 0), len(currentDposList), maxElectDelegate)
	var newRoundList []types.ShuffleDel
	truncDelegateList := Shuffle(beginTime+1, maxElectDelegate)
	log.Infof("shuffle beginTime=%v, trunc=%v", time.Unix(beginTime, 0), truncDelegateList)

	for index := int64(0); index < int64(maxElectDelegate); index++ {
		delegateIndex := truncDelegateList[index]
		workTime := beginTime + index*blockInterval
		newRoundList = append(newRoundList, types.ShuffleDel{WorkTime: uint64(workTime), Address: currentDposList[delegateIndex].Address, Vote: currentDposList[delegateIndex].Vote, Nickname: currentDposList[delegateIndex].Nickname})
	}
	return newRoundList
}

// get block shuffle time by block time and any shuffleTime,can not get future shuffle time
func CalShuffleTimeByHeaderTime(nextRoundBeginTime, blockTime, blockInterval, maxElectDelegate int64) int64 {
	var count int64
	nowUnix := time.Now().Unix()
	if nextRoundBeginTime == blockTime { // The first block of this round
		return nextRoundBeginTime
	} else if nextRoundBeginTime < blockTime { // Calculate backwards
		for {
			shuffleTime := nextRoundBeginTime + count*maxElectDelegate*blockInterval
			if shuffleTime <= blockTime && shuffleTime+maxElectDelegate*blockInterval > blockTime {
				return shuffleTime
			}
			if shuffleTime >= nowUnix {
				break
			}
			count++
		}
	} else { // Calculate forward
		for {
			shuffleTime := nextRoundBeginTime - count*maxElectDelegate*blockInterval
			if shuffleTime <= blockTime {
				return shuffleTime
			}
			if shuffleTime <= 0 {
				break
			}
			count++
		}
	}
	return 0
}
