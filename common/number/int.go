package number

import (
	"math/big"

	"github.com/Aurorachain/go-Aurora/common"
)

var tt256 = new(big.Int).Lsh(big.NewInt(1), 256)
var tt256m1 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
var tt255 = new(big.Int).Lsh(big.NewInt(1), 255)

func limitUnsigned256(x *Number) *Number {
	x.num.And(x.num, tt256m1)
	return x
}

func limitSigned256(x *Number) *Number {
	if x.num.Cmp(tt255) < 0 {
		return x
	} else {
		x.num.Sub(x.num, tt256)
		return x
	}
}

type Initialiser func(n int64) *Number

type Number struct {
	num   *big.Int
	limit func(n *Number) *Number
}

func NewInitialiser(limiter func(*Number) *Number) Initialiser {
	return func(n int64) *Number {
		return &Number{big.NewInt(n), limiter}
	}
}

func Uint256(n int64) *Number {
	return &Number{big.NewInt(n), limitUnsigned256}
}

func Int256(n int64) *Number {
	return &Number{big.NewInt(n), limitSigned256}
}

func Big(n int64) *Number {
	return &Number{big.NewInt(n), func(x *Number) *Number { return x }}
}

func (i *Number) Add(x, y *Number) *Number {
	i.num.Add(x.num, y.num)
	return i.limit(i)
}

func (i *Number) Sub(x, y *Number) *Number {
	i.num.Sub(x.num, y.num)
	return i.limit(i)
}

func (i *Number) Mul(x, y *Number) *Number {
	i.num.Mul(x.num, y.num)
	return i.limit(i)
}

func (i *Number) Div(x, y *Number) *Number {
	i.num.Div(x.num, y.num)
	return i.limit(i)
}

func (i *Number) Mod(x, y *Number) *Number {
	i.num.Mod(x.num, y.num)
	return i.limit(i)
}

func (i *Number) Lsh(x *Number, s uint) *Number {
	i.num.Lsh(x.num, s)
	return i.limit(i)
}

func (i *Number) Pow(x, y *Number) *Number {
	i.num.Exp(x.num, y.num, big.NewInt(0))
	return i.limit(i)
}

func (i *Number) Set(x *Number) *Number {
	i.num.Set(x.num)
	return i.limit(i)
}

func (i *Number) SetBytes(x []byte) *Number {
	i.num.SetBytes(x)
	return i.limit(i)
}

func (i *Number) Cmp(x *Number) int {
	return i.num.Cmp(x.num)
}

func (i *Number) String() string {
	return i.num.String()
}

func (i *Number) Bytes() []byte {
	return i.num.Bytes()
}

func (i *Number) Uint64() uint64 {
	return i.num.Uint64()
}

func (i *Number) Int64() int64 {
	return i.num.Int64()
}

func (i *Number) Int256() *Number {
	return Int(0).Set(i)
}

func (i *Number) Uint256() *Number {
	return Uint(0).Set(i)
}

func (i *Number) FirstBitSet() int {
	for j := 0; j < i.num.BitLen(); j++ {
		if i.num.Bit(j) > 0 {
			return j
		}
	}

	return i.num.BitLen()
}

var (
	Zero       = Uint(0)
	One        = Uint(1)
	Two        = Uint(2)
	MaxUint256 = Uint(0).SetBytes(common.Hex2Bytes("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"))

	MinOne = Int(-1)

	Uint = Uint256
	Int  = Int256
)
