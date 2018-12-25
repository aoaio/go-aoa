package bitutil

import "bytes"

func Fuzz(data []byte) int {
	if len(data) == 0 {
		return -1
	}
	if data[0]%2 == 0 {
		return fuzzEncode(data[1:])
	}
	return fuzzDecode(data[1:])
}

func fuzzEncode(data []byte) int {
	proc, _ := bitsetDecodeBytes(bitsetEncodeBytes(data), len(data))
	if !bytes.Equal(data, proc) {
		panic("content mismatch")
	}
	return 0
}

func fuzzDecode(data []byte) int {
	blob, err := bitsetDecodeBytes(data, 1024)
	if err != nil {
		return 0
	}
	if comp := bitsetEncodeBytes(blob); !bytes.Equal(comp, data) {
		panic("content mismatch")
	}
	return 0
}
