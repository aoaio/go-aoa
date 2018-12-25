package core

import (
	"fmt"
	"math/big"

	"github.com/Aurorachain/go-Aurora/aoadb"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/consensus"
	"github.com/Aurorachain/go-Aurora/core/state"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/core/vm"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/consensus/delegatestate"
)

var (
	canonicalSeed = 1
)

type BlockGen struct {
	i           int
	parent      *types.Block
	chain       []*types.Block
	chainReader consensus.ChainReader
	header      *types.Header
	statedb     *state.StateDB

	gasPool  *GasPool
	txs      []*types.Transaction
	receipts []*types.Receipt
	uncles   []*types.Header

	config     *params.ChainConfig
	engine     consensus.Engine
	delegatedb *delegatestate.DelegateDB
}

func (b *BlockGen) SetCoinbase(addr common.Address) {
	if b.gasPool != nil {
		if len(b.txs) > 0 {
			panic("coinbase must be set before adding transactions")
		}
		panic("coinbase can only be set once")
	}
	b.header.Coinbase = addr
	b.gasPool = new(GasPool).AddGas(b.header.GasLimit)
}

func (b *BlockGen) SetExtra(data []byte) {
	b.header.Extra = data
}

func (b *BlockGen) AddTx(tx *types.Transaction) {
	if b.gasPool == nil {
		b.SetCoinbase(common.Address{})
	}
	b.statedb.Prepare(tx.Hash(), common.Hash{}, len(b.txs))
	receipt, _, err := ApplyTransaction(b.config, nil, &b.header.Coinbase, b.gasPool, b.statedb, b.header, tx, &b.header.GasUsed, vm.Config{}, b.delegatedb, b.header.Time.Uint64(),false)
	if err != nil {
		panic(err)
	}
	b.txs = append(b.txs, tx)
	b.receipts = append(b.receipts, receipt)
}

func (b *BlockGen) Number() *big.Int {
	return new(big.Int).Set(b.header.Number)
}

func (b *BlockGen) AddUncheckedReceipt(receipt *types.Receipt) {
	b.receipts = append(b.receipts, receipt)
}

func (b *BlockGen) TxNonce(addr common.Address) uint64 {
	if !b.statedb.Exist(addr) {
		panic("account does not exist")
	}
	return b.statedb.GetNonce(addr)
}

func (b *BlockGen) AddUncle(h *types.Header) {
	b.uncles = append(b.uncles, h)
}

func (b *BlockGen) PrevBlock(index int) *types.Block {
	if index >= b.i {
		panic("block index out of range")
	}
	if index == -1 {
		return b.parent
	}
	return b.chain[index]
}

func (b *BlockGen) OffsetTime(seconds int64) {
	b.header.Time.Add(b.header.Time, new(big.Int).SetInt64(seconds))
	if b.header.Time.Cmp(b.parent.Header().Time) <= 0 {
		panic("block time out of range")
	}

}

func GenerateChain(config *params.ChainConfig, parent *types.Block, aoaEngine consensus.Engine, db aoadb.Database, n int, gen func(int, *BlockGen)) ([]*types.Block, []types.Receipts) {
	if config == nil {
		config = params.TestChainConfig
	}
	blocks, receipts := make(types.Blocks, n), make([]types.Receipts, n)
	genblock := func(i int, parent *types.Block, statedb *state.StateDB, delegatedb *delegatestate.DelegateDB) (*types.Block, types.Receipts) {

		blockchain, _ := NewBlockChain(db, config, aoaEngine, vm.Config{},nil)
		defer blockchain.Stop()

		b := &BlockGen{i: i, parent: parent, chain: blocks, chainReader: blockchain, statedb: statedb, config: config, engine: aoaEngine, delegatedb: delegatedb}
		b.header = makeHeader(b.chainReader, parent, statedb, delegatedb)

		if gen != nil {
			gen(i, b)
		}

		if b.engine != nil {
			block, _ := b.engine.Finalize(b.chainReader, b.header, statedb,delegatedb, b.txs,b.receipts)

			_, err := statedb.CommitTo(db, false)
			if err != nil {
				panic(fmt.Sprintf("state write error: %v", err))
			}
			_, err = delegatedb.CommitTo(db, false)
			if err != nil {
				panic(fmt.Sprintf("delegate state write error: %v", err))
			}
			return block, b.receipts
		}
		return nil, nil
	}
	for i := 0; i < n; i++ {
		statedb, err := state.New(parent.Root(), state.NewDatabase(db))
		if err != nil {
			panic(err)
		}
		delegatedb, err := delegatestate.New(parent.DelegateRoot(), delegatestate.NewDatabase(db))
		if err != nil {
			panic(err)
		}
		block, receipt := genblock(i, parent, statedb, delegatedb)
		blocks[i] = block
		receipts[i] = receipt
		parent = block
	}
	return blocks, receipts
}

func makeHeader(chain consensus.ChainReader, parent *types.Block, state *state.StateDB,db *delegatestate.DelegateDB) *types.Header {
	var time *big.Int
	if parent.Time() == nil {
		time = big.NewInt(10)
	} else {
		time = new(big.Int).Add(parent.Time(), big.NewInt(10))
	}

	return &types.Header{
		Root:       state.IntermediateRoot(false),
		ParentHash: parent.Hash(),
		Coinbase:   parent.Coinbase(),
		GasLimit:     CalcGasLimit(parent),
		Number:       new(big.Int).Add(parent.Number(), common.Big1),
		Time:         time,
		DelegateRoot: db.IntermediateRoot(false),
	}
}

func newCanonical(aoaEngine consensus.Engine, n int, full bool) (aoadb.Database, *BlockChain, error) {

	gspec := new(Genesis)
	db, _ := aoadb.NewMemDatabase()
	genesis := gspec.MustCommit(db)

	blockchain, _ := NewBlockChain(db, params.AllAuroraProtocolChanges, aoaEngine, vm.Config{},nil)

	if n == 0 {
		return db, blockchain, nil
	}
	if full {

		blocks := makeBlockChain(genesis, n, aoaEngine, db, canonicalSeed)
		_, err := blockchain.InsertChain(blocks)
		return db, blockchain, err
	}

	headers := makeHeaderChain(genesis.Header(), n, aoaEngine, db, canonicalSeed)
	_, err := blockchain.InsertHeaderChain(headers, 1)
	return db, blockchain, err
}

func makeHeaderChain(parent *types.Header, n int, aoaEngine consensus.Engine, db aoadb.Database, seed int) []*types.Header {
	blocks := makeBlockChain(types.NewBlockWithHeader(parent), n, aoaEngine, db, seed)
	headers := make([]*types.Header, len(blocks))
	for i, block := range blocks {
		headers[i] = block.Header()
	}
	return headers
}

func makeBlockChain(parent *types.Block, n int, aoaEngine consensus.Engine, db aoadb.Database, seed int) []*types.Block {
	blocks, _ := GenerateChain(params.TestChainConfig, parent, aoaEngine, db, n, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{0: byte(seed), 19: byte(i)})
	})
	return blocks
}
