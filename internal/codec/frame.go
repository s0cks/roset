// Package codec turns a payload into the cell matrix drawn on the hex flower and back.
//
// Pipeline (encode): payload -> zero-pad to capacity -> Reed-Solomon ->
// codeword byte g goes to petal g%6 (so a whole damaged petal costs at most one
// sixth of the codeword) -> deterministic per-petal byte shuffle (burst
// protection) -> XOR whitening -> bits packed 1 or 2 per cell -> Gray-coded
// levels. The header sits on its own ring, is not shuffled or whitened, and is
// protected by 3x repetition plus a CRC.
package codec

import (
	"errors"
	"fmt"
	"sync"

	"roset/internal/spec"
)

const (
	ModeRawSHA256 = 0 // payload is exactly 32 bytes
	ModeSHA256Tag = 1 // 32-byte hash followed by tag bytes
	ModeEscape    = 15
)

var ErrHeader = errors.New("codec: header unreadable (CRC failed)")

type Header struct{ Version, Mode, Length int }

// Cells is the symbol content: the header copies and one level per data cell,
// in the order of spec.Geometry.Cells. Levels are 0 (background) .. Levels()-1 (full ink).
type Cells struct {
	Header [spec.HeaderCopies][]bool
	Data   []uint8
}

type Result struct {
	Version   int
	Mode      int
	Payload   []byte
	Corrected int // number of corrupted bytes repaired by Reed-Solomon
}

// Hash returns the first 32 payload bytes and any trailing tag.
func (r *Result) Hash() (hash, tag []byte) {
	if len(r.Payload) < 32 {
		return nil, r.Payload
	}
	return r.Payload[:32], r.Payload[32:]
}

func headerBytes(h Header) []byte { return []byte{byte(h.Version<<4 | h.Mode), byte(h.Length)} }

func headerWord(h Header) uint32 {
	return uint32(h.Version&15)<<28 | uint32(h.Mode&15)<<24 | uint32(h.Length&255)<<16 | uint32(CRC16(headerBytes(h)))
}

func wordHeader(w uint32) (Header, bool) {
	h := Header{Version: int(w>>28) & 15, Mode: int(w>>24) & 15, Length: int(w>>16) & 255}
	return h, uint16(w&0xFFFF) == CRC16(headerBytes(h))
}

func packHeader(h Header) [spec.HeaderCopies][]bool {
	w := headerWord(h)
	var out [spec.HeaderCopies][]bool
	for j := range out {
		out[j] = make([]bool, spec.HeaderBits)
		for b := 0; b < spec.HeaderBits; b++ {
			out[j][b] = w>>(31-b)&1 == 1
		}
	}
	return out
}

// ParseHeader majority-votes the header copies and verifies the CRC; each copy is
// also tried on its own, so one clean copy is always enough.
func ParseHeader(copies [spec.HeaderCopies][]bool) (Header, error) {
	var words [spec.HeaderCopies]uint32
	var votes [spec.HeaderBits]int
	for j := 0; j < spec.HeaderCopies; j++ {
		if len(copies[j]) != spec.HeaderBits {
			return Header{}, ErrHeader
		}
		for b := 0; b < spec.HeaderBits; b++ {
			if copies[j][b] {
				words[j] |= 1 << (31 - b)
				votes[b]++
			}
		}
	}
	var maj uint32
	for b := 0; b < spec.HeaderBits; b++ {
		if votes[b]*2 > spec.HeaderCopies {
			maj |= 1 << (31 - b)
		}
	}
	for _, w := range append([]uint32{maj}, words[:]...) {
		if h, ok := wordHeader(w); ok {
			return h, nil
		}
	}
	return Header{}, ErrHeader
}

// plan is the version's deterministic permutation and whitening.
type plan struct {
	perm   [][]int // perm[petal][byte index in petal] -> byte slot in the petal
	whiten []byte  // one bit per codeword-bit position, per flat cell*bits
	filler []uint8 // levels for cells not covered by any byte slot
}

var (
	plans   sync.Map
	rsCache sync.Map
)

func getPlan(v spec.Version) *plan {
	if p, ok := plans.Load(v.ID); ok {
		return p.(*plan)
	}
	bpp := v.BytesPerPetal()
	perm := make([][]int, spec.NumPetals)
	r := &xorshift32{0xC0DEC0DE}
	for p := range perm {
		perm[p] = make([]int, bpp)
		for i := range perm[p] {
			perm[p][i] = i
		}
		for i := bpp - 1; i > 0; i-- {
			j := int(r.next() % uint32(i+1))
			perm[p][i], perm[p][j] = perm[p][j], perm[p][i]
		}
	}
	cells := v.TotalCells()
	w := &xorshift32{0x9E3779B9}
	wh := make([]byte, cells*v.Bits)
	for i := range wh {
		wh[i] = byte(w.next() >> 16 & 1)
	}
	f := &xorshift32{0x1B873593}
	fill := make([]uint8, cells)
	for i := range fill {
		fill[i] = uint8(f.next()>>16) & uint8(v.Levels()-1)
	}
	p := &plan{perm: perm, whiten: wh, filler: fill}
	plans.Store(v.ID, p)
	return p
}

func getRS(nsym int) *RS {
	if r, ok := rsCache.Load(nsym); ok {
		return r.(*RS)
	}
	r := NewRS(nsym)
	rsCache.Store(nsym, r)
	return r
}

// gray maps between a bit group and a level so that neighbouring levels differ in one bit.
func gray(x int) int { return x ^ (x >> 1) }

// byteCells returns the flat index of the first cell of codeword byte g.
func byteCells(v spec.Version, p *plan, g int) int {
	petal, idx := g%spec.NumPetals, g/spec.NumPetals
	return petal*v.Geo().PetalN + p.perm[petal][idx]*v.CellsPerByte()
}

// PickVersion returns the smallest version with patch radius k and the given
// bits per cell whose capacity fits n payload bytes.
func PickVersion(n, k, bits int) (spec.Version, error) {
	any := false
	for _, v := range spec.Versions {
		if v.K != k || v.Bits != bits {
			continue
		}
		any = true
		if n <= v.Capacity() && n <= 255 {
			return v, nil
		}
	}
	if !any {
		return spec.Version{}, fmt.Errorf("no symbol with patch radius %d and %d levels per cell; try a larger --logo-size or --levels 4", k, 1<<bits)
	}
	return spec.Version{}, fmt.Errorf("payload of %d bytes does not fit this logo size / level count; try a larger --logo-size or --levels 4", n)
}

// Encode builds the cell matrix for a payload.
func Encode(v spec.Version, mode int, payload []byte) (*Cells, error) {
	if len(payload) > v.Capacity() {
		return nil, fmt.Errorf("payload %d bytes > capacity %d of version %d", len(payload), v.Capacity(), v.ID)
	}
	if mode < 0 || mode > 15 {
		return nil, fmt.Errorf("mode %d out of range", mode)
	}
	msg := make([]byte, v.Capacity())
	copy(msg, payload)
	cw := getRS(v.NSym).Encode(msg)
	p := getPlan(v)
	c := &Cells{Header: packHeader(Header{Version: v.ID, Mode: mode, Length: len(payload)})}
	c.Data = append([]uint8(nil), p.filler...)
	cpb := v.CellsPerByte()
	for g := 0; g < v.N; g++ {
		base := byteCells(v, p, g)
		for ci := 0; ci < cpb; ci++ {
			x := 0
			for b := 0; b < v.Bits; b++ {
				bitPos := ci*v.Bits + b
				bit := int(cw[g]>>(7-bitPos)) & 1
				bit ^= int(p.whiten[(base+ci)*v.Bits+b])
				x = x<<1 | bit
			}
			c.Data[base+ci] = uint8(gray(x))
		}
	}
	return c, nil
}

// DecodeData recovers the payload from cell levels given a parsed header.
func DecodeData(v spec.Version, h Header, data []uint8) (*Result, error) {
	if h.Version != v.ID {
		return nil, fmt.Errorf("header version %d does not match geometry version %d", h.Version, v.ID)
	}
	if h.Length > v.Capacity() {
		return nil, fmt.Errorf("header length %d exceeds capacity", h.Length)
	}
	if len(data) != v.TotalCells() {
		return nil, errors.New("codec: wrong number of data cells")
	}
	p := getPlan(v)
	cpb := v.CellsPerByte()
	cw := make([]byte, v.N)
	for g := range cw {
		base := byteCells(v, p, g)
		var by byte
		for ci := 0; ci < cpb; ci++ {
			x := gray(int(data[base+ci]) & (v.Levels() - 1))
			for b := 0; b < v.Bits; b++ {
				bit := x >> (v.Bits - 1 - b) & 1
				bit ^= int(p.whiten[(base+ci)*v.Bits+b])
				by = by<<1 | byte(bit)
			}
		}
		cw[g] = by
	}
	fixed, nerr, err := getRS(v.NSym).Decode(cw)
	if err != nil {
		return nil, err
	}
	return &Result{Version: v.ID, Mode: h.Mode, Payload: fixed[:h.Length], Corrected: nerr}, nil
}

// Decode is the convenience inverse of Encode for an exact cell matrix.
func Decode(c *Cells) (*Result, error) {
	h, err := ParseHeader(c.Header)
	if err != nil {
		return nil, err
	}
	v, ok := spec.Lookup(h.Version)
	if !ok {
		return nil, fmt.Errorf("unknown symbol version %d", h.Version)
	}
	return DecodeData(v, h, c.Data)
}
