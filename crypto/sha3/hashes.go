package sha3

import (
	"hash"
)

func NewKeccak256() hash.Hash { return &state{rate: 136, outputLen: 32, dsbyte: 0x01} }

func NewKeccak512() hash.Hash { return &state{rate: 72, outputLen: 64, dsbyte: 0x01} }

func New224() hash.Hash { return &state{rate: 144, outputLen: 28, dsbyte: 0x06} }

func New256() hash.Hash { return &state{rate: 136, outputLen: 32, dsbyte: 0x06} }

func New384() hash.Hash { return &state{rate: 104, outputLen: 48, dsbyte: 0x06} }

func New512() hash.Hash { return &state{rate: 72, outputLen: 64, dsbyte: 0x06} }

func Sum224(data []byte) (digest [28]byte) {
	h := New224()
	h.Write(data)
	h.Sum(digest[:0])
	return
}

func Sum256(data []byte) (digest [32]byte) {
	h := New256()
	h.Write(data)
	h.Sum(digest[:0])
	return
}

func Sum384(data []byte) (digest [48]byte) {
	h := New384()
	h.Write(data)
	h.Sum(digest[:0])
	return
}

func Sum512(data []byte) (digest [64]byte) {
	h := New512()
	h.Write(data)
	h.Sum(digest[:0])
	return
}
