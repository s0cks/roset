// Package layout is the single source of truth for what is drawn where.
// Ink(x,y) answers "how much ink is at this point?" for both the PNG rasterizer
// and tests; the SVG writer emits the equivalent vector primitives.
package layout

import (
	"fmt"
	"math"

	"roset/internal/codec"
	"roset/internal/spec"
)

type Style int

const (
	StyleDots Style = iota // every inked cell is its own dot
	StyleRuns              // neighbouring cells of the same level merge into capsule strokes
)

func ParseStyle(s string) (Style, error) {
	switch s {
	case "dots":
		return StyleDots, nil
	case "runs":
		return StyleRuns, nil
	}
	return 0, fmt.Errorf("unknown style %q (want dots or runs)", s)
}

type Layout struct {
	V     spec.Version
	G     *spec.Geometry
	Cells *codec.Cells
	Style Style
}

func New(v spec.Version, c *codec.Cells, s Style) *Layout {
	return &Layout{V: v, G: v.Geo(), Cells: c, Style: s}
}

// Level is the ink level (0 = background) of data cell i as a 0..1 darkness.
func (l *Layout) Darkness(level uint8) float64 {
	return float64(level) / float64(l.V.Levels()-1)
}

// Pair is two adjacent data cells of the same non-zero level (a "run" link).
type Pair struct{ A, B int }

// Pairs lists every link between neighbouring data cells of the same non-zero level.
func (l *Layout) Pairs() []Pair {
	var out []Pair
	for i, c := range l.G.Cells {
		if l.Cells.Data[i] == 0 {
			continue
		}
		for _, d := range spec.Neighbours[:3] { // half the directions: each pair once
			j := l.G.CellAt(c.Q+d[0], c.R+d[1])
			if j >= 0 && l.Cells.Data[j] == l.Cells.Data[i] {
				out = append(out, Pair{i, j})
			}
		}
	}
	return out
}

// HeaderBit is the value of header slot t (copy t/32, bit t%32).
func (l *Layout) HeaderBit(t int) bool {
	return l.Cells.Header[t/spec.HeaderBits][t%spec.HeaderBits]
}

// HeaderRuns lists maximal circular runs of set header slots as (start, length);
// an all-set ring is one run of HeaderSlots.
func (l *Layout) HeaderRuns() []Pair {
	n := spec.HeaderSlots
	z := -1
	for t := 0; t < n; t++ {
		if !l.HeaderBit(t) {
			z = t
			break
		}
	}
	if z < 0 {
		return []Pair{{0, n}}
	}
	var out []Pair
	cur := Pair{A: -1}
	for k := 1; k <= n; k++ {
		t := (z + k) % n
		if l.HeaderBit(t) {
			if cur.A < 0 {
				cur = Pair{A: t}
			}
			cur.B++
		} else if cur.A >= 0 {
			out = append(out, cur)
			cur = Pair{A: -1}
		}
	}
	return out
}

func segDist2(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := 0.0
	if d2 := dx*dx + dy*dy; d2 > 0 {
		t = math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/d2))
	}
	ex, ey := px-(ax+t*dx), py-(ay+t*dy)
	return ex*ex + ey*ey
}

// Ink returns the ink coverage (0 = background, 1 = full ink) at page point (x,y)
// in units. The clear logo disc reports 0; the renderer composites the logo there.
func (l *Layout) Ink(x, y float64) float64 {
	g := l.G
	if x*x+y*y <= g.LogoR*g.LogoR {
		return 0
	}
	h := spec.HexNorm(x, y)
	if h > g.ABorder+spec.BorderStroke/2+0.01 {
		return 0
	}
	if math.Abs(h-g.ABorder) <= spec.BorderStroke/2 || math.Abs(h-g.AInner) <= spec.InnerStroke/2 {
		return 1
	}
	switch {
	case h < g.AInner:
		return 0
	case math.Abs(h-g.AHeader) <= spec.DotRadius+0.01:
		return l.headerInk(x, y)
	case h < g.AHeader-0.5:
		return l.dataInk(x, y)
	}
	return 0
}

func (l *Layout) headerInk(x, y float64) float64 {
	r2 := spec.DotRadius * spec.DotRadius
	n := spec.HeaderSlots
	for t := 0; t < n; t++ {
		if !l.HeaderBit(t) {
			continue
		}
		ax, ay := l.G.HeaderPos(t)
		if (x-ax)*(x-ax)+(y-ay)*(y-ay) <= r2 {
			return 1
		}
		if l.Style == StyleRuns {
			u := (t + 1) % n
			if l.HeaderBit(u) {
				bx, by := l.G.HeaderPos(u)
				if segDist2(x, y, ax, ay, bx, by) <= r2 {
					return 1
				}
			}
		}
	}
	return 0
}

func (l *Layout) dataInk(x, y float64) float64 {
	g := l.G
	q0, r0 := g.NearestLattice(x, y)
	r2 := spec.DotRadius * spec.DotRadius
	best := 0.0
	visit := func(q, r int) {
		i := g.CellAt(q, r)
		if i < 0 || l.Cells.Data[i] == 0 {
			return
		}
		lv := l.Cells.Data[i]
		t := l.Darkness(lv)
		if t <= best {
			return
		}
		ax, ay := g.LatticePos(q, r)
		if (x-ax)*(x-ax)+(y-ay)*(y-ay) <= r2 {
			best = t
			return
		}
		if l.Style != StyleRuns {
			return
		}
		for _, d := range spec.Neighbours[:3] {
			j := g.CellAt(q+d[0], r+d[1])
			if j < 0 || l.Cells.Data[j] != lv {
				continue
			}
			bx, by := g.LatticePos(q+d[0], r+d[1])
			if segDist2(x, y, ax, ay, bx, by) <= r2 {
				best = t
				return
			}
		}
	}
	visit(q0, r0)
	for _, d := range spec.Neighbours {
		visit(q0+d[0], r0+d[1])
	}
	return best
}
