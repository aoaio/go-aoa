package abi

import (
	"bytes"
	"math/big"
	"reflect"
	"testing"
)

func TestNumberTypes(t *testing.T) {
	ubytes := make([]byte, 32)
	ubytes[31] = 1

	unsigned := U256(big.NewInt(1))
	if !bytes.Equal(unsigned, ubytes) {
		t.Errorf("expected %x got %x", ubytes, unsigned)
	}
}

func TestSigned(t *testing.T) {
	if isSigned(reflect.ValueOf(uint(10))) {
		t.Error("signed")
	}

	if !isSigned(reflect.ValueOf(int(10))) {
		t.Error("not signed")
	}
}
