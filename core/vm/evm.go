package vm

import (
	"math/big"
	"sync/atomic"
	"time"

	"errors"
	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/core/types"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/log"
	"github.com/Aurorachain/go-Aurora/params"
)

var emptyCodeHash = crypto.Keccak256Hash(nil)

type (

	CanTransferFunc func(StateDB, common.Address, *common.Address, *big.Int) bool
	TransferFunc    func(StateDB, common.Address, common.Address, *common.Address, *big.Int)

	GetHashFunc func(uint64) common.Hash
	VoteFunc    func(StateDB, common.Address, *big.Int, []types.Vote, *map[common.Address]types.Candidate, int64) error
)

func run(evm *EVM, contract *Contract, input []byte) ([]byte, error) {
	if contract.CodeAddr != nil {
		precompiles := PrecompiledContracts
		if p := precompiles[*contract.CodeAddr]; p != nil {
			return RunPrecompiledContract(p, input, contract)
		}
	}
	return evm.interpreter.Run(contract, input)
}

type Context struct {

	CanTransfer CanTransferFunc

	Transfer TransferFunc

	GetHash GetHashFunc

	Vote VoteFunc

	Origin   common.Address
	GasPrice *big.Int

	Coinbase     common.Address
	GasLimit     uint64
	BlockNumber  *big.Int
	Time         *big.Int
	Difficulty   *big.Int
	DelegateList *map[common.Address]types.Candidate
}

type EVM struct {

	Context

	StateDB StateDB

	depth int

	chainConfig *params.ChainConfig

	chainRules params.Rules

	vmConfig Config

	interpreter *Interpreter

	abort int32

	callGasTemp uint64

	WatchInnerTx bool

	InnerTxs []*types.InnerTx
}

var maxElectDelegate int64

func NewEVM(ctx Context, statedb StateDB, chainConfig *params.ChainConfig, vmConfig Config) *EVM {
	maxElectDelegate = chainConfig.MaxElectDelegate.Int64()
	evm := &EVM{
		Context:     ctx,
		StateDB:     statedb,
		vmConfig:    vmConfig,
		chainConfig: chainConfig,
		chainRules:  chainConfig.Rules(ctx.BlockNumber),
	}

	evm.interpreter = NewInterpreter(evm, vmConfig)
	return evm
}

func (evm *EVM) Cancel() {
	atomic.StoreInt32(&evm.abort, 1)
}

func (evm *EVM) Call(caller ContractRef, addr common.Address, input []byte, gas uint64, action uint64, value *big.Int, args ...interface{}) (ret []byte, leftOverGas uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}
	var voteList []types.Vote
	var asset *common.Address
	for _, a := range args {
		switch a.(type) {
		case []types.Vote:
			voteList = a.([]types.Vote)
		case *common.Address:
			asset = a.(*common.Address)
		default:
			return nil, gas, errors.New("wrong parameter")
		}
	}

	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	registerCost := new(big.Int)
	registerCost.SetString(params.TxGasAgentCreation, 10)

	checkValue := value
	if action == types.ActionSubVote {
		checkValue = big.NewInt(1).Mul(big.NewInt(-1), value)
	}

	if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), asset, checkValue) ||
		(action == types.ActionRegister && !evm.Context.CanTransfer(evm.StateDB, caller.Address(), nil, registerCost)) {
		return nil, gas, ErrInsufficientBalance
	}

	var (
		to       = AccountRef(addr)
		snapshot = evm.StateDB.Snapshot()
	)
	if !evm.StateDB.Exist(addr) {
		precompiles := PrecompiledContracts
		if precompiles[addr] == nil && value.Sign() == 0 {
			if evm.vmConfig.Debug && evm.depth == 0 {
				evm.vmConfig.Tracer.CaptureStart(caller.Address(), addr, false, input, gas, value)
				evm.vmConfig.Tracer.CaptureEnd(ret, 0, 0, nil)
			}
			return nil, gas, nil
		}
		evm.StateDB.CreateAccount(addr)
	}
	if action == types.ActionAddVote || action == types.ActionSubVote {
		if len(voteList) == 0 {
			return nil, gas, errors.New("empty vote list")
		}
		err := evm.Vote(evm.StateDB, caller.Address(), value, voteList, evm.DelegateList, maxElectDelegate)
		if err != nil {
			log.Warn("HereVote", "err", err)
			evm.StateDB.RevertToSnapshot(snapshot, evm.chainConfig.IsEpiphron(evm.BlockNumber))
			return nil, gas, err
		}
	} else if action == types.ActionRegister {

		if _, ok := (*evm.DelegateList)[caller.Address()]; ok {
			return nil, gas, errors.New("Address " + caller.Address().Hex() + " have already register delegate")
		}
	} else {
		evm.Transfer(evm.StateDB, caller.Address(), to.Address(), asset, value)
	}

	contract := NewContract(caller, to, asset, value, gas)
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	start := time.Now()

	if evm.vmConfig.Debug && evm.depth == 0 {
		evm.vmConfig.Tracer.CaptureStart(caller.Address(), addr, false, input, gas, value)

		defer func() {
			evm.vmConfig.Tracer.CaptureEnd(ret, gas-contract.Gas, time.Since(start), err)
		}()
	}
	ret, err = run(evm, contract, input)

	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot, evm.chainConfig.IsEpiphron(evm.BlockNumber))
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}
	return ret, contract.Gas, err
}

func (evm *EVM) CallCode(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}

	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	if !evm.CanTransfer(evm.StateDB, caller.Address(), nil, value) {
		return nil, gas, ErrInsufficientBalance
	}

	var (
		snapshot = evm.StateDB.Snapshot()
		to       = AccountRef(caller.Address())
	)

	contract := NewContract(caller, to, nil, value, gas)
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	ret, err = run(evm, contract, input)
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot, evm.chainConfig.IsEpiphron(evm.BlockNumber))
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}
	return ret, contract.Gas, err
}

func (evm *EVM) DelegateCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}

	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	var (
		snapshot = evm.StateDB.Snapshot()
		to       = AccountRef(caller.Address())
	)

	contract := NewContract(caller, to, nil, nil, gas).AsDelegate()
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	ret, err = run(evm, contract, input)
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot, evm.chainConfig.IsEpiphron(evm.BlockNumber))
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}
	return ret, contract.Gas, err
}

func (evm *EVM) StaticCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil
	}

	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	if !evm.interpreter.readOnly {
		evm.interpreter.readOnly = true
		defer func() { evm.interpreter.readOnly = false }()
	}

	var (
		to       = AccountRef(addr)
		snapshot = evm.StateDB.Snapshot()
	)

	contract := NewContract(caller, to, nil, new(big.Int), gas)
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	ret, err = run(evm, contract, input)
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot, evm.chainConfig.IsEpiphron(evm.BlockNumber))
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}
	return ret, contract.Gas, err
}

func (evm *EVM) Create(caller ContractRef, code []byte, gas uint64, asset *common.Address, value *big.Int, abi string) (ret []byte, contractAddr common.Address, leftOverGas uint64, err error) {

	if evm.depth > int(params.CallCreateDepth) {
		return nil, common.Address{}, gas, ErrDepth
	}
	if !evm.CanTransfer(evm.StateDB, caller.Address(), asset, value) {
		return nil, common.Address{}, gas, ErrInsufficientBalance
	}

	nonce := evm.StateDB.GetNonce(caller.Address())
	evm.StateDB.SetNonce(caller.Address(), nonce+1)

	contractAddr = crypto.CreateAddress(caller.Address(), nonce)
	contractHash := evm.StateDB.GetCodeHash(contractAddr)
	if evm.StateDB.GetNonce(contractAddr) != 0 || (contractHash != (common.Hash{}) && contractHash != emptyCodeHash) {
		return nil, common.Address{}, 0, ErrContractAddressCollision
	}

	snapshot := evm.StateDB.Snapshot()
	evm.StateDB.CreateAccount(contractAddr)

	evm.Transfer(evm.StateDB, caller.Address(), contractAddr, asset, value)

	contract := NewContract(caller, AccountRef(contractAddr), asset, value, gas)
	contract.SetCallCode(&contractAddr, crypto.Keccak256Hash(code), code)

	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, contractAddr, gas, nil
	}

	if evm.vmConfig.Debug && evm.depth == 0 {
		evm.vmConfig.Tracer.CaptureStart(caller.Address(), contractAddr, true, code, gas, value)
	}
	start := time.Now()

	ret, err = run(evm, contract, nil)

	maxCodeSizeExceeded := len(ret) > params.MaxCodeSize

	if err == nil && !maxCodeSizeExceeded {
		createDataGas := uint64(len(ret)) * params.CreateDataGas
		createDataGas += uint64(len(abi)) * params.TxABIGas
		if contract.UseGas(createDataGas) {
			evm.StateDB.SetCode(contractAddr, ret)
			evm.StateDB.SetAbi(contractAddr, abi)
		} else {
			err = ErrCodeStoreOutOfGas
		}
	}

	if maxCodeSizeExceeded || (err != nil && (err != ErrCodeStoreOutOfGas)) {
		evm.StateDB.RevertToSnapshot(snapshot, evm.chainConfig.IsEpiphron(evm.BlockNumber))
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}

	if maxCodeSizeExceeded && err == nil {
		err = errMaxCodeSizeExceeded
	}
	if evm.vmConfig.Debug && evm.depth == 0 {
		evm.vmConfig.Tracer.CaptureEnd(ret, gas-contract.Gas, time.Since(start), err)
	}
	return ret, contractAddr, contract.Gas, err
}

func (evm *EVM) ChainConfig() *params.ChainConfig { return evm.chainConfig }

func (evm *EVM) Interpreter() *Interpreter { return evm.interpreter }

func (evm *EVM) watchInnerTx(from common.Address, to common.Address, asset *common.Address, value *big.Int) {
	if evm.WatchInnerTx && evm.vmConfig.WatchInnerTx && big.NewInt(0).Cmp(value) < 0 {
		itx := types.InnerTx{From: from, To: to, AssetID: asset, Value: new(big.Int).Set(value)}
		evm.InnerTxs = append(evm.InnerTxs, &itx)
	}
}
