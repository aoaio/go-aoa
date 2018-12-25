package core

import (
	"math/big"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/consensus"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/core/vm"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/log"
)

type ChainContext interface {

	Engine() consensus.Engine

	GetHeader(common.Hash, uint64) *types.Header

	GetDelegatePoll() (*map[common.Address]types.Candidate, error)

	GetGenesisConfig() *params.ChainConfig
}

func NewEVMContext(msg Message, header *types.Header, chain ChainContext, author *common.Address) vm.Context {
	var delegates *map[common.Address]types.Candidate
	var err error
	if msg.Action() == types.ActionRegister || msg.Action() == types.ActionAddVote || msg.Action() == types.ActionSubVote {
		delegates, err = chain.GetDelegatePoll()
		if err != nil {
			return vm.Context{}
		}
	}

	return vm.Context{
		CanTransfer:  CanTransfer,
		Transfer:     Transfer,
		Vote:         Vote,
		GetHash:      GetHashFn(header, chain),
		Origin:       msg.From(),
		Coinbase:     header.Coinbase,
		BlockNumber:  new(big.Int).Set(header.Number),
		Time:         new(big.Int).Set(header.Time),
		Difficulty:   new(big.Int).Set(types.BlockDifficult),
		GasLimit:     header.GasLimit,
		GasPrice:     new(big.Int).Set(msg.GasPrice()),
		DelegateList: delegates,
	}
}

func GetHashFn(ref *types.Header, chain ChainContext) func(n uint64) common.Hash {
	var cache map[uint64]common.Hash

	return func(n uint64) common.Hash {

		if cache == nil {
			cache = map[uint64]common.Hash{
				ref.Number.Uint64() - 1: ref.ParentHash,
			}
		}

		if hash, ok := cache[n]; ok {
			return hash
		}

		for header := chain.GetHeader(ref.ParentHash, ref.Number.Uint64()-1); header != nil; header = chain.GetHeader(header.ParentHash, header.Number.Uint64()-1) {
			cache[header.Number.Uint64()-1] = header.ParentHash
			if n == header.Number.Uint64()-1 {
				return header.ParentHash
			}
		}
		return common.Hash{}
	}
}

func CanTransfer(db vm.StateDB, addr common.Address, asset *common.Address, amount *big.Int) bool {
	if asset == nil {
		return db.GetBalance(addr).Cmp(amount) >= 0
	} else {
		return db.GetAssetBalance(addr, *asset).Cmp(amount) >= 0
	}
}

func Transfer(db vm.StateDB, sender, recipient common.Address, asset *common.Address, amount *big.Int) {
	if asset == nil {
		db.SubBalance(sender, amount)
		db.AddBalance(recipient, amount)
	} else {
		ok := db.SubAssetBalance(sender, *asset, amount)
		if ok {
			db.AddAssetBalance(recipient, *asset, amount)
		}
	}
}

func Vote(db vm.StateDB, user common.Address, amount *big.Int, vote []types.Vote, delegateList *map[common.Address]types.Candidate, maxElectDelegate int64) error {
	d := *delegateList

	newVoteList, diff, err := changeVoteList(db.GetVoteList(user), vote, d)
	if err != nil {
		log.Debug("InVoteError", "err", err)
		return err
	}
	lockBalance, _ := new(big.Int).SetString(db.GetLockBalance(user).String(), 0)

	if (lockBalance.Add(lockBalance, diff)).Cmp(new(big.Int).Mul(big.NewInt(params.Aoa), big.NewInt(maxElectDelegate))) > 0 {
		return vm.ErrVote
	}

	db.SubBalance(user, diff)

	db.AddLockBalance(user, diff)

	db.SetVoteList(user, newVoteList)

	return nil
}

func changeVoteList(prevVoteList []common.Address, curVoteList []types.Vote, delegateList map[common.Address]types.Candidate) ([]common.Address, *big.Int, error) {
	var (
		voteChangeList = prevVoteList
		diff           = int64(0)
	)

	for _, vote := range curVoteList {
		switch vote.Operation {
		case 0:
			if _, contain := sliceContains(*vote.Candidate, voteChangeList); !contain {
				if _, ok := delegateList[*vote.Candidate]; !ok {
					return nil, nil, vm.ErrVote
				}
				diff += 1
				voteChangeList = append(voteChangeList, *vote.Candidate)
			} else {
				return nil, big.NewInt(0), vm.ErrVote
			}
		case 1:
			if j, contain := sliceContains(*vote.Candidate, voteChangeList); contain {
				diff -= 1
				voteChangeList = append(voteChangeList[:j], voteChangeList[j+1:]...)
			} else {
				return nil, big.NewInt(0), vm.ErrVote
			}
		default:
			return nil, big.NewInt(0), vm.ErrVote
		}
	}
	lockNumber := big.NewInt(diff)
	lockNumber = lockNumber.Mul(lockNumber, big.NewInt(params.Aoa))
	return voteChangeList, lockNumber, nil
}

func sliceContains(address common.Address, slice []common.Address) (int, bool) {
	for i, a := range slice {

		if address == a {
			return i, true
		}
	}
	return -1, false
}
