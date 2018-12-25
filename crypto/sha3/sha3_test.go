package sha3

import (
	"bytes"
	"compress/flate"
	"encoding/hex"
	"encoding/json"
	"hash"
	"os"
	"strings"
	"testing"
)

const (
	testString  = "brekeccakkeccak koax koax"
	katFilename = "testdata/keccakKats.json.deflate"
)

func newHashShake128() hash.Hash {
	return &state{rate: 168, dsbyte: 0x1f, outputLen: 512}
}
func newHashShake256() hash.Hash {
	return &state{rate: 136, dsbyte: 0x1f, outputLen: 512}
}

var testDigests = map[string]func() hash.Hash{
	"SHA3-224": New224,
	"SHA3-256": New256,
	"SHA3-384": New384,
	"SHA3-512": New512,
	"SHAKE128": newHashShake128,
	"SHAKE256": newHashShake256,
}

var testShakes = map[string]func() ShakeHash{
	"SHAKE128": NewShake128,
	"SHAKE256": NewShake256,
}

type KeccakKats struct {
	Kats map[string][]struct {
		Digest  string `json:"digest"`
		Length  int64  `json:"length"`
		Message string `json:"message"`
	}
}

func testUnalignedAndGeneric(t *testing.T, testf func(impl string)) {
	xorInOrig, copyOutOrig := xorIn, copyOut
	xorIn, copyOut = xorInGeneric, copyOutGeneric
	testf("generic")
	if xorImplementationUnaligned != "generic" {
		xorIn, copyOut = xorInUnaligned, copyOutUnaligned
		testf("unaligned")
	}
	xorIn, copyOut = xorInOrig, copyOutOrig
}

func TestKeccakKats(t *testing.T) {
	testUnalignedAndGeneric(t, func(impl string) {

		deflated, err := os.Open(katFilename)
		if err != nil {
			t.Errorf("error opening %s: %s", katFilename, err)
		}
		file := flate.NewReader(deflated)
		dec := json.NewDecoder(file)
		var katSet KeccakKats
		err = dec.Decode(&katSet)
		if err != nil {
			t.Errorf("error decoding KATs: %s", err)
		}

		for functionName, kats := range katSet.Kats {
			d := testDigests[functionName]()
			for _, kat := range kats {
				d.Reset()
				in, err := hex.DecodeString(kat.Message)
				if err != nil {
					t.Errorf("error decoding KAT: %s", err)
				}
				d.Write(in[:kat.Length/8])
				got := strings.ToUpper(hex.EncodeToString(d.Sum(nil)))
				if got != kat.Digest {
					t.Errorf("function=%s, implementation=%s, length=%d\nmessage:\n  %s\ngot:\n  %s\nwanted:\n %s",
						functionName, impl, kat.Length, kat.Message, got, kat.Digest)
					t.Logf("wanted %+v", kat)
					t.FailNow()
				}
				continue
			}
		}
	})
}

func TestUnalignedWrite(t *testing.T) {
	testUnalignedAndGeneric(t, func(impl string) {
		buf := sequentialBytes(0x10000)
		for alg, df := range testDigests {
			d := df()
			d.Reset()
			d.Write(buf)
			want := d.Sum(nil)
			d.Reset()
			for i := 0; i < len(buf); {

				offsets := [17]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1}
				for _, j := range offsets {
					if v := len(buf) - i; v < j {
						j = v
					}
					d.Write(buf[i : i+j])
					i += j
				}
			}
			got := d.Sum(nil)
			if !bytes.Equal(got, want) {
				t.Errorf("Unaligned writes, implementation=%s, alg=%s\ngot %q, want %q", impl, alg, got, want)
			}
		}
	})
}

func TestAppend(t *testing.T) {
	testUnalignedAndGeneric(t, func(impl string) {
		d := New224()

		for capacity := 2; capacity <= 66; capacity += 64 {

			buf := make([]byte, 2, capacity)
			d.Reset()
			d.Write([]byte{0xcc})
			buf = d.Sum(buf)
			expected := "0000DF70ADC49B2E76EEE3A6931B93FA41841C3AF2CDF5B32A18B5478C39"
			if got := strings.ToUpper(hex.EncodeToString(buf)); got != expected {
				t.Errorf("got %s, want %s", got, expected)
			}
		}
	})
}

func TestAppendNoRealloc(t *testing.T) {
	testUnalignedAndGeneric(t, func(impl string) {
		buf := make([]byte, 1, 200)
		d := New224()
		d.Write([]byte{0xcc})
		buf = d.Sum(buf)
		expected := "00DF70ADC49B2E76EEE3A6931B93FA41841C3AF2CDF5B32A18B5478C39"
		if got := strings.ToUpper(hex.EncodeToString(buf)); got != expected {
			t.Errorf("%s: got %s, want %s", impl, got, expected)
		}
	})
}

func TestSqueezing(t *testing.T) {
	testUnalignedAndGeneric(t, func(impl string) {
		for functionName, newShakeHash := range testShakes {
			d0 := newShakeHash()
			d0.Write([]byte(testString))
			ref := make([]byte, 32)
			d0.Read(ref)

			d1 := newShakeHash()
			d1.Write([]byte(testString))
			var multiple []byte
			for range ref {
				one := make([]byte, 1)
				d1.Read(one)
				multiple = append(multiple, one...)
			}
			if !bytes.Equal(ref, multiple) {
				t.Errorf("%s (%s): squeezing %d bytes one at a time failed", functionName, impl, len(ref))
			}
		}
	})
}

func sequentialBytes(size int) []byte {
	result := make([]byte, size)
	for i := range result {
		result[i] = byte(i)
	}
	return result
}

func BenchmarkPermutationFunction(b *testing.B) {
	b.SetBytes(int64(200))
	var lanes [25]uint64
	for i := 0; i < b.N; i++ {
		keccakF1600(&lanes)
	}
}

func benchmarkHash(b *testing.B, h hash.Hash, size, num int) {
	b.StopTimer()
	h.Reset()
	data := sequentialBytes(size)
	b.SetBytes(int64(size * num))
	b.StartTimer()

	var state []byte
	for i := 0; i < b.N; i++ {
		for j := 0; j < num; j++ {
			h.Write(data)
		}
		state = h.Sum(state[:0])
	}
	b.StopTimer()
	h.Reset()
}

func benchmarkShake(b *testing.B, h ShakeHash, size, num int) {
	b.StopTimer()
	h.Reset()
	data := sequentialBytes(size)
	d := make([]byte, 32)

	b.SetBytes(int64(size * num))
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		h.Reset()
		for j := 0; j < num; j++ {
			h.Write(data)
		}
		h.Read(d)
	}
}

func BenchmarkSha3_512_MTU(b *testing.B) { benchmarkHash(b, New512(), 1350, 1) }
func BenchmarkSha3_384_MTU(b *testing.B) { benchmarkHash(b, New384(), 1350, 1) }
func BenchmarkSha3_256_MTU(b *testing.B) { benchmarkHash(b, New256(), 1350, 1) }
func BenchmarkSha3_224_MTU(b *testing.B) { benchmarkHash(b, New224(), 1350, 1) }

func BenchmarkShake128_MTU(b *testing.B)  { benchmarkShake(b, NewShake128(), 1350, 1) }
func BenchmarkShake256_MTU(b *testing.B)  { benchmarkShake(b, NewShake256(), 1350, 1) }
func BenchmarkShake256_16x(b *testing.B)  { benchmarkShake(b, NewShake256(), 16, 1024) }
func BenchmarkShake256_1MiB(b *testing.B) { benchmarkShake(b, NewShake256(), 1024, 1024) }

func BenchmarkSha3_512_1MiB(b *testing.B) { benchmarkHash(b, New512(), 1024, 1024) }

func Example_sum() {
	buf := []byte("some data to hash")

	h := make([]byte, 64)

	ShakeSum256(h, buf)
}

func Example_mac() {
	k := []byte("this is a secret key; you should generate a strong random key that's at least 32 bytes long")
	buf := []byte("and this is some data to authenticate")

	h := make([]byte, 32)
	d := NewShake256()

	d.Write(k)

	d.Write(buf)

	d.Read(h)
}
