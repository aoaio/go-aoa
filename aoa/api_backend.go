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

package aoa

import (
	"context"
	"math/big"

	"github.com/Aurorachain/go-aoa/accounts"
	aa "github.com/Aurorachain/go-aoa/accounts/walletType"
	"github.com/Aurorachain/go-aoa/aoa/downloader"
	"github.com/Aurorachain/go-aoa/aoa/gasprice"
	"github.com/Aurorachain/go-aoa/aoadb"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/common/math"
	"github.com/Aurorachain/go-aoa/core"
	"github.com/Aurorachain/go-aoa/core/bloombits"
	"github.com/Aurorachain/go-aoa/core/state"
	"github.com/Aurorachain/go-aoa/core/types"
	"github.com/Aurorachain/go-aoa/core/vm"
	"github.com/Aurorachain/go-aoa/core/watch"
	"github.com/Aurorachain/go-aoa/event"
	"github.com/Aurorachain/go-aoa/params"
	"github.com/Aurorachain/go-aoa/rpc"
)

// AoaApiBackend implements aoaapi.Backend for full nodes
type AoaApiBackend struct {
	aoa *Aurora
	gpo *gasprice.Oracle
}

func (b *AoaApiBackend) ChainConfig() *params.ChainConfig {
	return b.aoa.chainConfig
}

func (b *AoaApiBackend) GetDelegateWalletInfoCallback() func(data *aa.DelegateWalletInfo) {
	return b.aoa.protocolManager.GetAddDelegateWalletCallback()
}

func (b *AoaApiBackend) CurrentBlock() *types.Block {
	return b.aoa.blockchain.CurrentBlock()
}

func (b *AoaApiBackend) SetHead(number uint64) {
	b.aoa.protocolManager.downloader.Cancel()
	b.aoa.blockchain.SetHead(number)
}

func (b *AoaApiBackend) HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the delegate
	if blockNr == rpc.PendingBlockNumber {
		block := b.aoa.dposMiner.PendingBlock()
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.aoa.blockchain.CurrentBlock().Header(), nil
	}
	return b.aoa.blockchain.GetHeaderByNumber(uint64(blockNr)), nil
}

func (b *AoaApiBackend) BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the delegate
	if blockNr == rpc.PendingBlockNumber {
		block := b.aoa.dposMiner.PendingBlock()
		return block, nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.aoa.blockchain.CurrentBlock(), nil
	}
	return b.aoa.blockchain.GetBlockByNumber(uint64(blockNr)), nil
}

func (b *AoaApiBackend) StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	// Pending state is only known by the delegate
	if blockNr == rpc.PendingBlockNumber {
		currentBlock := b.aoa.blockchain.CurrentBlock()
		statedb, _ := b.aoa.blockchain.StateAt(currentBlock.Root())
		return statedb, currentBlock.Header(), nil
	}
	// Otherwise resolve the block number and return its state
	header, err := b.HeaderByNumber(ctx, blockNr)
	if header == nil || err != nil {
		return nil, nil, err
	}
	stateDb, err := b.aoa.BlockChain().StateAt(header.Root)
	return stateDb, header, err
}

func (b *AoaApiBackend) GetBlock(ctx context.Context, blockHash common.Hash) (*types.Block, error) {
	return b.aoa.blockchain.GetBlockByHash(blockHash), nil
}

func (b *AoaApiBackend) GetDelegatePoll(block *types.Block) (*map[common.Address]types.Candidate, error) {
	delegateDB, err := b.aoa.blockchain.DelegateStateAt(block.DelegateRoot())
	if err != nil {
		return nil, err
	}
	delegates := delegateDB.GetDelegates()
	res := make(map[common.Address]types.Candidate)
	for _, delegate := range delegates {
		address := common.HexToAddress(delegate.Address)
		res[address] = delegate
	}
	return &res, nil
}

func (b *AoaApiBackend) GetReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
	return core.GetBlockReceipts(b.aoa.chainDb, blockHash, core.GetBlockNumber(b.aoa.chainDb, blockHash)), nil
}

func (b *AoaApiBackend) GetTd(blockHash common.Hash) *big.Int {
	return b.aoa.blockchain.GetTdByHash(blockHash)
}

func (b *AoaApiBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error) {
	state.SetBalance(msg.From(), math.MaxBig256)
	vmError := func() error { return nil }

	ctext := core.NewEVMContext(msg, header, b.aoa.BlockChain(), nil)
	return vm.NewEVM(ctext, state, b.aoa.chainConfig, vmCfg), vmError, nil
}

func (b *AoaApiBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.aoa.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *AoaApiBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.aoa.BlockChain().SubscribeChainEvent(ch)
}

func (b *AoaApiBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.aoa.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *AoaApiBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.aoa.BlockChain().SubscribeChainSideEvent(ch)
}

func (b *AoaApiBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.aoa.BlockChain().SubscribeLogsEvent(ch)
}

func (b *AoaApiBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return b.aoa.txPool.AddLocal(signedTx)
}

func (b *AoaApiBackend) GetPoolTransactions() (types.Transactions, error) {
	pending, err := b.aoa.txPool.Pending()
	if err != nil {
		return nil, err
	}
	var txs types.Transactions
	for _, batch := range pending {
		txs = append(txs, batch...)
	}
	return txs, nil
}

func (b *AoaApiBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.aoa.txPool.Get(hash)
}

func (b *AoaApiBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.aoa.txPool.State().GetNonce(addr), nil
}

func (b *AoaApiBackend) Stats() (pending int, queued int) {
	return b.aoa.txPool.Stats()
}

func (b *AoaApiBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.aoa.TxPool().Content()
}

func (b *AoaApiBackend) SubscribeTxPreEvent(ch chan<- core.TxPreEvent) event.Subscription {
	return b.aoa.TxPool().SubscribeTxPreEvent(ch)
}

func (b *AoaApiBackend) Downloader() *downloader.Downloader {
	return b.aoa.Downloader()
}

func (b *AoaApiBackend) ProtocolVersion() int {
	return b.aoa.EthVersion()
}

func (b *AoaApiBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestPrice(ctx)
}

func (b *AoaApiBackend) ChainDb() aoadb.Database {
	return b.aoa.ChainDb()
}

func (b *AoaApiBackend) AccountManager() *accounts.Manager {
	return b.aoa.AccountManager()
}

func (b *AoaApiBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.aoa.bloomIndexer.Sections()
	return params.BloomBitsBlocks, sections
}

func (b *AoaApiBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.aoa.bloomRequests)
	}
}

func (b *AoaApiBackend) IsWatchInnerTxEnable() bool {
	return b.aoa.config.EnableInterTxWatching
}

func (b *AoaApiBackend) GetInnerTxDb() watch.InnerTxDb {
	return b.aoa.BlockChain().GetInnerTxDb()
}
