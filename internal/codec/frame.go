// Package codec turns a payload into the bit matrix drawn on the rings and back.
//
// Pipeline (encode): payload -> zero-pad to capacity -> Reed-Solomon ->
// split into 8-cell groups -> deterministic group shuffle (burst protection) ->
// XOR whitening -> ring cells. The header lives on its own three rings, is not
// shuffled or whitened, and is protected by 3x repetition plus a CRC.
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

type Cells struct {
	Header [spec.HeaderRings][]bool
	Data   [][]bool
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

func packHeader(h Header) [spec.HeaderRings][]bool {
	w := headerWord(h)
	var out [spec.HeaderRings][]bool
	for j := range out {
		out[j] = make([]bool, spec.HeaderSlots)
		for b := 0; b < 32; b++ {
			out[j][(b+spec.HeaderRotStep*j)%32] = w>>(31-b)&1 == 1
		}
	}
	return out
}

// ParseHeader majority-votes the three header copies and verifies the CRC.
func ParseHeader(rings [spec.HeaderRings][]bool) (Header, error) {
	var words [spec.HeaderRings]uint32
	var votes [32]int
	for j := 0; j < spec.HeaderRings; j++ {
		if len(rings[j]) != spec.HeaderSlots {
			return Header{}, ErrHeader
		}
		for s := 0; s < 32; s++ {
			b := ((s-spec.HeaderRotStep*j)%32 + 32) % 32
			if rings[j][s] {
				words[j] |= 1 << (31 - b)
				votes[b]++
			}
		}
	}
	var maj uint32
	for b := 0; b < 32; b++ {
		if votes[b] >= 2 {
			maj |= 1 << (31 - b)
		}
	}
	for _, w := range []uint32{maj, words[0], words[1], words[2]} {
		if h, ok := wordHeader(w); ok {
			return h, nil
		}
	}
	return Header{}, ErrHeader
}

type plan struct {
	perm   []int  // codeword group g -> cell-group position
	whiten []bool // per flat cell
	groups int
}

var (
	plans   sync.Map
	rsCache sync.Map
)

func getPlan(v spec.Version) *plan {
	if p, ok := plans.Load(v.ID); ok {
		return p.(*plan)
	}
	cells := v.TotalCells()
	groups := cells / 8
	perm := make([]int, groups)
	for i := range perm {
		perm[i] = i
	}
	r := &xorshift32{0xC0DEC0DE}
	for i := groups - 1; i > 0; i-- {
		j := int(r.next() % uint32(i+1))
		perm[i], perm[j] = perm[j], perm[i]
	}
	w := &xorshift32{0x9E3779B9}
	wh := make([]bool, cells)
	for i := range wh {
		wh[i] = w.next()>>16&1 == 1
	}
	p := &plan{perm: perm, whiten: wh, groups: groups}
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

// PickVersion returns the smallest version with the given logo-zone radius
// (0 = none) whose capacity fits n payload bytes.
func PickVersion(n int, logoR float64) (spec.Version, error) {
	for _, v := range spec.Versions {
		if v.LogoR == logoR && n <= v.Capacity() && n <= 255 {
			return v, nil
		}
	}
	return spec.Version{}, fmt.Errorf("payload of %d bytes exceeds the largest symbol (%d bytes)", n, spec.Versions[len(spec.Versions)-1].Capacity())
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
	flat := make([]bool, v.TotalCells())
	for g := 0; g < p.groups; g++ {
		var by byte
		if g < v.N {
			by = cw[g]
		}
		base := 8 * p.perm[g]
		for b := 0; b < 8; b++ {
			flat[base+b] = by>>(7-b)&1 == 1
		}
	}
	for i := range flat {
		flat[i] = flat[i] != p.whiten[i]
	}
	c := &Cells{Header: packHeader(Header{Version: v.ID, Mode: mode, Length: len(payload)})}
	off := 0
	for _, n := range v.SlotCounts() {
		c.Data = append(c.Data, flat[off:off+n:off+n])
		off += n
	}
	return c, nil
}

// DecodeData recovers the payload from data-ring cells given a parsed header.
func DecodeData(v spec.Version, h Header, data [][]bool) (*Result, error) {
	if h.Version != v.ID {
		return nil, fmt.Errorf("header version %d does not match geometry version %d", h.Version, v.ID)
	}
	if h.Length > v.Capacity() {
		return nil, fmt.Errorf("header length %d exceeds capacity", h.Length)
	}
	counts := v.SlotCounts()
	if len(data) != len(counts) {
		return nil, errors.New("codec: wrong number of data rings")
	}
	flat := make([]bool, 0, v.TotalCells())
	for k, ring := range data {
		if len(ring) != counts[k] {
			return nil, errors.New("codec: wrong slot count on ring")
		}
		flat = append(flat, ring...)
	}
	p := getPlan(v)
	for i := range flat {
		flat[i] = flat[i] != p.whiten[i]
	}
	cw := make([]byte, v.N)
	for g := 0; g < v.N; g++ {
		base := 8 * p.perm[g]
		var by byte
		for b := 0; b < 8; b++ {
			by <<= 1
			if flat[base+b] {
				by |= 1
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
