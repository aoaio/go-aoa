package core

import (
	"github.com/Aurorachain/go-Aurora/core/state"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/core/vm"
	"github.com/Aurorachain/go-Aurora/consensus/delegatestate"
)

type Validator interface {

	ValidateBody(block *types.Block) error

	ValidateState(block, parent *types.Block, state *state.StateDB, receipts types.Receipts, usedGas uint64,db *delegatestate.DelegateDB) error
}

type Processor interface {
	Process(block *types.Block, statedb *state.StateDB, cfg vm.Config,db *delegatestate.DelegateDB) (types.Receipts, []*types.Log, uint64, error)
}
