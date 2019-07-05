package crypto

import (
	"encoding/hex"
	"fmt"
	"github.com/Aurorachain/go-aoa/common/hexutil"
	"github.com/Aurorachain/go-aoa/common/math"
	"math/big"
	"testing"
	"github.com/hbakhtiyor/schnorr"
)

const (
	veryLightScryptN = 2
	veryLightScryptP = 1
)

func TestSchnorrSign(t *testing.T) {
	privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("err when generate privateKey %v", err)
	}
	publicKey := privateKey.PublicKey
	publicKeyBytes := CompressPubkey(&publicKey)
	publicKeyString := hexutil.Encode(publicKeyBytes[:])

	privateKeyC := math.PaddedBigBytes(privateKey.D, privateKey.Params().BitSize/8)
	privateKeyString := hexutil.Encode(privateKeyC)

	//common.FromHex(string(privateKeyC))

	fmt.Println("privateKey",privateKeyString)
	fmt.Println("publicKey", publicKeyString)


	var message [32]byte
	var publicKeyBytes2 [33]byte
	copy(publicKeyBytes2[:],publicKeyBytes)

	msg, _ := hex.DecodeString("243F6A8885A308D313198A2E03707344A4093822299F31D0082EFA98EC4E6C89")
	fmt.Printf("msg: %v", msg)
	copy(message[:], msg)

	z := new(big.Int)
	setBytes := z.SetBytes(privateKeyC)

	signature, err := schnorr.Sign(setBytes, message)
	if err != nil {
		fmt.Printf("\nThe schnorr signing is failed: %v\n", err)
	}
	fmt.Printf("\nThe schnorr signature is: %v\n", signature)
	result, err := schnorr.Verify(publicKeyBytes2, message, signature)
	if err != nil {
		return
	}
	fmt.Printf("\nThe schnorr signature verify result is: %v\n", result)

	sign, err := Sign(msg, privateKey)
	if err != nil {
		return
	}
	fmt.Printf("\nThe aoa signature is: %x\n", sign)

	//result, err := schnorr.BatchVerify(publicKeys, testmsg, signatures)
	//
	//signature, err := schnorr.AggregateSignatures(privateKeys, testmsg)
}