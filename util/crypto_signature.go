package util

import (
	"crypto/ecdsa"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/crypto/secp256k1"
	"github.com/Aurorachain/go-Aurora/crypto/sha3"
	"strings"
)

func Sign(data []byte, privateKey *ecdsa.PrivateKey) (sign []byte, err error) {
	dataBytes := sha3.Sum256(data)
	return crypto.Sign(dataBytes[:], privateKey)
}

func VerifySignature(address string, data, sign []byte) (bool, error) {
	dataBytes := sha3.Sum256(data)

	pubkey, err := secp256k1.RecoverPubkey(dataBytes[:], sign)

	if err != nil {
		return false, err
	}

	return strings.EqualFold(crypto.PubkeyToAddress(*crypto.ToECDSAPub(pubkey)).Hex(), address), nil

}
