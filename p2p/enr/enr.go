package enr

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/Aurorachain/go-Aurora/crypto"
	"github.com/Aurorachain/go-Aurora/crypto/sha3"
	"github.com/Aurorachain/go-Aurora/rlp"
)

const SizeLimit = 300

const ID_SECP256k1_KECCAK = ID("secp256k1-keccak")

var (
	errNoID           = errors.New("unknown or unspecified identity scheme")
	errInvalidSigsize = errors.New("invalid signature size")
	errInvalidSig     = errors.New("invalid signature")
	errNotSorted      = errors.New("record key/value pairs are not sorted by key")
	errDuplicateKey   = errors.New("record contains duplicate key")
	errIncompletePair = errors.New("record contains incomplete k/v pair")
	errTooBig         = fmt.Errorf("record bigger than %d bytes", SizeLimit)
	errEncodeUnsigned = errors.New("can't encode unsigned record")
	errNotFound       = errors.New("no such key in record")
)

type Record struct {
	seq       uint64
	signature []byte
	raw       []byte
	pairs     []pair
}

type pair struct {
	k string
	v rlp.RawValue
}

func (r *Record) Signed() bool {
	return r.signature != nil
}

func (r *Record) Seq() uint64 {
	return r.seq
}

func (r *Record) SetSeq(s uint64) {
	r.signature = nil
	r.raw = nil
	r.seq = s
}

func (r *Record) Load(e Entry) error {
	i := sort.Search(len(r.pairs), func(i int) bool { return r.pairs[i].k >= e.ENRKey() })
	if i < len(r.pairs) && r.pairs[i].k == e.ENRKey() {
		if err := rlp.DecodeBytes(r.pairs[i].v, e); err != nil {
			return &KeyError{Key: e.ENRKey(), Err: err}
		}
		return nil
	}
	return &KeyError{Key: e.ENRKey(), Err: errNotFound}
}

func (r *Record) Set(e Entry) {
	r.signature = nil
	r.raw = nil
	blob, err := rlp.EncodeToBytes(e)
	if err != nil {
		panic(fmt.Errorf("enr: can't encode %s: %v", e.ENRKey(), err))
	}

	i := sort.Search(len(r.pairs), func(i int) bool { return r.pairs[i].k >= e.ENRKey() })

	if i < len(r.pairs) && r.pairs[i].k == e.ENRKey() {

		r.pairs[i].v = blob
		return
	} else if i < len(r.pairs) {

		el := pair{e.ENRKey(), blob}
		r.pairs = append(r.pairs, pair{})
		copy(r.pairs[i+1:], r.pairs[i:])
		r.pairs[i] = el
		return
	}

	r.pairs = append(r.pairs, pair{e.ENRKey(), blob})
}

func (r Record) EncodeRLP(w io.Writer) error {
	if !r.Signed() {
		return errEncodeUnsigned
	}
	_, err := w.Write(r.raw)
	return err
}

func (r *Record) DecodeRLP(s *rlp.Stream) error {
	raw, err := s.Raw()
	if err != nil {
		return err
	}
	if len(raw) > SizeLimit {
		return errTooBig
	}

	dec := Record{raw: raw}
	s = rlp.NewStream(bytes.NewReader(raw), 0)
	if _, err := s.List(); err != nil {
		return err
	}
	if err = s.Decode(&dec.signature); err != nil {
		return err
	}
	if err = s.Decode(&dec.seq); err != nil {
		return err
	}

	var prevkey string
	for i := 0; ; i++ {
		var kv pair
		if err := s.Decode(&kv.k); err != nil {
			if err == rlp.EOL {
				break
			}
			return err
		}
		if err := s.Decode(&kv.v); err != nil {
			if err == rlp.EOL {
				return errIncompletePair
			}
			return err
		}
		if i > 0 {
			if kv.k == prevkey {
				return errDuplicateKey
			}
			if kv.k < prevkey {
				return errNotSorted
			}
		}
		dec.pairs = append(dec.pairs, kv)
		prevkey = kv.k
	}
	if err := s.ListEnd(); err != nil {
		return err
	}

	if err = dec.verifySignature(); err != nil {
		return err
	}
	*r = dec
	return nil
}

type s256raw []byte

func (s256raw) ENRKey() string { return "secp256k1" }

func (r *Record) NodeAddr() []byte {
	var entry s256raw
	if r.Load(&entry) != nil {
		return nil
	}
	return crypto.Keccak256(entry)
}

func (r *Record) Sign(privkey *ecdsa.PrivateKey) error {
	r.seq = r.seq + 1
	r.Set(ID_SECP256k1_KECCAK)
	r.Set(Secp256k1(privkey.PublicKey))
	return r.signAndEncode(privkey)
}

func (r *Record) appendPairs(list []interface{}) []interface{} {
	list = append(list, r.seq)
	for _, p := range r.pairs {
		list = append(list, p.k, p.v)
	}
	return list
}

func (r *Record) signAndEncode(privkey *ecdsa.PrivateKey) error {

	list := make([]interface{}, 1, len(r.pairs)*2+2)
	list = r.appendPairs(list)

	h := sha3.NewKeccak256()
	rlp.Encode(h, list[1:])
	sig, err := crypto.Sign(h.Sum(nil), privkey)
	if err != nil {
		return err
	}
	sig = sig[:len(sig)-1]

	r.signature, list[0] = sig, sig
	r.raw, err = rlp.EncodeToBytes(list)
	if err != nil {
		return err
	}
	if len(r.raw) > SizeLimit {
		return errTooBig
	}
	return nil
}

func (r *Record) verifySignature() error {

	var id ID
	var entry s256raw
	if err := r.Load(&id); err != nil {
		return err
	} else if id != ID_SECP256k1_KECCAK {
		return errNoID
	}
	if err := r.Load(&entry); err != nil {
		return err
	} else if len(entry) != 33 {
		return fmt.Errorf("invalid public key")
	}

	list := make([]interface{}, 0, len(r.pairs)*2+1)
	list = r.appendPairs(list)
	h := sha3.NewKeccak256()
	rlp.Encode(h, list)
	if !crypto.VerifySignature(entry, h.Sum(nil), r.signature) {
		return errInvalidSig
	}
	return nil
}
