// +build amd64,!appengine,!gccgo

package sha3

//go:noescape

func keccakF1600(state *[25]uint64)
