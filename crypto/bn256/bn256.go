package bn256

import (
	"crypto/rand"
	"io"
	"math/big"
)

type G1 struct {
	p *curvePoint
}

func RandomG1(r io.Reader) (*big.Int, *G1, error) {
	var k *big.Int
	var err error

	for {
		k, err = rand.Int(r, Order)
		if err != nil {
			return nil, nil, err
		}
		if k.Sign() > 0 {
			break
		}
	}

	return k, new(G1).ScalarBaseMult(k), nil
}

func (g *G1) String() string {
	return "bn256.G1" + g.p.String()
}

func (e *G1) CurvePoints() (*big.Int, *big.Int, *big.Int, *big.Int) {
	return e.p.x, e.p.y, e.p.z, e.p.t
}

func (e *G1) ScalarBaseMult(k *big.Int) *G1 {
	if e.p == nil {
		e.p = newCurvePoint(nil)
	}
	e.p.Mul(curveGen, k, new(bnPool))
	return e
}

func (e *G1) ScalarMult(a *G1, k *big.Int) *G1 {
	if e.p == nil {
		e.p = newCurvePoint(nil)
	}
	e.p.Mul(a.p, k, new(bnPool))
	return e
}

func (e *G1) Add(a, b *G1) *G1 {
	if e.p == nil {
		e.p = newCurvePoint(nil)
	}
	e.p.Add(a.p, b.p, new(bnPool))
	return e
}

func (e *G1) Neg(a *G1) *G1 {
	if e.p == nil {
		e.p = newCurvePoint(nil)
	}
	e.p.Negative(a.p)
	return e
}

func (n *G1) Marshal() []byte {
	n.p.MakeAffine(nil)

	xBytes := new(big.Int).Mod(n.p.x, P).Bytes()
	yBytes := new(big.Int).Mod(n.p.y, P).Bytes()

	const numBytes = 256 / 8

	ret := make([]byte, numBytes*2)
	copy(ret[1*numBytes-len(xBytes):], xBytes)
	copy(ret[2*numBytes-len(yBytes):], yBytes)

	return ret
}

func (e *G1) Unmarshal(m []byte) (*G1, bool) {

	const numBytes = 256 / 8

	if len(m) != 2*numBytes {
		return nil, false
	}

	if e.p == nil {
		e.p = newCurvePoint(nil)
	}

	e.p.x.SetBytes(m[0*numBytes : 1*numBytes])
	e.p.y.SetBytes(m[1*numBytes : 2*numBytes])

	if e.p.x.Sign() == 0 && e.p.y.Sign() == 0 {

		e.p.y.SetInt64(1)
		e.p.z.SetInt64(0)
		e.p.t.SetInt64(0)
	} else {
		e.p.z.SetInt64(1)
		e.p.t.SetInt64(1)

		if !e.p.IsOnCurve() {
			return nil, false
		}
	}

	return e, true
}

type G2 struct {
	p *twistPoint
}

func RandomG2(r io.Reader) (*big.Int, *G2, error) {
	var k *big.Int
	var err error

	for {
		k, err = rand.Int(r, Order)
		if err != nil {
			return nil, nil, err
		}
		if k.Sign() > 0 {
			break
		}
	}

	return k, new(G2).ScalarBaseMult(k), nil
}

func (g *G2) String() string {
	return "bn256.G2" + g.p.String()
}

func (e *G2) CurvePoints() (*gfP2, *gfP2, *gfP2, *gfP2) {
	return e.p.x, e.p.y, e.p.z, e.p.t
}

func (e *G2) ScalarBaseMult(k *big.Int) *G2 {
	if e.p == nil {
		e.p = newTwistPoint(nil)
	}
	e.p.Mul(twistGen, k, new(bnPool))
	return e
}

func (e *G2) ScalarMult(a *G2, k *big.Int) *G2 {
	if e.p == nil {
		e.p = newTwistPoint(nil)
	}
	e.p.Mul(a.p, k, new(bnPool))
	return e
}

func (e *G2) Add(a, b *G2) *G2 {
	if e.p == nil {
		e.p = newTwistPoint(nil)
	}
	e.p.Add(a.p, b.p, new(bnPool))
	return e
}

func (n *G2) Marshal() []byte {
	n.p.MakeAffine(nil)

	xxBytes := new(big.Int).Mod(n.p.x.x, P).Bytes()
	xyBytes := new(big.Int).Mod(n.p.x.y, P).Bytes()
	yxBytes := new(big.Int).Mod(n.p.y.x, P).Bytes()
	yyBytes := new(big.Int).Mod(n.p.y.y, P).Bytes()

	const numBytes = 256 / 8

	ret := make([]byte, numBytes*4)
	copy(ret[1*numBytes-len(xxBytes):], xxBytes)
	copy(ret[2*numBytes-len(xyBytes):], xyBytes)
	copy(ret[3*numBytes-len(yxBytes):], yxBytes)
	copy(ret[4*numBytes-len(yyBytes):], yyBytes)

	return ret
}

func (e *G2) Unmarshal(m []byte) (*G2, bool) {

	const numBytes = 256 / 8

	if len(m) != 4*numBytes {
		return nil, false
	}

	if e.p == nil {
		e.p = newTwistPoint(nil)
	}

	e.p.x.x.SetBytes(m[0*numBytes : 1*numBytes])
	e.p.x.y.SetBytes(m[1*numBytes : 2*numBytes])
	e.p.y.x.SetBytes(m[2*numBytes : 3*numBytes])
	e.p.y.y.SetBytes(m[3*numBytes : 4*numBytes])

	if e.p.x.x.Sign() == 0 &&
		e.p.x.y.Sign() == 0 &&
		e.p.y.x.Sign() == 0 &&
		e.p.y.y.Sign() == 0 {

		e.p.y.SetOne()
		e.p.z.SetZero()
		e.p.t.SetZero()
	} else {
		e.p.z.SetOne()
		e.p.t.SetOne()

		if !e.p.IsOnCurve() {
			return nil, false
		}
	}

	return e, true
}

type GT struct {
	p *gfP12
}

func (g *GT) String() string {
	return "bn256.GT" + g.p.String()
}

func (e *GT) ScalarMult(a *GT, k *big.Int) *GT {
	if e.p == nil {
		e.p = newGFp12(nil)
	}
	e.p.Exp(a.p, k, new(bnPool))
	return e
}

func (e *GT) Add(a, b *GT) *GT {
	if e.p == nil {
		e.p = newGFp12(nil)
	}
	e.p.Mul(a.p, b.p, new(bnPool))
	return e
}

func (e *GT) Neg(a *GT) *GT {
	if e.p == nil {
		e.p = newGFp12(nil)
	}
	e.p.Invert(a.p, new(bnPool))
	return e
}

func (n *GT) Marshal() []byte {
	n.p.Minimal()

	xxxBytes := n.p.x.x.x.Bytes()
	xxyBytes := n.p.x.x.y.Bytes()
	xyxBytes := n.p.x.y.x.Bytes()
	xyyBytes := n.p.x.y.y.Bytes()
	xzxBytes := n.p.x.z.x.Bytes()
	xzyBytes := n.p.x.z.y.Bytes()
	yxxBytes := n.p.y.x.x.Bytes()
	yxyBytes := n.p.y.x.y.Bytes()
	yyxBytes := n.p.y.y.x.Bytes()
	yyyBytes := n.p.y.y.y.Bytes()
	yzxBytes := n.p.y.z.x.Bytes()
	yzyBytes := n.p.y.z.y.Bytes()

	const numBytes = 256 / 8

	ret := make([]byte, numBytes*12)
	copy(ret[1*numBytes-len(xxxBytes):], xxxBytes)
	copy(ret[2*numBytes-len(xxyBytes):], xxyBytes)
	copy(ret[3*numBytes-len(xyxBytes):], xyxBytes)
	copy(ret[4*numBytes-len(xyyBytes):], xyyBytes)
	copy(ret[5*numBytes-len(xzxBytes):], xzxBytes)
	copy(ret[6*numBytes-len(xzyBytes):], xzyBytes)
	copy(ret[7*numBytes-len(yxxBytes):], yxxBytes)
	copy(ret[8*numBytes-len(yxyBytes):], yxyBytes)
	copy(ret[9*numBytes-len(yyxBytes):], yyxBytes)
	copy(ret[10*numBytes-len(yyyBytes):], yyyBytes)
	copy(ret[11*numBytes-len(yzxBytes):], yzxBytes)
	copy(ret[12*numBytes-len(yzyBytes):], yzyBytes)

	return ret
}

func (e *GT) Unmarshal(m []byte) (*GT, bool) {

	const numBytes = 256 / 8

	if len(m) != 12*numBytes {
		return nil, false
	}

	if e.p == nil {
		e.p = newGFp12(nil)
	}

	e.p.x.x.x.SetBytes(m[0*numBytes : 1*numBytes])
	e.p.x.x.y.SetBytes(m[1*numBytes : 2*numBytes])
	e.p.x.y.x.SetBytes(m[2*numBytes : 3*numBytes])
	e.p.x.y.y.SetBytes(m[3*numBytes : 4*numBytes])
	e.p.x.z.x.SetBytes(m[4*numBytes : 5*numBytes])
	e.p.x.z.y.SetBytes(m[5*numBytes : 6*numBytes])
	e.p.y.x.x.SetBytes(m[6*numBytes : 7*numBytes])
	e.p.y.x.y.SetBytes(m[7*numBytes : 8*numBytes])
	e.p.y.y.x.SetBytes(m[8*numBytes : 9*numBytes])
	e.p.y.y.y.SetBytes(m[9*numBytes : 10*numBytes])
	e.p.y.z.x.SetBytes(m[10*numBytes : 11*numBytes])
	e.p.y.z.y.SetBytes(m[11*numBytes : 12*numBytes])

	return e, true
}

func Pair(g1 *G1, g2 *G2) *GT {
	return &GT{optimalAte(g2.p, g1.p, new(bnPool))}
}

func PairingCheck(a []*G1, b []*G2) bool {
	pool := new(bnPool)

	acc := newGFp12(pool)
	acc.SetOne()

	for i := 0; i < len(a); i++ {
		if a[i].p.IsInfinity() || b[i].p.IsInfinity() {
			continue
		}
		acc.Mul(acc, miller(b[i].p, a[i].p, pool), pool)
	}
	ret := finalExponentiation(acc, pool)
	acc.Put(pool)

	return ret.IsOne()
}

type bnPool struct {
	bns   []*big.Int
	count int
}

func (pool *bnPool) Get() *big.Int {
	if pool == nil {
		return new(big.Int)
	}

	pool.count++
	l := len(pool.bns)
	if l == 0 {
		return new(big.Int)
	}

	bn := pool.bns[l-1]
	pool.bns = pool.bns[:l-1]
	return bn
}

func (pool *bnPool) Put(bn *big.Int) {
	if pool == nil {
		return
	}
	pool.bns = append(pool.bns, bn)
	pool.count--
}

func (pool *bnPool) Count() int {
	return pool.count
}
