package rlp

import (
	"fmt"
	"io"
)

type MyCoolType struct {
	Name string
	a, b uint
}

func (x *MyCoolType) EncodeRLP(w io.Writer) (err error) {
	if x == nil {
		err = Encode(w, []uint{0, 0})
	} else {
		err = Encode(w, []uint{x.a, x.b})
	}
	return err
}

func ExampleEncoder() {
	var t *MyCoolType
	bytes, _ := EncodeToBytes(t)
	fmt.Printf("%v → %X\n", t, bytes)

	t = &MyCoolType{Name: "foobar", a: 5, b: 6}
	bytes, _ = EncodeToBytes(t)
	fmt.Printf("%v → %X\n", t, bytes)

}
