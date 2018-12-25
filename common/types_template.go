// +build none

package common

import "math/big"

type _N_ [_S_]byte

func BytesTo_N_(b []byte) _N_ {
	var h _N_
	h.SetBytes(b)
	return h
}
func StringTo_N_(s string) _N_ { return BytesTo_N_([]byte(s)) }
func BigTo_N_(b *big.Int) _N_  { return BytesTo_N_(b.Bytes()) }
func HexTo_N_(s string) _N_    { return BytesTo_N_(FromHex(s)) }

func (h _N_) Str() string   { return string(h[:]) }
func (h _N_) Bytes() []byte { return h[:] }
func (h _N_) Big() *big.Int { return new(big.Int).SetBytes(h[:]) }
func (h _N_) Hex() string   { return "0x" + Bytes2Hex(h[:]) }

func (h *_N_) SetBytes(b []byte) {

	if len(b) > len(h) {
		b = b[len(b)-_S_:]
	}

	for i := len(b) - 1; i >= 0; i-- {
		h[_S_-len(b)+i] = b[i]
	}
}

func (h *_N_) SetString(s string) { h.SetBytes([]byte(s)) }

func (h *_N_) Set(other _N_) {
	for i, v := range other {
		h[i] = v
	}
}
