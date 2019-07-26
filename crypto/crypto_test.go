// Copyright 2018 The go-aurora Authors
// This file is part of the go-aurora library.
//
// The go-aurora library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-aurora library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-aurora library. If not, see <http://www.gnu.org/licenses/>.

package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/crypto/secp256k1"
	"golang.org/x/crypto/sha3"
	"io/ioutil"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

var testAddrHex = "970e8128ab834e8eac17ab8e3812f010678cf791"
var testPrivHex = "289c2857d4598e37fb9647507e47a309d6133539bf21a8b9cb6df88fd5232032"

// These tests are sanity checks.
// They should ensure that we don't e.g. use Sha3-224 instead of Sha3-256
// and that the sha3 library uses keccak-f permutation.
func TestKeccak256Hash(t *testing.T) {
	msg := []byte("abc")
	exp, _ := hex.DecodeString("4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45")
	checkhash(t, "Sha3-256-array", func(in []byte) []byte { h := Keccak256Hash(in); return h[:] }, msg, exp)
}

func TestToECDSAErrors(t *testing.T) {
	if _, err := HexToECDSA("0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("HexToECDSA should've returned error")
	}
	if _, err := HexToECDSA("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"); err == nil {
		t.Fatal("HexToECDSA should've returned error")
	}
}

func BenchmarkSha3(b *testing.B) {
	a := []byte("hello world")
	for i := 0; i < b.N; i++ {
		Keccak256(a)
	}
}

func TestPrivateKey(t *testing.T) {
	key, _ := HexToECDSA("47c130855c576651c871ecd6939dbf3bd277264b329e720d6f13e46a7f9b703b")
	genAddr := PubkeyToAddress(key.PublicKey)
	fmt.Println(genAddr.Hex())
}

func TestSign(t *testing.T) {
	key, _ := HexToECDSA(testPrivHex)
	addr := common.HexToAddress(testAddrHex)

	msg := Keccak256([]byte("foo"))
	sig, err := Sign(msg, key)
	if err != nil {
		t.Errorf("Sign error: %s", err)
	}
	recoveredPub, err := Ecrecover(msg, sig)
	if err != nil {
		t.Errorf("ECRecover error: %s", err)
	}
	pubKey := ToECDSAPub(recoveredPub)

	recoveredAddr := PubkeyToAddress(*pubKey)
	if addr != recoveredAddr {
		t.Errorf("Address mismatch: want: %x have: %x", addr, recoveredAddr)
	}

	// should be equal to SigToPub
	recoveredPub2, err := SigToPub(msg, sig)
	if err != nil {
		t.Errorf("ECRecover error: %s", err)
	}
	recoveredAddr2 := PubkeyToAddress(*recoveredPub2)
	if addr != recoveredAddr2 {
		t.Errorf("Address mismatch: want: %x have: %x", addr, recoveredAddr2)
	}
}

func TestInvalidSign(t *testing.T) {
	if _, err := Sign(make([]byte, 1), nil); err == nil {
		t.Errorf("expected sign with hash 1 byte to error")
	}
	if _, err := Sign(make([]byte, 33), nil); err == nil {
		t.Errorf("expected sign with hash 33 byte to error")
	}
}

func TestNewContractAddress(t *testing.T) {
	key, _ := HexToECDSA(testPrivHex)
	addr := common.HexToAddress(testAddrHex)
	genAddr := PubkeyToAddress(key.PublicKey)
	// sanity check before using addr to create contract address
	checkAddr(t, genAddr, addr)

	caddr0 := CreateAddress(addr, 0)
	caddr1 := CreateAddress(addr, 1)
	caddr2 := CreateAddress(addr, 2)
	checkAddr(t, common.HexToAddress("333c3310824b7c685133f2bedb2ca4b8b4df633d"), caddr0)
	checkAddr(t, common.HexToAddress("8bda78331c916a08481428e4b07c96d3e916d165"), caddr1)
	checkAddr(t, common.HexToAddress("c9ddedf451bc62ce88bf9292afb13df35b670699"), caddr2)
}

func TestLoadECDSAFile(t *testing.T) {
	keyBytes := common.FromHex(testPrivHex)
	fileName0 := "test_key0"
	fileName1 := "test_key1"
	checkKey := func(k *ecdsa.PrivateKey) {
		checkAddr(t, PubkeyToAddress(k.PublicKey), common.HexToAddress(testAddrHex))
		loadedKeyBytes := FromECDSA(k)
		if !bytes.Equal(loadedKeyBytes, keyBytes) {
			t.Fatalf("private key mismatch: want: %x have: %x", keyBytes, loadedKeyBytes)
		}
	}

	ioutil.WriteFile(fileName0, []byte(testPrivHex), 0600)
	defer os.Remove(fileName0)

	key0, err := LoadECDSA(fileName0)
	if err != nil {
		t.Fatal(err)
	}
	checkKey(key0)

	// again, this time with SaveECDSA instead of manual save:
	err = SaveECDSA(fileName1, key0)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(fileName1)

	key1, err := LoadECDSA(fileName1)
	if err != nil {
		t.Fatal(err)
	}
	checkKey(key1)
}

func TestValidateSignatureValues(t *testing.T) {
	check := func(expected bool, v byte, r, s *big.Int) {
		if ValidateSignatureValues(v, r, s, false) != expected {
			t.Errorf("mismatch for v: %d r: %d s: %d want: %v", v, r, s, expected)
		}
	}
	minusOne := big.NewInt(-1)
	one := common.Big1
	zero := common.Big0
	secp256k1nMinus1 := new(big.Int).Sub(secp256k1_N, common.Big1)

	// correct v,r,s
	check(true, 0, one, one)
	check(true, 1, one, one)
	// incorrect v, correct r,s,
	check(false, 2, one, one)
	check(false, 3, one, one)

	// incorrect v, combinations of incorrect/correct r,s at lower limit
	check(false, 2, zero, zero)
	check(false, 2, zero, one)
	check(false, 2, one, zero)
	check(false, 2, one, one)

	// correct v for any combination of incorrect r,s
	check(false, 0, zero, zero)
	check(false, 0, zero, one)
	check(false, 0, one, zero)

	check(false, 1, zero, zero)
	check(false, 1, zero, one)
	check(false, 1, one, zero)

	// correct sig with max r,s
	check(true, 0, secp256k1nMinus1, secp256k1nMinus1)
	// correct v, combinations of incorrect r,s at upper limit
	check(false, 0, secp256k1_N, secp256k1nMinus1)
	check(false, 0, secp256k1nMinus1, secp256k1_N)
	check(false, 0, secp256k1_N, secp256k1_N)

	// current callers ensures r,s cannot be negative, but let's test for that too
	// as crypto package could be used stand-alone
	check(false, 0, minusOne, one)
	check(false, 0, one, minusOne)
}

func checkhash(t *testing.T, name string, f func([]byte) []byte, msg, exp []byte) {
	sum := f(msg)
	if !bytes.Equal(exp, sum) {
		t.Fatalf("hash %s mismatch: want: %x have: %x", name, exp, sum)
	}
}

func checkAddr(t *testing.T, addr0, addr1 common.Address) {
	if addr0 != addr1 {
		t.Fatalf("address mismatch: want: %x have: %x", addr0, addr1)
	}
}

// test to help Python team with integration of libsecp256k1
// skip but keep it after they are done
func TestPythonIntegration(t *testing.T) {
	kh := "289c2857d4598e37fb9647507e47a309d6133539bf21a8b9cb6df88fd5232032"
	k0, _ := HexToECDSA(kh)

	msg0 := Keccak256([]byte("foo"))
	sig0, _ := Sign(msg0, k0)

	msg1 := common.FromHex("00000000000000000000000000000000")
	sig1, _ := Sign(msg0, k0)

	t.Logf("msg: %x, privkey: %s sig: %x\n", msg0, kh, sig0)
	t.Logf("msg: %x, privkey: %s sig: %x\n", msg1, kh, sig1)
}

func TestFromECDSAPub(t *testing.T) {
	//privateKeyECDSA, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	//if err != nil {
	//	fmt.Println("error")
	//	return
	//}
	//publicKey := privateKeyECDSA.Public()
	//fmt.Println(publicKey)
	//res := FromECDSAPub(publicKey.(*ecdsa.PublicKey))
	//printRes := base64.StdEncoding.EncodeToString(res)
	//fmt.Println(printRes)
	//decodeRes, _ := base64.StdEncoding.DecodeString(printRes)
	//pub := ToECDSAPub(decodeRes)
	//fmt.Println(pub)
	startTime := time.Now().Nanosecond()
	msg := "aafadsfladfsafadsfadsfasdfadsfadsfadsfdsafasdffasdfdsa"
	privateKeyECDSA, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	if err != nil {
		fmt.Println("error")
		return
	}
	key := privateKeyECDSA.Public().(*ecdsa.PublicKey)
	privateHex := hex.EncodeToString(FromECDSA(privateKeyECDSA))
	fmt.Printf("privateHex:%s\n", privateHex)
	fmt.Println(PubkeyToAddress(*key).Hex())
	common.FromHex(msg)
	var b [32]byte
	b = sha3.Sum256(common.FromHex(msg))
	sign, err := Sign(b[:], privateKeyECDSA)
	//fmt.Println(err)
	//fmt.Println(sign)
	//decode := hexutil.MustDecode(string(sign))
	pubkey, _ := secp256k1.RecoverPubkey(b[:], sign)
	endTime := time.Now().Nanosecond()
	fmt.Println(PubkeyToAddress(*ToECDSAPub(pubkey)).Hex())
	fmt.Printf("cost time:%d", endTime-startTime)
	//publicKey, _ := SigToPub(b[:], sign)
	//pubString := hexutil.MustDecode(hexutil.Encode(FromECDSAPub(&privateKeyECDSA.PublicKey)[:]))
	//signature := VerifySignature(FromECDSAPub(&privateKeyECDSA.PublicKey),b[:], sign)

	//signature := VerifySignature(FromECDSAPub(publicKey),b[:], sign)
	//fmt.Println(signature)
	//publicKey := privateKeyECDSA.Public()
	//fmt.Println(publicKey)
	//res := FromECDSAPub(publicKey.(*ecdsa.PublicKey))
	//printRes := base64.StdEncoding.EncodeToString(res)
	//fmt.Println(printRes)
	//decodeRes, _ := base64.StdEncoding.DecodeString(printRes)
	//pub := ToECDSAPub(decodeRes)
	//fmt.Println(pub)
}

func TestCompressPubkey2(t *testing.T) {
	blockHex := "0x3235343539393333396237383933303738643339646663346333383361653062"
	privateKeyECDSA, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	blockByte := common.Hex2Bytes(blockHex)
	sign, err := Sign(blockByte[:32], privateKeyECDSA)
	if err != nil {
		t.Fatal(err)
	}
	key := privateKeyECDSA.Public().(*ecdsa.PublicKey)
	address := PubkeyToAddress(*key).Hex()
	pubkey, err := secp256k1.RecoverPubkey(blockByte[:32], sign)

	if err != nil {
		t.Fatal(err)
	}
	recoverAddress := PubkeyToAddress(*ToECDSAPub(pubkey)).Hex()
	fmt.Printf("address:%s recoverAddress:%s\n", address, recoverAddress)

	for i := 1; i < 10000; i++ {
		sign, err := Sign(blockByte[:32], privateKeyECDSA)
		if err != nil {
			t.Fatal(err)
		}
		pubkey, err := secp256k1.RecoverPubkey(blockByte[:32], sign)

		if err != nil {
			t.Fatal(err)
		}
		recoverAddress := PubkeyToAddress(*ToECDSAPub(pubkey)).Hex()
		// fmt.Printf("address:%s recoverAddress:%s\n",address,recoverAddress)
		if !strings.EqualFold(address, recoverAddress) {
			break
			fmt.Printf("address:%s recoverAddress:%s\n", address, recoverAddress)
		}
	}
}

func TestGenerateKey2(t *testing.T) {
	privateKeyECDSA, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := privateKeyECDSA.Public().(*ecdsa.PublicKey)
	privateKeyBytes := FromECDSA(privateKeyECDSA)
	privateKeyHex := hex.EncodeToString(privateKeyBytes)
	fmt.Printf("privateKeyHex:%s length:%d\n", privateKeyHex, len(privateKeyHex))
	publicKeyBytes := FromECDSAPub(publicKey)
	publicKeyHex := hex.EncodeToString(publicKeyBytes)
	fmt.Printf("publicKey:%s length:%d\n", publicKeyHex, len(publicKeyHex))
	address := PubkeyToAddress(*publicKey).Hex()
	fmt.Printf("address:%s n", address)
}

func TestCheckPrivatekey(t *testing.T) {
	privateKey, err := HexToECDSA("0xa747ae7e44e5392e0f31bba73a34222adcdbcdd4dcd9526f9f2f25ad48db4c7")
	if err != nil {
		t.Fatal(err)
	}
	publicKey := privateKey.Public().(*ecdsa.PublicKey)
	address := PubkeyToAddress(*publicKey).Hex()
	fmt.Printf("address:%s n", address)
}
