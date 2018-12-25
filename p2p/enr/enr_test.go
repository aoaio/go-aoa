package enr

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	privkey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	pubkey     = &privkey.PublicKey
)

var rnd = rand.New(rand.NewSource(time.Now().UnixNano()))

func randomString(strlen int) string {
	b := make([]byte, strlen)
	rnd.Read(b)
	return string(b)
}

func TestGetSetID(t *testing.T) {
	id := ID("someid")
	var r Record
	r.Set(id)

	var id2 ID
	require.NoError(t, r.Load(&id2))
	assert.Equal(t, id, id2)
}

func TestGetSetIP4(t *testing.T) {
	ip := IP4{192, 168, 0, 3}
	var r Record
	r.Set(ip)

	var ip2 IP4
	require.NoError(t, r.Load(&ip2))
	assert.Equal(t, ip, ip2)
}

func TestGetSetIP6(t *testing.T) {
	ip := IP6{0x20, 0x01, 0x48, 0x60, 0, 0, 0x20, 0x01, 0, 0, 0, 0, 0, 0, 0x00, 0x68}
	var r Record
	r.Set(ip)

	var ip2 IP6
	require.NoError(t, r.Load(&ip2))
	assert.Equal(t, ip, ip2)
}

func TestGetSetDiscPort(t *testing.T) {
	port := DiscPort(30309)
	var r Record
	r.Set(port)

	var port2 DiscPort
	require.NoError(t, r.Load(&port2))
	assert.Equal(t, port, port2)
}

func TestGetSetSecp256k1(t *testing.T) {
	var r Record
	if err := r.Sign(privkey); err != nil {
		t.Fatal(err)
	}

	var pk Secp256k1
	require.NoError(t, r.Load(&pk))
	assert.EqualValues(t, pubkey, &pk)
}

func TestLoadErrors(t *testing.T) {
	var r Record
	ip4 := IP4{127, 0, 0, 1}
	r.Set(ip4)

	var ip6 IP6
	err := r.Load(&ip6)
	if !IsNotFound(err) {
		t.Error("IsNotFound should return true for missing key")
	}
	assert.Equal(t, &KeyError{Key: ip6.ENRKey(), Err: errNotFound}, err)

	var list []uint
	err = r.Load(WithEntry(ip4.ENRKey(), &list))
	kerr, ok := err.(*KeyError)
	if !ok {
		t.Fatalf("expected KeyError, got %T", err)
	}
	assert.Equal(t, kerr.Key, ip4.ENRKey())
	assert.Error(t, kerr.Err)
	if IsNotFound(err) {
		t.Error("IsNotFound should return false for decoding errors")
	}
}

func TestSortedGetAndSet(t *testing.T) {
	type pair struct {
		k string
		v uint32
	}

	for _, tt := range []struct {
		input []pair
		want  []pair
	}{
		{
			input: []pair{{"a", 1}, {"c", 2}, {"b", 3}},
			want:  []pair{{"a", 1}, {"b", 3}, {"c", 2}},
		},
		{
			input: []pair{{"a", 1}, {"c", 2}, {"b", 3}, {"d", 4}, {"a", 5}, {"bb", 6}},
			want:  []pair{{"a", 5}, {"b", 3}, {"bb", 6}, {"c", 2}, {"d", 4}},
		},
		{
			input: []pair{{"c", 2}, {"b", 3}, {"d", 4}, {"a", 5}, {"bb", 6}},
			want:  []pair{{"a", 5}, {"b", 3}, {"bb", 6}, {"c", 2}, {"d", 4}},
		},
	} {
		var r Record
		for _, i := range tt.input {
			r.Set(WithEntry(i.k, &i.v))
		}
		for i, w := range tt.want {

			got := pair{k: r.pairs[i].k}
			assert.NoError(t, r.Load(WithEntry(w.k, &got.v)))
			assert.Equal(t, w, got)
		}
	}
}

func TestDirty(t *testing.T) {
	var r Record

	if r.Signed() {
		t.Error("Signed returned true for zero record")
	}
	if _, err := rlp.EncodeToBytes(r); err != errEncodeUnsigned {
		t.Errorf("expected errEncodeUnsigned, got %#v", err)
	}

	require.NoError(t, r.Sign(privkey))
	if !r.Signed() {
		t.Error("Signed return false for signed record")
	}
	_, err := rlp.EncodeToBytes(r)
	assert.NoError(t, err)

	r.SetSeq(3)
	if r.Signed() {
		t.Error("Signed returned true for modified record")
	}
	if _, err := rlp.EncodeToBytes(r); err != errEncodeUnsigned {
		t.Errorf("expected errEncodeUnsigned, got %#v", err)
	}
}

func TestGetSetOverwrite(t *testing.T) {
	var r Record

	ip := IP4{192, 168, 0, 3}
	r.Set(ip)

	ip2 := IP4{192, 168, 0, 4}
	r.Set(ip2)

	var ip3 IP4
	require.NoError(t, r.Load(&ip3))
	assert.Equal(t, ip2, ip3)
}

func TestSignEncodeAndDecode(t *testing.T) {
	var r Record
	r.Set(DiscPort(30303))
	r.Set(IP4{127, 0, 0, 1})
	require.NoError(t, r.Sign(privkey))

	blob, err := rlp.EncodeToBytes(r)
	require.NoError(t, err)

	var r2 Record
	require.NoError(t, rlp.DecodeBytes(blob, &r2))
	assert.Equal(t, r, r2)

	blob2, err := rlp.EncodeToBytes(r2)
	require.NoError(t, err)
	assert.Equal(t, blob, blob2)
}

func TestNodeAddr(t *testing.T) {
	var r Record
	if addr := r.NodeAddr(); addr != nil {
		t.Errorf("wrong address on empty record: got %v, want %v", addr, nil)
	}

	require.NoError(t, r.Sign(privkey))
	expected := "caaa1485d83b18b32ed9ad666026151bf0cae8a0a88c857ae2d4c5be2daa6726"
	assert.Equal(t, expected, hex.EncodeToString(r.NodeAddr()))
}

var pyRecord, _ = hex.DecodeString("f896b840954dc36583c1f4b69ab59b1375f362f06ee99f3723cd77e64b6de6d211c27d7870642a79d4516997f94091325d2a7ca6215376971455fb221d34f35b277149a1018664697363763582765f82696490736563703235366b312d6b656363616b83697034847f00000189736563703235366b31a103ca634cae0d49acb401d8a4c6b6fe8c55b70d115bf400769cc1400f3258cd3138")

func TestPythonInterop(t *testing.T) {
	var r Record
	if err := rlp.DecodeBytes(pyRecord, &r); err != nil {
		t.Fatalf("can't decode: %v", err)
	}

	var (
		wantAddr, _  = hex.DecodeString("caaa1485d83b18b32ed9ad666026151bf0cae8a0a88c857ae2d4c5be2daa6726")
		wantSeq      = uint64(1)
		wantIP       = IP4{127, 0, 0, 1}
		wantDiscport = DiscPort(30303)
	)
	if r.Seq() != wantSeq {
		t.Errorf("wrong seq: got %d, want %d", r.Seq(), wantSeq)
	}
	if addr := r.NodeAddr(); !bytes.Equal(addr, wantAddr) {
		t.Errorf("wrong addr: got %x, want %x", addr, wantAddr)
	}
	want := map[Entry]interface{}{new(IP4): &wantIP, new(DiscPort): &wantDiscport}
	for k, v := range want {
		desc := fmt.Sprintf("loading key %q", k.ENRKey())
		if assert.NoError(t, r.Load(k), desc) {
			assert.Equal(t, k, v, desc)
		}
	}
}

func TestRecordTooBig(t *testing.T) {
	var r Record
	key := randomString(10)

	r.Set(WithEntry(key, randomString(300)))
	if err := r.Sign(privkey); err != errTooBig {
		t.Fatalf("expected to get errTooBig, got %#v", err)
	}

	r.Set(WithEntry(key, randomString(100)))
	require.NoError(t, r.Sign(privkey))
}

func TestSignEncodeAndDecodeRandom(t *testing.T) {
	var r Record

	pairs := map[string]uint32{}
	for i := 0; i < 10; i++ {
		key := randomString(7)
		value := rnd.Uint32()
		pairs[key] = value
		r.Set(WithEntry(key, &value))
	}

	require.NoError(t, r.Sign(privkey))
	_, err := rlp.EncodeToBytes(r)
	require.NoError(t, err)

	for k, v := range pairs {
		desc := fmt.Sprintf("key %q", k)
		var got uint32
		buf := WithEntry(k, &got)
		require.NoError(t, r.Load(buf), desc)
		require.Equal(t, v, got, desc)
	}
}

func BenchmarkDecode(b *testing.B) {
	var r Record
	for i := 0; i < b.N; i++ {
		rlp.DecodeBytes(pyRecord, &r)
	}
	b.StopTimer()
	r.NodeAddr()
}
