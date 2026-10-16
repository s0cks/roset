// Package layout is the single source of truth for what is drawn where.
// Dark(x,y) answers "is this point ink?" for both the PNG rasterizer and tests;
// the SVG writer emits the equivalent vector primitives.
package layout

import (
	"fmt"
	"math"

	"roset/internal/codec"
	"roset/internal/spec"
)

type Style int

const (
	StyleDots Style = iota // every 1-cell is its own dot
	StyleRuns              // consecutive 1-cells merge into one capsule-shaped stroke
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
	Cells *codec.Cells
	Style Style
}

func New(v spec.Version, c *codec.Cells, s Style) *Layout { return &Layout{V: v, Cells: c, Style: s} }

// Run is a circular run of consecutive set slots.
type Run struct{ Start, Len int }

// Runs returns the circular runs of true bits. An all-true ring yields one run of Len==len(bits).
func Runs(bits []bool) []Run {
	n := len(bits)
	z := -1
	for i, b := range bits {
		if !b {
			z = i
			break
		}
	}
	if z < 0 {
		return []Run{{0, n}}
	}
	var out []Run
	cur := Run{Start: -1}
	for k := 1; k <= n; k++ {
		i := (z + k) % n
		if bits[i] {
			if cur.Start < 0 {
				cur = Run{Start: i}
			}
			cur.Len++
		} else if cur.Start >= 0 {
			out = append(out, cur)
			cur.Start = -1
			cur.Len = 0
		}
	}
	return out
}

// Dark reports whether point (x,y) in units (origin center, y down) is inked.
func (l *Layout) Dark(x, y float64) bool {
	r := math.Hypot(x, y)
	v := l.V
	if r > v.OuterRadius()+0.01 {
		return false
	}
	if r < v.LogoR {
		return false // clear logo zone; the logo itself is composited by the renderer
	}
	// finder bullseye (plain: disc + 2 rings; logo: 2 rings)
	if r <= v.FinderOuter() {
		for _, b := range v.FinderBands() {
			if r >= b[0] && r <= b[1] {
				return true
			}
		}
		return false
	}
	// sync dot (top, outside the boundary ring)
	if dy := y + v.SyncRadius(); x*x+dy*dy <= spec.SyncDotRadius*spec.SyncDotRadius {
		return true
	}
	// boundary ring
	if math.Abs(r-v.BoundaryRadius()) <= spec.BoundaryHalf {
		return true
	}
	if r < v.HeaderRingRadius(0)-spec.DotRadius || r > v.DataRingRadius(v.Rings-1)+spec.DotRadius {
		return false
	}
	theta := math.Atan2(x, -y)
	if theta < 0 {
		theta += 2 * math.Pi
	}
	runs := l.Style == StyleRuns
	if r < v.DataRingRadius(0)-0.5 {
		j := int(math.Floor(r - v.HeaderRingRadius(0) + 0.5))
		if j >= 0 && j < spec.HeaderRings {
			return cellDark(l.Cells.Header[j], v.HeaderRingRadius(j), r, theta, runs)
		}
		return false
	}
	k := int(math.Floor(r - v.DataRingRadius(0) + 0.5))
	if k >= 0 && k < v.Rings {
		return cellDark(l.Cells.Data[k], v.DataRingRadius(k), r, theta, runs)
	}
	return false
}

func cellDark(bits []bool, rc, r, theta float64, runs bool) bool {
	w := spec.DotRadius
	if math.Abs(r-rc) > w {
		return false
	}
	n := len(bits)
	t := theta * float64(n) / (2 * math.Pi)
	i0 := int(math.Floor(t))
	i0 = ((i0 % n) + n) % n
	i1 := (i0 + 1) % n
	if runs && bits[i0] && bits[i1] {
		return true
	}
	px, py := r*math.Sin(theta), -r*math.Cos(theta)
	for _, idx := range [2]int{i0, i1} {
		if !bits[idx] {
			continue
		}
		a := 2 * math.Pi * float64(idx) / float64(n)
		dx, dy := px-rc*math.Sin(a), py+rc*math.Cos(a)
		if dx*dx+dy*dy <= w*w {
			return true
		}
	}
	return false
}
