// Package spec defines the roset symbol geometry and version table.
//
// All lengths are in "units": 1 unit is the centre-to-centre pitch of the small
// hexagonal data cells. The symbol is centred on the origin of the *page* frame
// (x right, y down).
//
// Layout, centre outward:
//
//	logo zone    a clear disc for the logo (never sampled)
//	inner hex    a solid hexagonal stroke around the logo zone
//	petals       six patches of hex cells around the centre patch: a 7-patch
//	             "flower" in which every patch has 3K²+3K+1 cells and the patches
//	             tile the hex lattice exactly (the centre patch holds the logo)
//	header ring  a hexagonal ring of binary dots/runs carrying the header
//	border       a solid hexagonal stroke; its six outer corners locate the symbol
//
// The data cells are a hex lattice. Petal i is the set of cells within hex
// distance K of S_i, where S_0 = (K+1, K) in axial coordinates and S_i is S_0
// rotated by i*60 degrees. Because 3K²+3K+1 = (K+1)² + (K+1)K + K², those
// vectors generate a sublattice whose fundamental domain is exactly one patch,
// so the flower has no gaps or overlaps (checked by TestPetalsTile).
package spec

import (
	"math"
	"sort"
	"sync"
)

const (
	HeaderBits   = 32 // ver(4) mode(4) len(8) crc16(16)
	HeaderCopies = 3  // three copies, bitwise majority vote
	HeaderSlots  = HeaderBits * HeaderCopies
	MinSlotPitch = 0.9  // minimum header slot pitch along the ring
	DotRadius    = 0.38 // data dot / header dot radius
	InnerStroke  = 0.55 // inner hexagon stroke width
	BorderStroke = 0.8  // border hexagon stroke width
	QuietZone    = 2.0
	MinParity    = 16 // minimum Reed-Solomon parity bytes in any version
	gapInner     = 0.35
	gapHeader    = 1.15 // data cell centre to header dot centre, at least
	gapBorder    = 1.1  // header ring to border centreline
	sqrt3        = 1.7320508075688772
	NumPetals    = 6
)

// Cell is one data cell.
type Cell struct {
	Q, R  int     // axial lattice coordinates
	X, Y  float64 // page position
	Petal int
}

// Geometry is everything about a symbol that depends only on the patch radius K.
type Geometry struct {
	K       int
	PetalN  int    // cells per petal, 3K²+3K+1
	Cells   []Cell // 6*PetalN cells, petal-major, each petal in spiral order
	Rot     float64
	AInner  float64 // inner hexagon apothem (stroke centreline)
	LogoR   float64 // clear logo disc radius
	AHeader float64 // header ring apothem
	ABorder float64 // border apothem (stroke centreline)
	Half    float64 // half the canvas width

	grid     []int32 // (q,r) -> flat cell index+1 (0 = none)
	qMin     int
	rMin     int
	gw, gh   int
	headerXY [HeaderSlots][2]float64
}

var (
	geoMu sync.Mutex
	geos  = map[int]*Geometry{}
)

// Geo returns the (cached) geometry for patch radius K.
func Geo(k int) *Geometry {
	geoMu.Lock()
	defer geoMu.Unlock()
	if g, ok := geos[k]; ok {
		return g
	}
	g := buildGeometry(k)
	geos[k] = g
	return g
}

func hexDist(q, r int) int { return max(abs(q), abs(r), abs(q+r)) }
func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// lattice maps axial coordinates to the unrotated cartesian lattice frame.
func lattice(q, r int) (float64, float64) {
	return float64(q) + float64(r)/2, float64(r) * sqrt3 / 2
}

func rot60(q, r int) (int, int) { return -r, q + r }

// HexNorm is the "hexagonal distance" of a page point: the apothem of the
// flat-top hexagon through it. The six normals point at -90, -30, 30, ... degrees.
func HexNorm(x, y float64) float64 {
	best := math.Inf(-1)
	for i := 0; i < 6; i++ {
		a := -math.Pi/2 + float64(i)*math.Pi/3
		if v := x*math.Cos(a) + y*math.Sin(a); v > best {
			best = v
		}
	}
	return best
}

// ApothemToCircumradius converts a flat-top hexagon's apothem to its circumradius.
func ApothemToCircumradius(a float64) float64 { return a * 2 / sqrt3 }

func buildGeometry(k int) *Geometry {
	g := &Geometry{K: k, PetalN: 3*k*k + 3*k + 1}
	// patch cells in spiral order (by ring, then by angle)
	type pc struct{ q, r int }
	var patch []pc
	for q := -k; q <= k; q++ {
		for r := -k; r <= k; r++ {
			if hexDist(q, r) <= k {
				patch = append(patch, pc{q, r})
			}
		}
	}
	ang := func(p pc) float64 {
		x, y := lattice(p.q, p.r)
		a := math.Atan2(y, x)
		if a < 0 {
			a += 2 * math.Pi
		}
		return a
	}
	sort.Slice(patch, func(i, j int) bool {
		di, dj := hexDist(patch[i].q, patch[i].r), hexDist(patch[j].q, patch[j].r)
		if di != dj {
			return di < dj
		}
		return ang(patch[i]) < ang(patch[j])
	})
	// the page rotation puts petal 0's direction straight up
	sx, sy := lattice(k+1, k)
	g.Rot = -math.Pi/2 - math.Atan2(sy, sx)
	cr, sr := math.Cos(g.Rot), math.Sin(g.Rot)
	sq, sr2 := k+1, k
	for p := 0; p < NumPetals; p++ {
		for _, c := range patch {
			q, r := sq+c.q, sr2+c.r
			x, y := lattice(q, r)
			g.Cells = append(g.Cells, Cell{Q: q, R: r, X: x*cr - y*sr, Y: x*sr + y*cr, Petal: p})
		}
		sq, sr2 = rot60(sq, sr2)
	}
	// dense lookup grid
	qmin, qmax, rmin, rmax := 1<<30, -(1 << 30), 1<<30, -(1 << 30)
	for _, c := range g.Cells {
		qmin, qmax = min(qmin, c.Q), max(qmax, c.Q)
		rmin, rmax = min(rmin, c.R), max(rmax, c.R)
	}
	g.qMin, g.rMin, g.gw, g.gh = qmin-1, rmin-1, qmax-qmin+3, rmax-rmin+3
	g.grid = make([]int32, g.gw*g.gh)
	for i, c := range g.Cells {
		g.grid[(c.R-g.rMin)*g.gw+(c.Q-g.qMin)] = int32(i + 1)
	}
	// ring radii from the cell cloud
	hmin, hmax := math.Inf(1), math.Inf(-1)
	for _, c := range g.Cells {
		h := HexNorm(c.X, c.Y)
		hmin, hmax = math.Min(hmin, h), math.Max(hmax, h)
	}
	g.AInner = hmin - DotRadius - gapInner - InnerStroke/2
	g.LogoR = g.AInner - InnerStroke/2 - gapInner
	g.AHeader = math.Max(hmax+gapHeader, MinSlotPitch*HeaderSlots/(6*2/sqrt3))
	g.ABorder = g.AHeader + gapBorder
	g.Half = ApothemToCircumradius(g.ABorder+BorderStroke/2) + QuietZone
	// header slots: equally spaced along the hexagon, six sides of 16 slots,
	// starting at the vertex at angle 0 and running clockwise on screen
	rv := ApothemToCircumradius(g.AHeader)
	per := HeaderSlots / 6
	for t := 0; t < HeaderSlots; t++ {
		side, f := t/per, (float64(t%per)+0.5)/float64(per)
		a0, a1 := float64(side)*math.Pi/3, float64(side+1)*math.Pi/3
		x0, y0 := rv*math.Cos(a0), rv*math.Sin(a0)
		x1, y1 := rv*math.Cos(a1), rv*math.Sin(a1)
		g.headerXY[t] = [2]float64{x0 + f*(x1-x0), y0 + f*(y1-y0)}
	}
	return g
}

// HeaderPos is the page position of header slot t (0..HeaderSlots-1).
func (g *Geometry) HeaderPos(t int) (x, y float64) { return g.headerXY[t][0], g.headerXY[t][1] }

// CellAt returns the index of the data cell with axial coordinates (q,r), or -1.
func (g *Geometry) CellAt(q, r int) int {
	qi, ri := q-g.qMin, r-g.rMin
	if qi < 0 || ri < 0 || qi >= g.gw || ri >= g.gh {
		return -1
	}
	return int(g.grid[ri*g.gw+qi]) - 1
}

// Neighbour offsets in axial coordinates, in angular order.
var Neighbours = [6][2]int{{1, 0}, {0, 1}, {-1, 1}, {-1, 0}, {0, -1}, {1, -1}}

// NearestLattice returns the axial coordinates of the lattice site nearest page point (x,y).
func (g *Geometry) NearestLattice(x, y float64) (int, int) {
	cr, sr := math.Cos(-g.Rot), math.Sin(-g.Rot)
	lx, ly := x*cr-y*sr, x*sr+y*cr
	fq := lx - ly/sqrt3
	fr := 2 * ly / sqrt3
	// cube rounding
	fx, fz := fq, fr
	fy := -fx - fz
	rx, ry, rz := math.Round(fx), math.Round(fy), math.Round(fz)
	dx, dy, dz := math.Abs(rx-fx), math.Abs(ry-fy), math.Abs(rz-fz)
	switch {
	case dx > dy && dx > dz:
		rx = -ry - rz
	case dy > dz:
		ry = -rx - rz
	default:
		rz = -rx - ry
	}
	return int(rx), int(rz)
}

// LatticePos is the page position of axial lattice site (q,r).
func (g *Geometry) LatticePos(q, r int) (float64, float64) {
	x, y := lattice(q, r)
	cr, sr := math.Cos(g.Rot), math.Sin(g.Rot)
	return x*cr - y*sr, x*sr + y*cr
}

// LogoSizes maps the --logo-size names to patch radii.
var LogoSizes = map[string]int{"small": 4, "medium": 5, "large": 6, "xlarge": 7}

// Version describes a symbol size and its error-correction layout.
type Version struct {
	ID      int
	K       int // patch radius
	Bits    int // bits per cell: 1 (two levels) or 2 (four levels)
	Payload int // payload capacity in bytes: 32 or 80
	N       int // Reed-Solomon codeword bytes
	NSym    int // parity bytes
}

func (v Version) Levels() int       { return 1 << v.Bits }
func (v Version) Capacity() int     { return v.Payload }
func (v Version) Geo() *Geometry    { return Geo(v.K) }
func (v Version) CellsPerByte() int { return 8 / v.Bits }

// BytesPerPetal is how many codeword bytes each petal holds.
func (v Version) BytesPerPetal() int { return v.Geo().PetalN * v.Bits / 8 }

// TotalCells is the number of data cells.
func (v Version) TotalCells() int { return NumPetals * v.Geo().PetalN }

// Versions is built at init. Every K (logo size) and bits-per-cell combination
// gets the payload classes (32 and 80 bytes) it can carry with at least MinParity
// parity bytes; spare capacity is spent on extra parity.
var Versions = buildVersions()

func buildVersions() []Version {
	var out []Version
	id := 1
	for _, bits := range []int{1, 2} {
		for _, payload := range []int{32, 80} {
			for _, k := range []int{4, 5, 6, 7} {
				n := min(255, NumPetals*((3*k*k+3*k+1)*bits/8))
				if n-payload < MinParity {
					continue
				}
				out = append(out, Version{ID: id, K: k, Bits: bits, Payload: payload, N: n, NSym: n - payload})
				id++
			}
		}
	}
	return out
}

func Lookup(id int) (Version, bool) {
	for _, v := range Versions {
		if v.ID == id {
			return v, true
		}
	}
	return Version{}, false
}
