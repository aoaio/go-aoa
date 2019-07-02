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
	"errors"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/core/state"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/params"
	"math/big"
	"strings"
	"github.com/Aurorachain/go-aoa/consensus/delegatestate"
)

const (
	register = iota + 1
	addVote
	subVote
	cancel
)

var ErrInvalidSig = errors.New("invalid transaction v, r, s values")
var big8 = big.NewInt(8)

//Scan block transactions
func CountBlockVote(block *types.Block, delegateList map[string]types.Candidate, db *state.StateDB) types.CandidateWrapper {
	log.Infof("Start CountBlockVote, bolockNumber=%v", block.NumberU64())
	txs := block.Transactions()
	candidates := make([]types.VoteCandidate, 0)
	candidateVotes := make(map[string]int64, 0)
	for _, tx := range txs {
		signer := types.NewAuroraSigner(tx.ChainId())
		f, _ := types.Sender(signer, tx)
		from := strings.ToLower(f.Hex())
		switch tx.TxDataAction() {
		case types.ActionAddVote, types.ActionSubVote:
			votes, err := types.BytesToVote(tx.Vote())
			if err != nil {
				log.Infof("Vote_Util unmarshal error: %v", err)
				continue
			}
			for _, vote := range votes {
				address := strings.ToLower(vote.Candidate.Hex())
				operation := vote.Operation
				if operation == 0 {
					if _, ok := candidateVotes[address]; ok {
						candidateVotes[address] += 1
					} else {
						candidateVotes[address] = 1
					}
				} else if operation == 1 {
					if _, ok := candidateVotes[address]; ok {
						candidateVotes[address] -= 1
					} else {
						candidateVotes[address] = -1
					}
				}
			}
		case types.ActionRegister:
			candidate := types.VoteCandidate{Address: from, Vote: 0, Nickname: string(tx.Nickname()), Action: register}
			candidates = append(candidates, candidate)
		default:
			if _, ok := delegateList[from]; ok {
				registerCost := new(big.Int)
				registerCost.SetString(params.TxGasAgentCreation, 10)
				log.Infof("VoteUtil deal cancel, address balance=%v, compare=%v", db.GetBalance(common.HexToAddress(from)), registerCost)
				if db.GetBalance(common.HexToAddress(from)).Cmp(registerCost) < 0 {
					candidate := types.VoteCandidate{Address: from, Action: cancel}
					candidates = append(candidates, candidate)
				}
			}
		}
	}
	for address, vote := range candidateVotes {
		var action int
		if vote < 0 {
			action = subVote
			vote = -vote
		} else {
			action = addVote
		}
		candidate := types.VoteCandidate{Address: address, Vote: uint64(vote), Action: action}
		candidates = append(candidates, candidate)
	}
	candidateWrapper := types.CandidateWrapper{Candidates: candidates, BlockHeight: block.Number().Int64(), BlockTime: block.Time().Int64()}
	return candidateWrapper
}

func CountTrxVote(from string, tx *types.Transaction, statedb *state.StateDB, db *delegatestate.DelegateDB) ([]types.VoteCandidate, error) {
	candidates := make([]types.VoteCandidate, 0)
	candidateVotes := make(map[string]int64, 0)
	switch tx.TxDataAction() {
	case types.ActionAddVote, types.ActionSubVote:
		votes, err := types.BytesToVote(tx.Vote())
		if err != nil {
			log.Error("Vote_Util unmarshal error:", "err", err)
			return candidates, err
		}
		for _, vote := range votes {
			address := strings.ToLower(vote.Candidate.Hex())
			operation := vote.Operation
			if operation == 0 {
				if _, ok := candidateVotes[address]; ok {
					candidateVotes[address] += 1
				} else {
					candidateVotes[address] = 1
				}
			} else if operation == 1 {
				if _, ok := candidateVotes[address]; ok {
					candidateVotes[address] -= 1
				} else {
					candidateVotes[address] = -1
				}
			}
		}
	case types.ActionRegister:
		candidate := types.VoteCandidate{Address: from, Vote: 0, Nickname: string(tx.Nickname()), Action: register}
		candidates = append(candidates, candidate)
	}
	for address, vote := range candidateVotes {
		var action int
		if vote < 0 {
			action = subVote
			vote = -vote
		} else {
			action = addVote
		}
		candidate := types.VoteCandidate{Address: address, Vote: uint64(vote), Action: action}
		candidates = append(candidates, candidate)
	}
	if db.Exist(common.HexToAddress(from)) {
		registerCost := new(big.Int)
		registerCost.SetString(params.TxGasAgentCreation, 10)
		if statedb.GetBalance(common.HexToAddress(from)).Cmp(registerCost) < 0 {
			candidate := types.VoteCandidate{Address: from, Action: cancel}
			candidates = append(candidates, candidate)
		}
	}
	return candidates, nil
}

func recoverPlainPubKey(signHash common.Hash, R, S, Vb *big.Int, homestead bool) ([]byte, error) {
	if Vb.BitLen() > 8 {
		return nil, ErrInvalidSig
	}
	V := byte(Vb.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, R, S, homestead) {
		return nil, ErrInvalidSig
	}
	// encode the snature in uncompressed format
	r, s := R.Bytes(), S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V
	// recover the public key from the snature
	pub, err := crypto.Ecrecover(signHash[:], sig)
	if err != nil {
		return nil, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return nil, errors.New("invalid public key")
	}
	return pub, err
}


