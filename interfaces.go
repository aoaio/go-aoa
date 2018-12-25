package aurora

import (
	"context"
	"errors"
	"math/big"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/core/types"
)

var NotFound = errors.New("not found")

type Subscription interface {

	Unsubscribe()

	Err() <-chan error
}

type ChainReader interface {
	BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
	HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	TransactionCount(ctx context.Context, blockHash common.Hash) (uint, error)
	TransactionInBlock(ctx context.Context, blockHash common.Hash, index uint) (*types.Transaction, error)

	SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (Subscription, error)
}

type TransactionReader interface {

	TransactionByHash(ctx context.Context, txHash common.Hash) (tx *types.Transaction, isPending bool, err error)

	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
}

type ChainStateReader interface {
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	VoteListAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]common.Address, error)
	StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	GetDelegateList(ctx context.Context, blockNumber *big.Int) ([]types.Candidate, error)
}

type SyncProgress struct {
	StartingBlock uint64
	CurrentBlock  uint64
	HighestBlock  uint64
	PulledStates  uint64
	KnownStates   uint64
}

type ChainSyncReader interface {
	SyncProgress(ctx context.Context) (*SyncProgress, error)
}

type CallMsg struct {
	From     common.Address
	To       *common.Address
	Gas      uint64
	GasPrice *big.Int
	Value    *big.Int
	Data     []byte
}

type ContractCaller interface {
	CallContract(ctx context.Context, call CallMsg, blockNumber *big.Int) ([]byte, error)
}

type FilterQuery struct {
	FromBlock *big.Int
	ToBlock   *big.Int
	Addresses []common.Address

	Topics [][]common.Hash
}

type LogFilterer interface {
	FilterLogs(ctx context.Context, q FilterQuery) ([]types.Log, error)
	SubscribeFilterLogs(ctx context.Context, q FilterQuery, ch chan<- types.Log) (Subscription, error)
}

type TransactionSender interface {
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

type GasPricer interface {
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
}

type PendingStateReader interface {
	PendingBalanceAt(ctx context.Context, account common.Address) (*big.Int, error)
	PendingStorageAt(ctx context.Context, account common.Address, key common.Hash) ([]byte, error)
	PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	PendingTransactionCount(ctx context.Context) (uint, error)
}

type PendingContractCaller interface {
	PendingCallContract(ctx context.Context, call CallMsg) ([]byte, error)
}

type GasEstimator interface {
	EstimateGas(ctx context.Context, call CallMsg) (uint64, error)
}

type PendingStateEventer interface {
	SubscribePendingTransactions(ctx context.Context, ch chan<- *types.Transaction) (Subscription, error)
}
