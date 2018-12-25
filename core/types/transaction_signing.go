package types

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"

	"github.com/Aurorachain/go-Aurora/common"
	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/Aurorachain/go-Aurora/log"
)

var (
	ErrInvalidChainId = errors.New("invalid chain id for signer")
	big8 = big.NewInt(8)
)

type sigCache struct {
	signer Signer
	from   common.Address
}

func MakeSigner(config *params.ChainConfig, blockNumber *big.Int) Signer {
	var signer Signer
	signer = NewAuroraSigner(config.ChainId)
	return signer
}

func SignTx(tx *Transaction, s Signer, prv *ecdsa.PrivateKey) (*Transaction, error) {
	h := s.Hash(tx)
	sig, err := crypto.Sign(h[:], prv)
	if err != nil {
		return nil, err
	}
	return tx.WithSignature(s, sig)
}

func Sender(signer Signer, tx *Transaction) (common.Address, error) {
	if sc := tx.from.Load(); sc != nil {
		sigCache := sc.(sigCache)

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

type Signer interface {

	Sender(tx *Transaction) (common.Address, error)

	SignatureValues(tx *Transaction, sig []byte) (r, s, v *big.Int, err error)

	Hash(tx *Transaction) common.Hash

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

func (s AuroraSigner) Equal(s2 Signer) bool  {
	auroraSigner,ok := s2.(AuroraSigner)
	return ok && auroraSigner.chainId.Cmp(s.chainId) == 0
}

func (s AuroraSigner) Sender(tx *Transaction) (common.Address, error) {
	log.Debug("AuroraSigner|Sender","tx.ChainId",tx.ChainId(),"s.chainId",s.chainId)
	if tx.ChainId().Cmp(s.chainId) != 0 {
		return common.Address{}, ErrInvalidChainId
	}

	V := new(big.Int).Sub(tx.data.V, s.chainIdMul)
	V.Sub(V, big8)
	log.Debug("AuroraSigner|Sender","V",V.Int64())
	return recoverPlain(s.Hash(tx), tx.data.R, tx.data.S, V, true)
}

func (s AuroraSigner) Hash(tx *Transaction) common.Hash  {
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

func (s AuroraSigner) SignatureValues(tx *Transaction, sig []byte) (R, S, V *big.Int, err error) {
	R, S, V, err = signatureValues(tx, sig)
	if err != nil {
		return nil, nil, nil, err
	}
	log.Debug("AuroraSigner|SignatureValues","chainId",s.chainId.Int64(),"V",V.Int64())
	if s.chainId.Sign() != 0 {
		V = big.NewInt(int64(sig[64] + 27 + 8))
		V.Add(V, s.chainIdMul)
	}
	return R, S, V, nil
}

func signatureValues(tx *Transaction, sig []byte) (r, s, v *big.Int, err error)  {
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
		log.Error("recoverPlain1| err","vb",Vb.BitLen())
		return common.Address{}, ErrInvalidSig
	}
	V := byte(Vb.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, R, S, homestead) {
		log.Error("recoverPlain2| err","V",Vb.Uint64() - 27)
		return common.Address{}, ErrInvalidSig
	}

	r, s := R.Bytes(), S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V

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

func deriveChainId(v *big.Int) *big.Int {
	if v.BitLen() <= 64 {
		v := v.Uint64()

		return new(big.Int).SetUint64((v - 35) / 2)
	}
	v = new(big.Int).Sub(v, big.NewInt(35))
	return v.Div(v, big.NewInt(2))
}
