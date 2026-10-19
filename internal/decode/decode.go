// Package decode finds a roset symbol in an image and decodes it.
//
// Pipeline: grayscale -> adaptive threshold -> connected components -> the
// symbol's solid hexagonal border (convex hull, six side fits, six corners) ->
// homography from the border's corners -> try the 6 rotations x 2 mirrorings:
// read the header ring and check its CRC -> sample every data cell's luminance,
// cluster it into the symbol's ink levels -> descramble -> Reed-Solomon.
// Failures at any stage fall through to the next candidate (other thresholds,
// inverted polarity, other border candidates, other patch radii, other
// orientations).
package decode

import (
	"errors"
	"fmt"
	"image"
	"math"

	"roset/internal/codec"
	"roset/internal/detect"
	"roset/internal/spec"
)

var ErrNoSymbol = errors.New("no roset symbol found")

type Options struct {
	MaxDim int // analysis resolution cap (default 1600)
}

type sample struct {
	X, Y  float64
	Level int // data cells: the decoded level; header slots: 0/1; -1 for corners
}

// Report describes how a decode went. Fields describe the successful attempt,
// or the last attempt that got as far as sampling when decoding failed.
type Report struct {
	Version   int
	Unit      float64 // px per unit at analysis resolution
	Rotation  int     // border corner offset that matched (0..5)
	Mirrored  bool
	Binarizer string
	Inverted  bool
	FrameKind string
	Attempts  int
	Stage     string // furthest stage reached on failure
	Levels    []float64

	gray    *detect.Gray
	bin     *detect.Bin
	corners [6][2]float64
	samples []sample
	haveAtt bool
}

type pass struct {
	name string
	make func(g *detect.Gray) *detect.Bin
}

func passes(g *detect.Gray) []pass {
	m := max(g.W, g.H)
	odd := func(n int) int { return n | 1 }
	return []pass{
		{"bradley/8", func(g *detect.Gray) *detect.Bin { return detect.Bradley(g, odd(m/8), 0.15) }},
		{"otsu", func(g *detect.Gray) *detect.Bin { return detect.Otsu(g) }},
		{"bradley/16", func(g *detect.Gray) *detect.Bin { return detect.Bradley(g, odd(m/16), 0.10) }},
		{"bradley/6", func(g *detect.Gray) *detect.Bin { return detect.Bradley(g, odd(m/6), 0.22) }},
	}
}

// patchRadii lists the distinct K values of the version table.
func patchRadii() []int {
	var out []int
	seen := map[int]bool{}
	for _, v := range spec.Versions {
		if !seen[v.K] {
			seen[v.K] = true
			out = append(out, v.K)
		}
	}
	return out
}

func Decode(img image.Image, opt Options) (*codec.Result, *Report, error) {
	if opt.MaxDim == 0 {
		opt.MaxDim = 1600
	}
	g0, _ := detect.FromImage(img, opt.MaxDim)
	rep := &Report{Stage: "border", FrameKind: "hexagon"}
	order := []bool{false, true}
	if borderMean(g0) < 110 { // dark background: light-on-dark symbols are far more likely
		order = []bool{true, false}
	}
	ks := patchRadii()
	for _, inv := range order {
		g := g0
		if inv {
			g = g0.Invert()
		}
		for _, p := range passes(g) {
			bin := p.make(g)
			lab := detect.Label(bin)
			for _, cand := range detect.FindHexagons(lab, max(24, min(g.W, g.H)/20), 6) {
				for _, k := range ks {
					if res := tryHexagon(g, bin, cand, spec.Geo(k), rep); res != nil {
						rep.Binarizer, rep.Inverted = p.name, inv
						return res, rep, nil
					}
				}
			}
		}
	}
	return nil, rep, fmt.Errorf("%w (reached stage %q after %d attempts)", ErrNoSymbol, rep.Stage, rep.Attempts)
}

// canonCorners are the border's outer corners in page units.
func canonCorners(geo *spec.Geometry) [6][2]float64 {
	r := spec.ApothemToCircumradius(geo.ABorder + spec.BorderStroke/2)
	var c [6][2]float64
	for j := range c {
		a := float64(j) * math.Pi / 3
		c[j] = [2]float64{r * math.Cos(a), r * math.Sin(a)}
	}
	return c
}

func tryHexagon(g *detect.Gray, bin *detect.Bin, cand detect.HexCand, geo *spec.Geometry, rep *Report) *codec.Result {
	canon := canonCorners(geo)
	for _, dir := range []int{1, -1} {
		for rot := 0; rot < 6; rot++ {
			src := make([][2]float64, 6)
			dst := make([][2]float64, 6)
			for j := 0; j < 6; j++ {
				src[j] = canon[j]
				dst[j] = cand.V[((rot+dir*j)%6+6)%6]
			}
			H, ok := detect.Homography(src, dst)
			if !ok {
				continue
			}
			rep.Attempts++
			x0, y0 := H.Apply(0, 0)
			x1, y1 := H.Apply(1, 0)
			unit := math.Hypot(x1-x0, y1-y0)
			if unit < 1.6 || math.IsNaN(unit) {
				continue
			}
			f := &frame{g: g, H: H, unit: unit}
			ink, paper, ok := f.levels(geo)
			if !ok {
				continue
			}
			rep.gray, rep.bin, rep.corners, rep.haveAtt = g, bin, cand.V, true
			rep.Unit, rep.Rotation, rep.Mirrored = unit, rot, dir < 0
			rep.Stage = "header"
			hdr, hs := f.readHeader(geo, ink, paper)
			rep.samples = hs
			h, err := codec.ParseHeader(hdr)
			if err != nil {
				continue
			}
			v, ok := spec.Lookup(h.Version)
			if !ok || v.K != geo.K {
				continue
			}
			rep.Stage = "reed-solomon"
			data, ds := f.readData(v, ink, paper, rep)
			rep.samples = append(rep.samples, ds...)
			res, err := codec.DecodeData(v, h, data)
			if err != nil {
				continue
			}
			rep.Version = v.ID
			return res
		}
	}
	return nil
}

// frame maps page units to image pixels and samples luminance.
type frame struct {
	g    *detect.Gray
	H    detect.Mat3
	unit float64
}

// lum is the mean luminance over a small cross around page point (x,y).
func (f *frame) lum(x, y float64) (float64, float64, float64) {
	const o = 0.18
	var sum float64
	var cx, cy float64
	for i, d := range [5][2]float64{{0, 0}, {o, 0}, {-o, 0}, {0, o}, {0, -o}} {
		px, py := f.H.Apply(x+d[0], y+d[1])
		if i == 0 {
			cx, cy = px, py
		}
		sum += f.g.Bilinear(px, py)
	}
	return sum / 5, cx, cy
}

// levels estimates the ink and paper luminance from the border stroke and the
// margin just outside it.
func (f *frame) levels(geo *spec.Geometry) (ink, paper float64, ok bool) {
	r := spec.ApothemToCircumradius(1)
	var si, sp float64
	n := 0
	for side := 0; side < 6; side++ {
		a0, a1 := float64(side)*math.Pi/3, float64(side+1)*math.Pi/3
		for k := 1; k <= 8; k++ {
			t := 0.2 + 0.6*float64(k-1)/7
			at := func(apothem float64) (float64, float64) {
				rv := r * apothem
				return rv * (math.Cos(a0) + t*(math.Cos(a1)-math.Cos(a0))), rv * (math.Sin(a0) + t*(math.Sin(a1)-math.Sin(a0)))
			}
			x, y := at(geo.ABorder)
			l, _, _ := f.lum(x, y)
			si += l
			x, y = at(geo.ABorder + spec.BorderStroke/2 + 1.0)
			l, _, _ = f.lum(x, y)
			sp += l
			n++
		}
	}
	ink, paper = si/float64(n), sp/float64(n)
	return ink, paper, paper-ink >= 25
}

func (f *frame) readHeader(geo *spec.Geometry, ink, paper float64) ([spec.HeaderCopies][]bool, []sample) {
	var out [spec.HeaderCopies][]bool
	var samples []sample
	for j := range out {
		out[j] = make([]bool, spec.HeaderBits)
		for b := 0; b < spec.HeaderBits; b++ {
			x, y := geo.HeaderPos(j*spec.HeaderBits + b)
			l, px, py := f.lum(x, y)
			on := math.Abs(l-ink) < math.Abs(l-paper)
			out[j][b] = on
			lv := 0
			if on {
				lv = 1
			}
			samples = append(samples, sample{px, py, lv})
		}
	}
	return out, samples
}

// readData samples every data cell and clusters the luminances into the
// version's levels. The data is whitened, so all levels are used about equally;
// k-means seeded from the measured ink and paper adapts to contrast loss.
func (f *frame) readData(v spec.Version, ink, paper float64, rep *Report) ([]uint8, []sample) {
	geo := v.Geo()
	n := len(geo.Cells)
	lums := make([]float64, n)
	pos := make([][2]float64, n)
	for i, c := range geo.Cells {
		l, px, py := f.lum(c.X, c.Y)
		lums[i], pos[i] = l, [2]float64{px, py}
	}
	centers, levels := cluster(lums, v.Levels(), ink, paper)
	rep.Levels = centers
	samples := make([]sample, n)
	for i := range samples {
		samples[i] = sample{pos[i][0], pos[i][1], int(levels[i])}
	}
	return levels, samples
}

// cluster is 1-D k-means with L centres, seeded evenly between paper (level 0)
// and ink (level L-1). It returns the final centres and each value's level.
func cluster(vals []float64, L int, ink, paper float64) ([]float64, []uint8) {
	c := make([]float64, L)
	for l := range c {
		c[l] = paper + (ink-paper)*float64(l)/float64(L-1)
	}
	nearest := func(v float64) int {
		best, bi := math.Inf(1), 0
		for l, cv := range c {
			if d := math.Abs(v - cv); d < best {
				best, bi = d, l
			}
		}
		return bi
	}
	for it := 0; it < 12; it++ {
		sum := make([]float64, L)
		cnt := make([]int, L)
		for _, v := range vals {
			l := nearest(v)
			sum[l] += v
			cnt[l]++
		}
		for l := range c {
			if cnt[l] > 0 {
				c[l] = sum[l] / float64(cnt[l])
			}
		}
	}
	out := make([]uint8, len(vals))
	for i, v := range vals {
		out[i] = uint8(nearest(v))
	}
	return c, out
}

// borderMean is the mean luminance of a thin frame around the image.
func borderMean(g *detect.Gray) float64 {
	t := max(1, min(g.W, g.H)/20)
	var sum float64
	var n int
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			if x < t || y < t || x >= g.W-t || y >= g.H-t {
				sum += float64(g.Pix[y*g.W+x])
				n++
			}
		}
	}
	if n == 0 {
		return 255
	}
	return sum / float64(n)
}
