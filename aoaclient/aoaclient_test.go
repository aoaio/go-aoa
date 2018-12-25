package aoaclient

import "github.com/Aurorachain/go-Aurora"

var (
	_ = aurora.ChainReader(&Client{})
	_ = aurora.TransactionReader(&Client{})
	_ = aurora.ChainStateReader(&Client{})
	_ = aurora.ChainSyncReader(&Client{})
	_ = aurora.ContractCaller(&Client{})
	_ = aurora.GasEstimator(&Client{})
	_ = aurora.GasPricer(&Client{})
	_ = aurora.LogFilterer(&Client{})
	_ = aurora.PendingStateReader(&Client{})

	_ = aurora.PendingContractCaller(&Client{})
)
