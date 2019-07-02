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

package delegatestate

import (
	"github.com/Aurorachain/go-aoa/common"
	"math/big"
)

type journalEntry interface {
	undo(db *DelegateDB)
}

type journal []journalEntry

type (

	// Changes to the account trie.
	createObjectChange struct {
		account *common.Address
	}
	resetObjectChange struct {
		prev *delegateObject
	}
	addLogChange struct {
		txhash common.Hash
	}

	storageChange struct {
		account       *common.Address
		key, prevalue common.Hash
	}

	voteChange struct {
		account *common.Address
		prev    *big.Int
	}

	touchChange struct {
		account   *common.Address
		prev      bool
		prevDirty bool
	}

	suicideChange struct {
		account  *common.Address
		prev     bool // whether account had already suicided
		prevVote *big.Int
	}
)

var ripemd = common.HexToAddress("0000000000000000000000000000000000000003")

func (ch createObjectChange) undo(s *DelegateDB) {
	delete(s.delegateObjects, *ch.account)
	delete(s.delegateObjectsDirty, *ch.account)
}

func (ch resetObjectChange) undo(s *DelegateDB) {
	s.setStateObject(ch.prev)
}

func (ch addLogChange) undo(s *DelegateDB) {
	logs := s.logs[ch.txhash]
	if len(logs) == 1 {
		delete(s.logs, ch.txhash)
	} else {
		s.logs[ch.txhash] = logs[:len(logs)-1]
	}
	s.logSize--
}

func (ch storageChange) undo(s *DelegateDB) {
	s.GetStateObject(*ch.account).setState(ch.key, ch.prevalue)
}

func (ch voteChange) undo(s *DelegateDB) {
	s.GetStateObject(*ch.account).setVote(ch.prev)
}

func (ch touchChange) undo(s *DelegateDB) {
	if !ch.prev && *ch.account != ripemd {
		s.GetStateObject(*ch.account).touched = ch.prev
		if !ch.prevDirty {
			delete(s.delegateObjectsDirty, *ch.account)
		}
	}
}

func (ch suicideChange) undo(s *DelegateDB) {
	obj := s.GetStateObject(*ch.account)
	if obj != nil {
		obj.suicided = ch.prev
		obj.setVote(ch.prevVote)
	}
}
