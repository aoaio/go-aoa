package common

import (
	"encoding/hex"
	"math/big"
	"strings"
)

func ToHex(b []byte) string {
	hex := Bytes2Hex(b)

	if len(hex) == 0 {
		hex = "0"
	}
	return "0x" + hex
}

func FromHex(s string) []byte {
	if len(s) > 1 {
		if s[0:2] == "0x" || s[0:2] == "0X" {
			s = s[2:]
		}
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	return Hex2Bytes(s)
}

func FromAoAHex(s string) []byte {
	if len(s) > 1 {
		if strings.EqualFold(s[0:3], "aoa") {
			s = s[3:]
		}
	}
	return Hex2Bytes(s)
}

func CopyBytes(b []byte) (copiedBytes []byte) {
	if b == nil {
		return nil
	}
	copiedBytes = make([]byte, len(b))
	copy(copiedBytes, b)

	return
}

func hasHexPrefix(str string) bool {
	return len(str) >= 2 && str[0] == '0' && (str[1] == 'x' || str[1] == 'X')
}

func hasAOAPrefix(str string) bool {
	return len(str) >= 3 && (str[0] == 'A' || str[0] == 'a') && (str[1] == 'O' || str[1] == 'o') && (str[2] == 'A' || str[2] == 'a')
}

func isHexCharacter(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

func isHex(str string) bool {
	if len(str)%2 != 0 {
		return false
	}
	for _, c := range []byte(str) {
		if !isHexCharacter(c) {
			return false
		}
	}
	return true
}

func Bytes2Hex(d []byte) string {
	return hex.EncodeToString(d)
}

func Hex2Bytes(str string) []byte {
	h, _ := hex.DecodeString(str)

	return h
}

func Hex2BytesFixed(str string, flen int) []byte {
	h, _ := hex.DecodeString(str)
	if len(h) == flen {
		return h
	} else {
		if len(h) > flen {
			return h[len(h)-flen:]
		} else {
			hh := make([]byte, flen)
			copy(hh[flen-len(h):flen], h[:])
			return hh
		}
	}
}

func RightPadBytes(slice []byte, l int) []byte {
	if l <= len(slice) {
		return slice
	}

	padded := make([]byte, l)
	copy(padded, slice)

	return padded
}

func LeftPadBytes(slice []byte, l int) []byte {
	if l <= len(slice) {
		return slice
	}

	padded := make([]byte, l)
	copy(padded[l-len(slice):], slice)

	return padded
}

func BigToAsciiString(bi *big.Int) string {
	b := bi.Bytes()
	if b[0] == 0x0 {
		return ""
	}
	find := false
	r, i, l := 0, len(b)/2, len(b)
	for ; r < l; i = (r + l) / 2 {
		if b[i] == 0x0 {
			if b[i-1] != 0x0 {
				find = true
				break
			} else {
				l = i
			}
		} else {
			r = i
		}
	}
	if find {
		bstr := make([]byte, i)
		copy(bstr, b[:i])
		return string(bstr)
	}
	return ""
}
