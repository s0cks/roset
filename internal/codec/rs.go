package codec

import "errors"

// ErrUncorrectable is returned when Reed-Solomon decoding fails.
var ErrUncorrectable = errors.New("codec: too many errors to correct")

// RS is a systematic Reed-Solomon codec over GF(256), roots alpha^0..alpha^(nsym-1).
// Codewords are message||parity, highest power first.
type RS struct {
	nsym int
	gen  []byte
}

func NewRS(nsym int) *RS {
	gen := []byte{1}
	for i := 0; i < nsym; i++ {
		next := make([]byte, len(gen)+1)
		for j, g := range gen {
			next[j] ^= g
			next[j+1] ^= gfMul(g, expTab[i])
		}
		gen = next
	}
	return &RS{nsym: nsym, gen: gen}
}

func (r *RS) Encode(msg []byte) []byte {
	out := make([]byte, len(msg)+r.nsym)
	copy(out, msg)
	for i := range msg {
		c := out[i]
		if c == 0 {
			continue
		}
		for j := 1; j < len(r.gen); j++ {
			out[i+j] ^= gfMul(r.gen[j], c)
		}
	}
	copy(out, msg)
	return out
}

func (r *RS) syndromes(cw []byte) ([]byte, bool) {
	s := make([]byte, r.nsym)
	nz := false
	for j := 0; j < r.nsym; j++ {
		x := expTab[j]
		var y byte
		for _, c := range cw {
			y = gfMul(y, x) ^ c
		}
		s[j] = y
		if y != 0 {
			nz = true
		}
	}
	return s, nz
}

// Decode corrects cw in a copy and returns it with the number of corrected bytes.
func (r *RS) Decode(in []byte) ([]byte, int, error) {
	n := len(in)
	if n > 255 || n <= r.nsym {
		return nil, 0, ErrUncorrectable
	}
	cw := append([]byte(nil), in...)
	synd, nz := r.syndromes(cw)
	if !nz {
		return cw, 0, nil
	}
	nsym := r.nsym
	// Berlekamp-Massey, polynomials low-order first.
	C := make([]byte, nsym+1)
	B := make([]byte, nsym+1)
	C[0], B[0] = 1, 1
	L, m, b := 0, 1, byte(1)
	for k := 0; k < nsym; k++ {
		d := synd[k]
		for i := 1; i <= L; i++ {
			d ^= gfMul(C[i], synd[k-i])
		}
		if d == 0 {
			m++
			continue
		}
		T := append([]byte(nil), C...)
		coef := gfDiv(d, b)
		for i := 0; i+m <= nsym; i++ {
			C[i+m] ^= gfMul(coef, B[i])
		}
		if 2*L <= k {
			L = k + 1 - L
			B, b, m = T, d, 1
		} else {
			m++
		}
	}
	if L == 0 || L > nsym/2 {
		return nil, 0, ErrUncorrectable
	}
	// Chien search: error at power p when C(alpha^-p) == 0.
	var pos []int
	for p := 0; p < n; p++ {
		x := expTab[(255-p)%255]
		var v byte
		xp := byte(1)
		for i := 0; i <= L; i++ {
			v ^= gfMul(C[i], xp)
			xp = gfMul(xp, x)
		}
		if v == 0 {
			pos = append(pos, p)
		}
	}
	if len(pos) != L {
		return nil, 0, ErrUncorrectable
	}
	// Error evaluator Omega = S*C mod x^nsym.
	omega := make([]byte, nsym)
	for i := 0; i < nsym; i++ {
		var v byte
		for j := 0; j <= i && j <= L; j++ {
			v ^= gfMul(C[j], synd[i-j])
		}
		omega[i] = v
	}
	for _, p := range pos {
		xinv := expTab[(255-p)%255]
		var ov byte
		xp := byte(1)
		for i := 0; i < nsym; i++ {
			ov ^= gfMul(omega[i], xp)
			xp = gfMul(xp, xinv)
		}
		var dv byte // formal derivative of C at xinv
		xp = 1
		for i := 1; i <= L; i += 2 {
			dv ^= gfMul(C[i], xp)
			xp = gfMul(xp, gfMul(xinv, xinv))
		}
		if dv == 0 {
			return nil, 0, ErrUncorrectable
		}
		cw[n-1-p] ^= gfMul(expTab[p], gfDiv(ov, dv))
	}
	if _, nz := r.syndromes(cw); nz {
		return nil, 0, ErrUncorrectable
	}
	return cw, len(pos), nil
}
