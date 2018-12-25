package sha3

import (
	"io"
)

type ShakeHash interface {

	io.Writer

	io.Reader

	Clone() ShakeHash

	Reset()
}

func (d *state) Clone() ShakeHash {
	return d.clone()
}

func NewShake128() ShakeHash { return &state{rate: 168, dsbyte: 0x1f} }

func NewShake256() ShakeHash { return &state{rate: 136, dsbyte: 0x1f} }

func ShakeSum128(hash, data []byte) {
	h := NewShake128()
	h.Write(data)
	h.Read(hash)
}

func ShakeSum256(hash, data []byte) {
	h := NewShake256()
	h.Write(data)
	h.Read(hash)
}
