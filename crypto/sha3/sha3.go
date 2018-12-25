package sha3

type spongeDirection int

const (

	spongeAbsorbing spongeDirection = iota

	spongeSqueezing
)

const (

	maxRate = 168
)

type state struct {

	a    [25]uint64
	buf  []byte
	rate int

	dsbyte  byte
	storage [maxRate]byte

	outputLen int
	state     spongeDirection
}

func (d *state) BlockSize() int { return d.rate }

func (d *state) Size() int { return d.outputLen }

func (d *state) Reset() {

	for i := range d.a {
		d.a[i] = 0
	}
	d.state = spongeAbsorbing
	d.buf = d.storage[:0]
}

func (d *state) clone() *state {
	ret := *d
	if ret.state == spongeAbsorbing {
		ret.buf = ret.storage[:len(ret.buf)]
	} else {
		ret.buf = ret.storage[d.rate-cap(d.buf) : d.rate]
	}

	return &ret
}

func (d *state) permute() {
	switch d.state {
	case spongeAbsorbing:

		xorIn(d, d.buf)
		d.buf = d.storage[:0]
		keccakF1600(&d.a)
	case spongeSqueezing:

		keccakF1600(&d.a)
		d.buf = d.storage[:d.rate]
		copyOut(d, d.buf)
	}
}

func (d *state) padAndPermute(dsbyte byte) {
	if d.buf == nil {
		d.buf = d.storage[:0]
	}

	d.buf = append(d.buf, dsbyte)
	zerosStart := len(d.buf)
	d.buf = d.storage[:d.rate]
	for i := zerosStart; i < d.rate; i++ {
		d.buf[i] = 0
	}

	d.buf[d.rate-1] ^= 0x80

	d.permute()
	d.state = spongeSqueezing
	d.buf = d.storage[:d.rate]
	copyOut(d, d.buf)
}

func (d *state) Write(p []byte) (written int, err error) {
	if d.state != spongeAbsorbing {
		panic("sha3: write to sponge after read")
	}
	if d.buf == nil {
		d.buf = d.storage[:0]
	}
	written = len(p)

	for len(p) > 0 {
		if len(d.buf) == 0 && len(p) >= d.rate {

			xorIn(d, p[:d.rate])
			p = p[d.rate:]
			keccakF1600(&d.a)
		} else {

			todo := d.rate - len(d.buf)
			if todo > len(p) {
				todo = len(p)
			}
			d.buf = append(d.buf, p[:todo]...)
			p = p[todo:]

			if len(d.buf) == d.rate {
				d.permute()
			}
		}
	}

	return
}

func (d *state) Read(out []byte) (n int, err error) {

	if d.state == spongeAbsorbing {
		d.padAndPermute(d.dsbyte)
	}

	n = len(out)

	for len(out) > 0 {
		n := copy(out, d.buf)
		d.buf = d.buf[n:]
		out = out[n:]

		if len(d.buf) == 0 {
			d.permute()
		}
	}

	return
}

func (d *state) Sum(in []byte) []byte {

	dup := d.clone()
	hash := make([]byte, dup.outputLen)
	dup.Read(hash)
	return append(in, hash...)
}
