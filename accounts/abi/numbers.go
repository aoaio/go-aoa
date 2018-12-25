package abi

import (
	"math/big"
	"reflect"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/common/math"
)

var (
	big_t      = reflect.TypeOf(&big.Int{})
	derefbig_t = reflect.TypeOf(big.Int{})
	uint8_t    = reflect.TypeOf(uint8(0))
	uint16_t   = reflect.TypeOf(uint16(0))
	uint32_t   = reflect.TypeOf(uint32(0))
	uint64_t   = reflect.TypeOf(uint64(0))
	int_t      = reflect.TypeOf(int(0))
	int8_t     = reflect.TypeOf(int8(0))
	int16_t    = reflect.TypeOf(int16(0))
	int32_t    = reflect.TypeOf(int32(0))
	int64_t    = reflect.TypeOf(int64(0))
	address_t  = reflect.TypeOf(common.Address{})
	int_ts     = reflect.TypeOf([]int(nil))
	int8_ts    = reflect.TypeOf([]int8(nil))
	int16_ts   = reflect.TypeOf([]int16(nil))
	int32_ts   = reflect.TypeOf([]int32(nil))
	int64_ts   = reflect.TypeOf([]int64(nil))
)

func U256(n *big.Int) []byte {
	return math.PaddedBigBytes(math.U256(n), 32)
}

func isSigned(v reflect.Value) bool {
	switch v.Type() {
	case int_ts, int8_ts, int16_ts, int32_ts, int64_ts, int_t, int8_t, int16_t, int32_t, int64_t:
		return true
	}
	return false
}
