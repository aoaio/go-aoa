package util

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/log"
	"math"
	"strconv"
	"time"
)

func Shuffle(height int64, delegateNumber int) []int {
	var truncDelegateList []int

	for i := 0; i < delegateNumber; i++ {
		truncDelegateList = append(truncDelegateList, i)
	}

	seed := math.Floor(float64(height / int64(delegateNumber)))

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

			truncDelegateList[newIndex], truncDelegateList[i] = truncDelegateList[i], truncDelegateList[newIndex]
			x++
		}
	}
	return truncDelegateList

}

func ShuffleNewRound(beginTime int64, maxElectDelegate int, currentDposList []types.Candidate,blockInterval int64) []types.ShuffleDel {
	if len(currentDposList) < maxElectDelegate {
		maxElectDelegate = len(currentDposList)
	}
	log.Info("shuffle", "beginTime", beginTime, "delegateNumber", len(currentDposList),"delegateNumber",maxElectDelegate)
	var newRoundList []types.ShuffleDel
	truncDelegateList := Shuffle(beginTime+1, maxElectDelegate)
	log.Debug("shuffle", "beginTime", time.Unix(beginTime, 0), "trunc", truncDelegateList)

	for index := int64(0); index < int64(maxElectDelegate); index++ {
		delegateIndex := truncDelegateList[index]
		workTime := beginTime + index*blockInterval
		newRoundList = append(newRoundList, types.ShuffleDel{WorkTime: uint64(workTime), Address: currentDposList[delegateIndex].Address, Vote: currentDposList[delegateIndex].Vote, Nickname: currentDposList[delegateIndex].Nickname})
	}
	return newRoundList
}

func CalShuffleTimeByHeaderTime(nextRoundBeginTime, blockTime,blockInterval,maxElectDelegate int64) int64 {
	var count int64
	nowUnix := time.Now().Unix()
	if nextRoundBeginTime == blockTime {
		return nextRoundBeginTime
	} else if nextRoundBeginTime < blockTime {
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
	} else {
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
