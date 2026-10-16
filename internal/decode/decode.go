// Package decode finds a roset symbol in an image and decodes it.
//
// Pipeline: grayscale -> adaptive threshold -> connected components ->
// bullseye finder -> affine frame (finder, then boundary ring) -> sync-mark
// rotation search -> cell sampling in the symbol's own polar frame ->
// header CRC -> descramble -> Reed-Solomon. Failures at any stage fall through
// to the next candidate (other thresholds, inverted polarity, other frames,
// mirrored slot order, other versions).
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
	X, Y float64
	Bit  bool
}

// Report describes how a decode went. Fields describe the successful attempt,
// or the last attempt that got as far as sampling when decoding failed.
type Report struct {
	Version   int
	Unit      float64 // px per unit at analysis resolution
	Cx, Cy    float64
	Theta0    float64 // sync angle in the normalized frame (radians)
	Dir       int     // +1 clockwise slot order, -1 mirrored
	Binarizer string
	Inverted  bool
	FrameKind string // "projective", "boundary" or "finder"
	Attempts  int
	Stage     string // furthest stage reached on failure

	gray    *detect.Gray
	bin     *detect.Bin
	frame   detect.Frame
	ver     spec.Version
	samples []sample
	haveAtt bool
}

type pass struct {
	name string
	make func(g *detect.Gray) *detect.Bin
}

func passes(g *detect.Gray) []pass {
	m := g.W
	if g.H > m {
		m = g.H
	}
	odd := func(n int) int { return n | 1 }
	return []pass{
		{"bradley/8", func(g *detect.Gray) *detect.Bin { return detect.Bradley(g, odd(m/8), 0.15) }},
		{"otsu", func(g *detect.Gray) *detect.Bin { return detect.Otsu(g) }},
		{"bradley/16", func(g *detect.Gray) *detect.Bin { return detect.Bradley(g, odd(m/16), 0.10) }},
		{"bradley/6", func(g *detect.Gray) *detect.Bin { return detect.Bradley(g, odd(m/6), 0.22) }},
	}
}

func Decode(img image.Image, opt Options) (*codec.Result, *Report, error) {
	if opt.MaxDim == 0 {
		opt.MaxDim = 1600
	}
	g0, _ := detect.FromImage(img, opt.MaxDim)
	rep := &Report{Stage: "finder"}
	order := []bool{false, true}
	if borderMean(g0) < 110 { // dark background: light-on-dark symbols are far more likely
		order = []bool{true, false}
	}
	for _, inv := range order {
		g := g0
		if inv {
			g = g0.Invert()
		}
		for _, p := range passes(g) {
			bin := p.make(g)
			lab := detect.Label(bin)
			var triples []detect.Finder
			haveTriples := false
			pairs := map[float64][]detect.Finder{}
			for _, v := range spec.Versions {
				var fs []detect.Finder
				if !v.HasLogo() {
					if !haveTriples {
						triples, haveTriples = detect.FindFinders(lab, 6), true
					}
					fs = triples
				} else {
					if _, ok := pairs[v.LogoR]; !ok {
						pairs[v.LogoR] = detect.FindFinderPairs(lab, v.FinderBands(), 6)
					}
					fs = pairs[v.LogoR]
				}
				for _, fnd := range fs {
					if res := tryVersion(g, bin, lab, fnd, v, rep); res != nil {
						rep.Binarizer, rep.Inverted = p.name, inv
						return res, rep, nil
					}
				}
			}
		}
	}
	return nil, rep, fmt.Errorf("%w (reached stage %q after %d attempts)", ErrNoSymbol, rep.Stage, rep.Attempts)
}

func tryVersion(g *detect.Gray, bin *detect.Bin, lab *detect.Labels, fnd detect.Finder, v spec.Version, rep *Report) *codec.Result {
	type fr struct {
		kind string
		f    detect.Frame
	}
	var frames []fr
	base, ok := detect.FrameFromFinder(lab, fnd, v.FinderBands())
	if !ok {
		return nil
	}
	if pf, ok := detect.FrameFromRings(lab, fnd, v.BoundaryRadius()); ok {
		frames = append(frames, fr{"projective", pf})
	}
	if rf, ok := detect.RefineByBoundary(lab, base, fnd, v.BoundaryRadius()); ok {
		frames = append(frames, fr{"boundary", rf})
	}
	frames = append(frames, fr{"finder", base})
	for _, fm := range frames {
		rep.Attempts++
		if fm.f.Unit() < 1.8 {
			continue
		}
		theta, ok := findSync(bin, fm.f, v)
		if !ok {
			continue
		}
		for _, dir := range []int{1, -1} {
			cells, samples := readCells(bin, fm.f, v, theta, dir)
			rep.gray, rep.bin, rep.frame, rep.ver, rep.samples, rep.haveAtt = g, bin, fm.f, v, samples, true
			rep.Unit, rep.Cx, rep.Cy, rep.Theta0, rep.Dir, rep.FrameKind = fm.f.Unit(), fm.f.Cx, fm.f.Cy, theta, dir, fm.kind
			rep.Stage = "header"
			h, err := codec.ParseHeader(cells.Header)
			if err != nil || h.Version != v.ID {
				continue
			}
			rep.Stage = "reed-solomon"
			res, err := codec.DecodeData(v, h, cells.Data)
			if err != nil {
				continue
			}
			rep.Version = v.ID
			return res
		}
	}
	return nil
}

// findSync locates the sync dot's angle on the ring just outside the boundary.
func findSync(bin *detect.Bin, f detect.Frame, v spec.Version) (float64, bool) {
	const N = 360
	var d, sm [N]float64
	rs := v.SyncRadius()
	for a := 0; a < N; a++ {
		phi := float64(a) * 2 * math.Pi / N
		cnt := 0
		for _, dr := range [5]float64{-0.4, -0.2, 0, 0.2, 0.4} {
			x, y := f.ImgPoint(rs+dr, phi)
			if bin.Dark(int(math.Round(x)), int(math.Round(y))) {
				cnt++
			}
		}
		d[a] = float64(cnt) / 5
	}
	for a := 0; a < N; a++ {
		s := 0.0
		for k := -2; k <= 2; k++ {
			s += d[(a+k+N)%N]
		}
		sm[a] = s / 5
	}
	peak, pv := 0, -1.0
	for a := 0; a < N; a++ {
		if sm[a] > pv {
			peak, pv = a, sm[a]
		}
	}
	if pv < 0.6 {
		return 0, false
	}
	var other float64
	var on int
	for a := 0; a < N; a++ {
		off := (a-peak+N+N/2)%N - N/2
		if off < -15 || off > 15 {
			other += d[a]
			on++
		}
	}
	if other/float64(on) > 0.15 {
		return 0, false
	}
	var sw, sa float64
	for k := -6; k <= 6; k++ {
		w := d[(peak+k+N)%N]
		sw += w
		sa += w * float64(k)
	}
	if sw == 0 {
		return 0, false
	}
	return (float64(peak) + sa/sw) * 2 * math.Pi / N, true
}

func sampleBit(bin *detect.Bin, f detect.Frame, rho, phi float64) (bool, float64, float64) {
	sp, cp := math.Sin(phi), math.Cos(phi)
	votes := 0
	var cx, cy float64
	for i, o := range [5][2]float64{{0, 0}, {0.2, 0}, {-0.2, 0}, {0, 0.2}, {0, -0.2}} {
		r := rho + o[0]
		x, y := f.ImgQ(r*sp+o[1]*cp, -r*cp+o[1]*sp)
		if i == 0 {
			cx, cy = x, y
		}
		if bin.Dark(int(math.Round(x)), int(math.Round(y))) {
			votes++
		}
	}
	return votes >= 3, cx, cy
}

func readCells(bin *detect.Bin, f detect.Frame, v spec.Version, theta0 float64, dir int) (*codec.Cells, []sample) {
	var samples []sample
	read := func(n int, rc float64) []bool {
		bits := make([]bool, n)
		for i := range bits {
			phi := theta0 + float64(dir)*2*math.Pi*float64(i)/float64(n)
			b, x, y := sampleBit(bin, f, rc, phi)
			bits[i] = b
			samples = append(samples, sample{x, y, b})
		}
		return bits
	}
	c := &codec.Cells{}
	for j := 0; j < spec.HeaderRings; j++ {
		c.Header[j] = read(spec.HeaderSlots, v.HeaderRingRadius(j))
	}
	for k, n := range v.SlotCounts() {
		c.Data = append(c.Data, read(n, v.DataRingRadius(k)))
	}
	return c, samples
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
