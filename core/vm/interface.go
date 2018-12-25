package vm

import (
	"math/big"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/core/types"
)

type StateDB interface {
	CreateAccount(common.Address)

	SubBalance(common.Address, *big.Int)
	AddBalance(common.Address, *big.Int)
	GetBalance(common.Address) *big.Int

	GetLockBalance(addr common.Address) *big.Int
	AddLockBalance(addr common.Address, amount *big.Int)
	SubLockBalance(addr common.Address, amount *big.Int)

	GetAssetBalance(addr common.Address, asset common.Address) *big.Int
	SubAssetBalance(addr common.Address, asset common.Address, amount *big.Int) bool
	AddAssetBalance(addr common.Address, asset common.Address, amount *big.Int)
	GetAssets(addr common.Address) []types.Asset

	ValidateAsset(assetInfo types.AssetInfo) error
	PublishAsset(addr common.Address, assetInfo types.AssetInfo) error
	GetAssetInfo(common.Address) (*types.AssetInfo, error)

	GetNonce(common.Address) uint64
	SetNonce(common.Address, uint64)

	GetCodeHash(common.Address) common.Hash
	GetCode(common.Address) []byte
	SetCode(common.Address, []byte)
	GetCodeSize(common.Address) int

	GetAbi(common.Address) string
	SetAbi(common.Address, string)

	GetVoteList(addr common.Address) []common.Address
	SetVoteList(addr common.Address, voteList []common.Address)

	SetLockBalance(addr common.Address, amount *big.Int)

	AddRefund(uint64)
	GetRefund() uint64

	GetState(common.Address, common.Hash) common.Hash
	SetState(common.Address, common.Hash, common.Hash)

	Suicide(common.Address) bool
	HasSuicided(common.Address) bool

	Exist(common.Address) bool

	Empty(common.Address) bool

	RevertToSnapshot(int, bool)
	Snapshot() int

	AddLog(*types.Log)
	AddPreimage(common.Hash, []byte)

	ForEachStorage(common.Address, func(common.Hash, common.Hash) bool)
}

type CallContext interface {

	Call(env *EVM, me ContractRef, addr common.Address, data []byte, gas, value *big.Int) ([]byte, error)

	CallCode(env *EVM, me ContractRef, addr common.Address, data []byte, gas, value *big.Int) ([]byte, error)

	DelegateCall(env *EVM, me ContractRef, addr common.Address, data []byte, gas *big.Int) ([]byte, error)

	Create(env *EVM, me ContractRef, data []byte, gas, value *big.Int) ([]byte, common.Address, error)
}
