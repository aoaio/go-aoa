package vm

import (
	"math/big"

	"github.com/Aurorachain/go-Aurora/common"
)

type ContractRef interface {
	Address() common.Address
}

type AccountRef common.Address

func (ar AccountRef) Address() common.Address { return (common.Address)(ar) }

type Contract struct {

	CallerAddress common.Address
	caller        ContractRef
	self          ContractRef

	jumpdests destinations

	Code     []byte
	CodeHash common.Hash
	CodeAddr *common.Address
	Input    []byte

	Gas   uint64
	value *big.Int
	asset *common.Address

	Args []byte

	DelegateCall bool
}

func NewContract(caller ContractRef, object ContractRef, asset *common.Address, value *big.Int, gas uint64) *Contract {
	c := &Contract{CallerAddress: caller.Address(), caller: caller, self: object, Args: nil, asset:asset}

	if parent, ok := caller.(*Contract); ok {

		c.jumpdests = parent.jumpdests
	} else {
		c.jumpdests = make(destinations)
	}

	c.Gas = gas

	c.value = value

	return c
}

func (c *Contract) AsDelegate() *Contract {
	c.DelegateCall = true

	parent := c.caller.(*Contract)
	c.CallerAddress = parent.CallerAddress
	c.value = parent.value

	return c
}

func (c *Contract) GetOp(n uint64) OpCode {
	return OpCode(c.GetByte(n))
}

func (c *Contract) GetByte(n uint64) byte {
	if n < uint64(len(c.Code)) {
		return c.Code[n]
	}

	return 0
}

func (c *Contract) Caller() common.Address {
	return c.CallerAddress
}

func (c *Contract) UseGas(gas uint64) (ok bool) {
	if c.Gas < gas {
		return false
	}
	c.Gas -= gas
	return true
}

func (c *Contract) Address() common.Address {
	return c.self.Address()
}

func (c *Contract) Value() *big.Int {
	return c.value
}

func (c *Contract) Asset() *common.Address {
	return c.asset
}

func (self *Contract) SetCode(hash common.Hash, code []byte) {
	self.Code = code
	self.CodeHash = hash
}

func (self *Contract) SetCallCode(addr *common.Address, hash common.Hash, code []byte) {
	self.Code = code
	self.CodeHash = hash
	self.CodeAddr = addr
}
