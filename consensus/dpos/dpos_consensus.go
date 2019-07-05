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

package dpos

import (
	"errors"
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/consensus"
	"github.com/Aurorachain/go-aoa/consensus/delegatestate"
	"github.com/Aurorachain/go-aoa/core/state"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/crypto/secp256k1"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/params"
	"github.com/Aurorachain/go-aoa/rlp"
	"github.com/Aurorachain/go-aoa/rpc"
	"math/big"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	allowedFutureBlockTime = 15 * time.Second // Max time from current time allowed for blocks, before they're considered future blocks
	errZeroBlockTime       = errors.New("timestamp equals parent's")
)

// Config are the configuration parameters of the ethash.
type Config struct {
	CacheDir string
}

type AuroraDpos struct {
	// config Config
	lock sync.Mutex
}

func New() *AuroraDpos {
	return &AuroraDpos{}
}

// AccumulateRewards credits the coinbase of the given block with the produce reward
func accumulateRewards(config *params.ChainConfig, state *state.StateDB, header *types.Header) {
	blockReward := config.FrontierBlockReward
	if config.IsByzantium(header.Number) {
		blockReward = config.ByzantiumBlockReward
	}

	reward := new(big.Int).Set(blockReward)
	state.AddBalance(header.Coinbase, reward)
}

// check parent exist and cache header
func (d *AuroraDpos) Prepare(chain consensus.ChainReader, header *types.Header) error {
	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	return nil
}

func (d *AuroraDpos) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, dState *delegatestate.DelegateDB, txs []*types.Transaction, receipts []*types.Receipt) (*types.Block, error) {
	accumulateRewards(chain.Config(), state, header)

	header.Root = state.IntermediateRoot(false)
	header.DelegateRoot = dState.IntermediateRoot(false)

	// Header seems complete, assemble into a block and return
	return types.NewBlock(header, txs, receipts), nil

}

func (d *AuroraDpos) VerifyHeaderAndSign(chain consensus.ChainReader, block *types.Block, currentShuffleList *types.ShuffleList, blockInterval int) error {
	header := block.Header()
	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	blockTime := header.Time.Uint64()
	currentTime := time.Now().Unix()
	if currentTime < int64(blockTime) || (currentTime >= (int64(blockTime) + int64(blockInterval))) {
		errMsg := fmt.Sprintf("block time is expire|blockNumber:%d blockTime:%d currentTime:%d", block.NumberU64(), blockTime, currentTime)
		return errors.New(errMsg)
	}
	if parent == nil {
		errMsg := fmt.Sprintf("unknown ancestor|currentBlockNumber:%d blockParentHash:%s", block.NumberU64(), block.ParentHash().Hex())
		return errors.New(errMsg)
	}
	err := d.verifyHeader(chain, header, parent)
	if err != nil {
		return err
	}
	coinbase := header.Coinbase.Hex()
	coinbaseSign := block.Signature
	if len(coinbaseSign) == 0 {
		errMsg := fmt.Sprintf("miss Signature blockNumber:%d", block.NumberU64())
		return errors.New(errMsg)
	}
	var delegate types.ShuffleDel
	var exist bool
	log.Infof("VerifyHeaderAndSign, coinbase=%v, shuffleDelsLen=%v", coinbase, len(currentShuffleList.ShuffleDels))
	for _, v := range currentShuffleList.ShuffleDels {
		if strings.EqualFold(v.Address, coinbase) {
			delegate = v
			exist = true
			break
		}
	}
	if !exist {
		return errors.New(fmt.Sprintf("coinbase:%s not exist in current shuffle list", coinbase))
	}
	log.Infof("VerifyHeaderAndSign, delegate=%v", delegate)
	if blockTime != delegate.WorkTime {
		errMsg := fmt.Sprintf("timeStamp not same blockNumber:%d coinbase:%s blockTime:%d shuffleTime:%d", block.NumberU64(), coinbase, blockTime, delegate.WorkTime)
		return errors.New(errMsg)
	}
	pubkey, err := secp256k1.RecoverPubkey(block.Hash().Bytes()[:32], coinbaseSign)
	log.Infof("VerifyHeaderAndSign, coinbaseSign=%v, blockHash=%v, pubkey=%v, err=%v ", coinbaseSign, block.Hash().Hex(), pubkey, err)
	//if err != nil {
	//	return err
	//}
	pubAddress := crypto.PubkeyToAddress(*crypto.ToECDSAPub(pubkey)).Hex()
	if !strings.EqualFold(pubAddress, coinbase) {
		errMsg := fmt.Sprintf("sign address error blockNumber:%d coinbase:%s signAddress:%s", block.NumberU64(), coinbase, pubAddress)
		return errors.New(errMsg)
	}
	return nil
}

func (d *AuroraDpos) VerifySignatureSend(blockHash common.Hash, confirmSign []byte, currentShuffleList *types.ShuffleList) error {
	confirmPubkey, err := secp256k1.RecoverPubkey(blockHash.Bytes()[:32], confirmSign)
	if err != nil {
		return err
	}
	confirmPubAddress := crypto.PubkeyToAddress(*crypto.ToECDSAPub(confirmPubkey)).Hex()
	for _, v := range currentShuffleList.ShuffleDels {
		if strings.EqualFold(v.Address, confirmPubAddress) {
			return nil
		}
	}
	errMsg := fmt.Sprintf("%s isn't in currentShuffleList blockHash:%s", confirmPubAddress, blockHash.Hex())
	return errors.New(errMsg)
}

func (d *AuroraDpos) VerifyBlockGenerate(chain consensus.ChainReader, block *types.Block, currentShuffleList *types.ShuffleList, blockInterval int) error {
	genesisConfig := chain.Config()
	maxElectDelegate := genesisConfig.MaxElectDelegate.Int64()
	delegateAmount := (maxElectDelegate / 3) * 2
	err := d.VerifyHeaderAndSign(chain, block, currentShuffleList, blockInterval)
	if err != nil {
		return err
	}
	// rlp decode
	rlpEncodeSigns := block.RlpEncodeSigns
	var signs []types.VoteSign
	err = rlp.DecodeBytes(rlpEncodeSigns, &signs)
	if err != nil {
		return err
	}

	checkSignAddressMap := make(map[string]byte, 0)
	blockHashBytes := block.Hash().Bytes()
	for _, sign := range signs {
		tempPubKey, _ := secp256k1.RecoverPubkey(blockHashBytes[:32], sign.Sign)
		tempPubAddress := crypto.PubkeyToAddress(*crypto.ToECDSAPub(tempPubKey)).Hex()
		if checkInShuffleList(currentShuffleList, tempPubAddress) {
			checkSignAddressMap[tempPubAddress] = 1
		}
	}
	if len(checkSignAddressMap) > int(delegateAmount) {
		return nil
	}
	errMsg := fmt.Sprintf("delegate sign not enough blockNumber:%d need check signs:%d actual check signs:%d", block.NumberU64(), delegateAmount, len(checkSignAddressMap))
	return errors.New(errMsg)
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications.
func (d *AuroraDpos) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header) (chan<- struct{}, <-chan error) {

	// // If we're running a full engine faking, accept any input as valid
	if len(headers) == 0 {
		abort, results := make(chan struct{}), make(chan error, len(headers))
		return abort, results
	}

	// Spawn as many workers as allowed threads
	workers := runtime.GOMAXPROCS(0)
	if len(headers) < workers {
		workers = len(headers)
	}

	// Create a task channel and spawn the verifiers
	var (
		inputs       = make(chan int)
		done         = make(chan int, workers)
		headerErrors = make([]error, len(headers))
		abort        = make(chan struct{})
	)
	for i := 0; i < workers; i++ {
		go func() {
			for index := range inputs {
				headerErrors[index] = d.verifyHeaders(chain, headers, index)
				done <- index
			}
		}()
	}

	errorsOut := make(chan error, len(headers))
	go func() {
		defer close(inputs)
		var (
			in, out = 0, 0
			checked = make([]bool, len(headers))
			inputs  = inputs
		)
		for {
			select {
			case inputs <- in:
				if in++; in == len(headers) {
					// Reached end of headers. Stop sending to workers.
					inputs = nil
				}
			case index := <-done:
				for checked[index] = true; checked[out]; out++ {
					errorsOut <- headerErrors[out]
					if out == len(headers)-1 {
						return
					}
				}
			case <-abort:
				return
			}
		}
	}()
	return abort, errorsOut
}

func (d *AuroraDpos) verifyHeaders(chain consensus.ChainReader, headers []*types.Header, index int) error {
	var parent *types.Header
	if index == 0 {
		parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
	} else if headers[index-1].Hash() == headers[index].ParentHash {
		parent = headers[index-1]
	}

	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	if chain.GetHeader(headers[index].Hash(), headers[index].Number.Uint64()) != nil {
		return nil // known block
	}

	return d.verifyHeader(chain, headers[index], parent)
}

// verifyHeader checks whether a header conforms to the consensus rules of the AOA engine
func (d *AuroraDpos) verifyHeader(chain consensus.ChainReader, header, parent *types.Header) error {
	// Ensure that the header's extra-data section is of a reasonable size
	if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
	}

	if header.Time.Cmp(big.NewInt(time.Now().Add(allowedFutureBlockTime).Unix())) > 0 {
		return consensus.ErrFutureBlock
	}

	if header.Time.Cmp(parent.Time) <= 0 {
		return errZeroBlockTime
	}

	maxGasLimit := uint64(0x7fffffffffffffff)
	if header.GasLimit > maxGasLimit {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, maxGasLimit)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}

	// Verify that the gas limit remains within allowed bounds
	diff := int64(parent.GasLimit) - int64(header.GasLimit)
	if diff < 0 {
		diff *= -1
	}
	limit := parent.GasLimit / params.GasLimitBoundDivisor

	if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
		return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
	}
	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
	}

	return nil
}

func (d *AuroraDpos) VerifyHeader(chain consensus.ChainReader, header *types.Header) error {
	// Short circuit if the header is known, or it's parent not
	number := header.Number.Uint64()
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	if time.Now().Unix() < header.Time.Int64() {
		return consensus.ErrFutureBlock
	}
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	// Sanity checks passed, do a proper verification
	return d.verifyHeader(chain, header, parent)
}

// APIs implements consensus.Engine, returning the user facing RPC APIs. Currently
// that is empty.
func (d *AuroraDpos) APIs(chain consensus.ChainReader) []rpc.API {
	return nil
}

func (d *AuroraDpos) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func checkInShuffleList(currentShuffleList *types.ShuffleList, address string) bool {
	for _, v := range currentShuffleList.ShuffleDels {
		if strings.EqualFold(v.Address, address) {
			return true
		}
	}
	return false
}
