package render

import (
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"roset/internal/layout"
	"roset/internal/spec"
)

type circ struct{ x, y, r float64 }
type seg struct{ x0, y0, x1, y1 float64 }

var (
	reCircle = regexp.MustCompile(`<circle cx="([-\d.]+)" cy="([-\d.]+)" r="([-\d.]+)"/>`)
	reSeg    = regexp.MustCompile(`M([-\d.]+) ([-\d.]+)L([-\d.]+) ([-\d.]+)`)
)

func f(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }

// The SVG's circles and strokes must cover exactly the points the layout model calls inked
// (outside the solid hexagons, which are polygons).
func TestSVGMatchesLayout(t *testing.T) {
	for _, id := range []int{1, 6, 9} {
		for _, st := range []layout.Style{layout.StyleDots, layout.StyleRuns} {
			l := sample(t, id, st)
			svg := SVG(l, "#16161c", "#faf8f0", nil)
			var cs []circ
			var ss []seg
			for _, m := range reCircle.FindAllStringSubmatch(svg, -1) {
				cs = append(cs, circ{f(m[1]), f(m[2]), f(m[3])})
			}
			for _, m := range reSeg.FindAllStringSubmatch(svg, -1) {
				ss = append(ss, seg{f(m[1]), f(m[2]), f(m[3]), f(m[4])})
			}
			if len(cs) == 0 {
				t.Fatalf("v%d: no circles in SVG", id)
			}
			if st == layout.StyleRuns && len(ss) == 0 {
				t.Fatalf("v%d: runs style produced no strokes", id)
			}
			if st == layout.StyleDots && len(ss) != 0 {
				t.Fatalf("v%d: dots style produced strokes", id)
			}
			covered := func(x, y float64) bool {
				for _, c := range cs {
					if (x-c.x)*(x-c.x)+(y-c.y)*(y-c.y) <= c.r*c.r {
						return true
					}
				}
				r := spec.DotRadius
				for _, s := range ss {
					dx, dy := s.x1-s.x0, s.y1-s.y0
					tt := math.Max(0, math.Min(1, ((x-s.x0)*dx+(y-s.y0)*dy)/(dx*dx+dy*dy)))
					ex, ey := x-(s.x0+tt*dx), y-(s.y0+tt*dy)
					if ex*ex+ey*ey <= r*r {
						return true
					}
				}
				return false
			}
			rng := rand.New(rand.NewSource(int64(id)))
			g := l.G
			bad, inked := 0, 0
			for i := 0; i < 60000; i++ {
				x, y := (rng.Float64()*2-1)*g.Half, (rng.Float64()*2-1)*g.Half
				h := spec.HexNorm(x, y)
				// skip the solid polygons and the boundaries of the dot regions
				if h < g.AInner+spec.InnerStroke/2+0.05 || h > g.ABorder-spec.BorderStroke/2-0.05 {
					continue
				}
				want := l.Ink(x, y) > 0
				if want {
					inked++
				}
				if covered(x, y) != want {
					bad++
				}
			}
			if inked < 1000 {
				t.Fatalf("v%d: only %d inked sample points; test is not exercising anything", id, inked)
			}
			// SVG coordinates are printed to 3 decimals, so a handful of edge samples may flip
			if bad > inked/300 {
				t.Errorf("v%d style=%d: %d of %d inked sample points disagree between SVG and layout", id, st, bad, inked)
			}
		}
	}
}

func TestSVGLevelColours(t *testing.T) {
	l := sample(t, 6, layout.StyleDots)
	svg := SVG(l, "#000000", "#ffffff", nil)
	for _, want := range []string{`fill="#555555"`, `fill="#aaaaaa"`, `fill="#000000"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("4-level SVG lacks a group with %s", want)
		}
	}
}
