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

package types

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"

	"github.com/Aurorachain/go-aoa/common"
	"github.com/Aurorachain/go-aoa/crypto"
	"github.com/Aurorachain/go-aoa/log"
	"github.com/Aurorachain/go-aoa/params"
)

var (
	ErrInvalidChainId = errors.New("invalid chain id for signer")
	big8              = big.NewInt(8)
)

// sigCache is used to cache the derived sender and contains
// the signer used to derive it.
type sigCache struct {
	signer Signer
	from   common.Address
}

// MakeSigner returns a Signer based on the given chain config and block number.
func MakeSigner(config *params.ChainConfig, blockNumber *big.Int) Signer {
	var signer Signer
	signer = NewAuroraSigner(config.ChainId)
	return signer
}

// SignTx signs the transaction using the given signer and private key
func SignTx(tx *Transaction, s Signer, prv *ecdsa.PrivateKey) (*Transaction, error) {
	h := s.Hash(tx)
	sig, err := crypto.Sign(h[:], prv)
	if err != nil {
		return nil, err
	}
	return tx.WithSignature(s, sig)
}

// Sender returns the address derived from the signature (V, R, S) using secp256k1
// elliptic curve and an error if it failed deriving or upon an incorrect
// signature.
//
// Sender may cache the address, allowing it to be used regardless of
// signing method. The cache is invalidated if the cached signer does
// not match the signer used in the current call.
func Sender(signer Signer, tx *Transaction) (common.Address, error) {
	if sc := tx.from.Load(); sc != nil {
		sigCache := sc.(sigCache)
		// If the signer used to derive from in a previous
		// call is not the same as used current, invalidate
		// the cache.
		if sigCache.signer.Equal(signer) {
			return sigCache.from, nil
		}
	}

	addr, err := signer.Sender(tx)
	if err != nil {
		return common.Address{}, err
	}
	tx.from.Store(sigCache{signer: signer, from: addr})
	return addr, nil
}

// Signer encapsulates transaction signature handling. Note that this interface is not a
// stable API and may change at any time to accommodate new protocol rules.
type Signer interface {
	// Sender returns the sender address of the transaction.
	Sender(tx *Transaction) (common.Address, error)
	// SignatureValues returns the raw R, S, V values corresponding to the
	// given signature.
	SignatureValues(tx *Transaction, sig []byte) (r, s, v *big.Int, err error)
	// Hash returns the hash to be signed.
	Hash(tx *Transaction) common.Hash
	// Equal returns true if the given signer is the same as the receiver.
	Equal(Signer) bool
}

type AuroraSigner struct {
	chainId, chainIdMul *big.Int
}

func NewAuroraSigner(chainId *big.Int) AuroraSigner {
	if chainId == nil {
		panic(errors.New("new Aurora Signer can not without chainId"))
	}
	return AuroraSigner{
		chainId:    chainId,
		chainIdMul: new(big.Int).Mul(chainId, big.NewInt(2)),
	}
}

func (s AuroraSigner) Equal(s2 Signer) bool {
	auroraSigner, ok := s2.(AuroraSigner)
	return ok && auroraSigner.chainId.Cmp(s.chainId) == 0
}

func (s AuroraSigner) Sender(tx *Transaction) (common.Address, error) {
	log.Debugf("AuroraSigner|Sender, tx.ChainId=%v, s.chainId=%v", tx.ChainId(), s.chainId)
	if tx.ChainId().Cmp(s.chainId) != 0 {
		return common.Address{}, ErrInvalidChainId
	}
	// V = 27 + chainId * 2 + 8
	V := new(big.Int).Sub(tx.data.V, s.chainIdMul)
	V.Sub(V, big8)
	log.Debugf("AuroraSigner|Sender, V=%v", V.Int64())
	return recoverPlain(s.Hash(tx), tx.data.R, tx.data.S, V, true)
}

// Hash returns the hash to be signed by the sender.
// It does not uniquely identify the transaction.
func (s AuroraSigner) Hash(tx *Transaction) common.Hash {
	return rlpHash([]interface{}{
		tx.data.AccountNonce,
		tx.data.Price,
		tx.data.GasLimit,
		tx.data.Recipient,
		tx.data.Amount,
		tx.data.Payload,
		tx.data.Action,
		tx.data.Vote,
		tx.data.Nickname,
		tx.data.Asset,
		tx.data.AssetInfo,
		tx.data.SubAddress,
		tx.data.Abi,
		s.chainId, uint(0), uint(0),
	})
}

// WithSignature returns a new transaction with the given signature. This signature
// needs to be in the [R || S || V] format where V is 0 or 1.
func (s AuroraSigner) SignatureValues(tx *Transaction, sig []byte) (R, S, V *big.Int, err error) {
	R, S, V, err = signatureValues(tx, sig)
	if err != nil {
		return nil, nil, nil, err
	}
	log.Infof("AuroraSigner|SignatureValues, chainId=%v, V=%v", s.chainId.Int64(), V.Int64())
	if s.chainId.Sign() != 0 {
		V = big.NewInt(int64(sig[64] + 27 + 8))
		V.Add(V, s.chainIdMul)
	}
	return R, S, V, nil
}

// SignatureValues returns signature values. This signature
// needs to be in the [R || S || V] format where V is 0 or 1.
func signatureValues(tx *Transaction, sig []byte) (r, s, v *big.Int, err error) {
	if len(sig) != 65 {
		panic(fmt.Sprintf("wrong size for signature: got %d, want 65", len(sig)))
	}
	r = new(big.Int).SetBytes(sig[:32])
	s = new(big.Int).SetBytes(sig[32:64])
	v = new(big.Int).SetBytes([]byte{sig[64] + 27})
	return r, s, v, nil
}

func recoverPlain(sighash common.Hash, R, S, Vb *big.Int, homestead bool) (common.Address, error) {
	if Vb.BitLen() > 8 {
		log.Errorf("recoverPlain1| err, vb=%v", Vb.BitLen())
		return common.Address{}, ErrInvalidSig
	}
	V := byte(Vb.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, R, S, homestead) {
		log.Error("recoverPlain2| err", "V", Vb.Uint64()-27)
		return common.Address{}, ErrInvalidSig
	}
	// encode the snature in uncompressed format
	r, s := R.Bytes(), S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V
	// recover the public key from the snature
	pub, err := crypto.Ecrecover(sighash[:], sig)
	if err != nil {
		return common.Address{}, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return common.Address{}, errors.New("invalid public key")
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pub[1:])[12:])
	return addr, nil
}

// deriveChainId derives the chain id from the given v parameter
func deriveChainId(v *big.Int) *big.Int {
	if v.BitLen() <= 64 {
		v := v.Uint64()
		//if v == 27 || v == 28 {
		//	return new(big.Int)
		//}
		return new(big.Int).SetUint64((v - 35) / 2)
	}
	v = new(big.Int).Sub(v, big.NewInt(35))
	return v.Div(v, big.NewInt(2))
}
