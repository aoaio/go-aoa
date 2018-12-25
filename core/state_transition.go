package core

import (
	"errors"
	"math"
	"math/big"

	"fmt"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/core/vm"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/params"
)

var (
	errInsufficientBalanceForGas = errors.New("insufficient balance to pay for gas")
)

/*
The State Transitioning Model

A state transition is a change made when a transaction is applied to the current world state
The state transitioning model does all all the necessary work to work out a valid new state root.

1) Nonce handling
2) Pre pay gas
3) Create a new state object if the recipient is \0*32
4) Value transfer
== If contract creation ==
  4a) Attempt to run transaction data
  4b) If valid, use result as code for the new state object
== end ==
5) Run Script section
6) Derive new state root
*/
type StateTransition struct {
	gp         *GasPool 
	msg        Message
	gas        uint64 
	gasPrice   *big.Int
	initialGas uint64 
	value      *big.Int
	data       []byte
	state      vm.StateDB
	evm        *vm.EVM
}

type Message interface {
	From() common.Address

	To() *common.Address

	GasPrice() *big.Int
	Gas() uint64
	Value() *big.Int

	Nonce() uint64
	CheckNonce() bool
	Data() []byte
	Action() uint64
	Vote() []types.Vote
	Asset() *common.Address
	AssetInfo() types.AssetInfo
	SubAddress() string
	Abi() string
}

func IntrinsicGas(data []byte, action uint64) (uint64, error) {

	var gas uint64
	switch action {
	case types.ActionTrans:
		gas = params.TxGas
	case types.ActionRegister:

		gas = params.TxGas
	case types.ActionAddVote, types.ActionSubVote:
		gas = params.TxGas
	case types.ActionPublishAsset:
		gas = params.TxGasAssetPublish
	case types.ActionCreateContract:
		gas = params.TxGasContractCreation
	case types.ActionCallContract:
		gas = params.TxGas
	}

	if len(data) > 0 {

		var nz uint64
		for _, byt := range data {
			if byt != 0 {
				nz++
			}
		}

		if (math.MaxUint64-gas)/params.TxDataNonZeroGas < nz {
			return 0, vm.ErrOutOfGas
		}
		gas += nz * params.TxDataNonZeroGas

		z := uint64(len(data)) - nz
		if (math.MaxUint64-gas)/params.TxDataZeroGas < z {
			return 0, vm.ErrOutOfGas
		}
		gas += z * params.TxDataZeroGas
	}
	return gas, nil
}

func NewStateTransition(evm *vm.EVM, msg Message, gp *GasPool) *StateTransition {
	return &StateTransition{
		gp:       gp,
		evm:      evm,
		msg:      msg,
		gasPrice: msg.GasPrice(),
		value:    msg.Value(),
		data:     msg.Data(),
		state:    evm.StateDB,
	}
}

func ApplyMessage(evm *vm.EVM, msg Message, gp *GasPool) ([]byte, uint64, bool, error) {
	return NewStateTransition(evm, msg, gp).TransitionDb()
}

func (st *StateTransition) from() vm.AccountRef {
	f := st.msg.From()
	if !st.state.Exist(f) {
		st.state.CreateAccount(f)
	}
	return vm.AccountRef(f)
}

func (st *StateTransition) to() vm.AccountRef {
	if st.msg == nil {
		return vm.AccountRef{}
	}
	to := st.msg.To()
	if to == nil {
		return vm.AccountRef{} 
	}

	reference := vm.AccountRef(*to)
	if !st.state.Exist(*to) {
		st.state.CreateAccount(*to)
	}
	return reference
}

func (st *StateTransition) useGas(amount uint64) error {
	if st.gas < amount {
		return vm.ErrOutOfGas
	}
	st.gas -= amount

	return nil
}

func (st *StateTransition) buyGas() error {
	var (
		state  = st.state
		sender = st.from()
	)
	mgval := new(big.Int).Mul(new(big.Int).SetUint64(st.msg.Gas()), st.gasPrice)
	if state.GetBalance(sender.Address()).Cmp(mgval) < 0 {
		return errInsufficientBalanceForGas
	}
	if err := st.gp.SubGas(st.msg.Gas()); err != nil {
		return err
	}
	st.gas += st.msg.Gas()

	st.initialGas = st.msg.Gas()
	state.SubBalance(sender.Address(), mgval)
	return nil
}

func (st *StateTransition) preCheck() error {

	if st.msg.Action() > types.ActionCallContract {
		return fmt.Errorf("Illegal action: %d", st.msg.Action())
	}

	msg := st.msg
	sender := st.from()

	if msg.CheckNonce() {
		nonce := st.state.GetNonce(sender.Address())
		if nonce < msg.Nonce() {
			return ErrNonceTooHigh
		} else if nonce > msg.Nonce() {
			return ErrNonceTooLow
		}
	}
	return st.buyGas()
}

func (st *StateTransition) TransitionDb() (ret []byte, usedGas uint64, failed bool, err error) {

	if err = st.preCheck(); err != nil {
		return
	}
	msg := st.msg
	sender := st.from() 

	gas, err := IntrinsicGas(st.data, msg.Action())
	if err = st.useGas(gas); err != nil {
		return nil, 0, false, err
	}

	var (
		evm = st.evm

		vmerr error
	)
	switch msg.Action() {
	case types.ActionCreateContract:
		if len(st.data) == 0 {
			return nil, 0, true, errors.New("Create contract but data is nil")
		}
		ret, _, st.gas, vmerr = evm.Create(sender, st.data, st.gas, msg.Asset(), st.value, st.msg.Abi())
	case types.ActionPublishAsset:
		/*
		 when an error occurs, revert state db and refund the gas payed by IntrinsicGas
		*/
		snapshot := st.state.Snapshot()
		err = st.publishAsset()
		if err != nil {
			st.state.RevertToSnapshot(snapshot, evm.ChainConfig().IsEpiphron(evm.BlockNumber))
			log.Error("PublishAsset error", "from", st.from().Address().String(), "err", err)
			return nil, 0, true, err
		}
	default:

		st.state.SetNonce(sender.Address(), st.state.GetNonce(sender.Address())+1)
		ret, st.gas, vmerr = evm.Call(sender, st.to().Address(), st.data, st.gas, msg.Action(), st.value, msg.Vote(), msg.Asset())
	}
	if vmerr != nil {
		log.Info("VM returned with error", "err", vmerr)

		if vmerr == vm.ErrInsufficientBalance {
			return nil, 0, false, vmerr
		}
		if vmerr == vm.ErrVote {
			return nil, 0, false, vmerr
		}
	}

	st.refundGas()
	st.state.AddBalance(st.evm.Coinbase, new(big.Int).Mul(new(big.Int).SetUint64(st.gasUsed()), st.gasPrice))

	return ret, st.gasUsed(), vmerr != nil, err
}

func (st *StateTransition) refundGas() {

	refund := st.gasUsed() / 2
	if refund > st.state.GetRefund() {
		refund = st.state.GetRefund()
	}
	st.gas += refund

	sender := st.from()

	remaining := new(big.Int).Mul(new(big.Int).SetUint64(st.gas), st.gasPrice)
	st.state.AddBalance(sender.Address(), remaining)

	st.gp.AddGas(st.gas)
}

func (st *StateTransition) gasUsed() uint64 {
	return st.initialGas - st.gas
}

func (st *StateTransition) publishAsset() error {
	ai := st.msg.AssetInfo()
	return st.state.PublishAsset(st.from().Address(), ai)
}
