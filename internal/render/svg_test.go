package render

import (
	"crypto/sha256"
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"roset/internal/codec"
	"roset/internal/layout"
	"roset/internal/spec"
)

// Independent check that the SVG describes the same picture as layout.Dark
// (which the PNG rasterizer uses): parse the SVG primitives and compare
// coverage at random points. Only points within a hair of an edge may differ.
type prim struct {
	kind           string // "disc", "ring", "arc"
	cx, cy, r, sw  float64
	x0, y0, x1, y1 float64 // arc endpoints
	rc             float64
	large          bool
}

var (
	circleRe = regexp.MustCompile(`<circle ([^>]*)/>`)
	attrRe   = regexp.MustCompile(`([\w-]+)="([^"]*)"`)
	pathRe   = regexp.MustCompile(`<path d="([^"]*)"[^>]*stroke-width="([^"]*)"`)
	arcRe    = regexp.MustCompile(`M([-\d.]+) ([-\d.]+)A([-\d.]+) [-\d.]+ 0 (\d) 1 ([-\d.]+) ([-\d.]+)`)
)

func f(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }

func parse(svg string) []prim {
	var out []prim
	// dots inside <g fill=...> inherit fill, so treat any circle with fill != none as disc
	for _, m := range circleRe.FindAllStringSubmatch(svg, -1) {
		a := map[string]string{}
		for _, kv := range attrRe.FindAllStringSubmatch(m[1], -1) {
			a[kv[1]] = kv[2]
		}
		p := prim{cx: f(a["cx"]), cy: f(a["cy"]), r: f(a["r"])}
		if a["fill"] == "none" {
			p.kind, p.sw = "ring", f(a["stroke-width"])
		} else {
			p.kind = "disc"
		}
		out = append(out, p)
	}
	for _, m := range pathRe.FindAllStringSubmatch(svg, -1) {
		sw := f(m[2])
		for _, a := range arcRe.FindAllStringSubmatch(m[1], -1) {
			out = append(out, prim{kind: "arc", x0: f(a[1]), y0: f(a[2]), rc: f(a[3]), large: a[4] == "1", x1: f(a[5]), y1: f(a[6]), sw: sw})
		}
	}
	return out
}

func ang(x, y float64) float64 { // clockwise from up, [0,2pi)
	a := math.Atan2(x, -y)
	if a < 0 {
		a += 2 * math.Pi
	}
	return a
}

func (p prim) hit(x, y float64) bool {
	switch p.kind {
	case "disc":
		return math.Hypot(x-p.cx, y-p.cy) <= p.r
	case "ring":
		return math.Abs(math.Hypot(x-p.cx, y-p.cy)-p.r) <= p.sw/2
	}
	r := math.Hypot(x, y)
	a0, a1 := ang(p.x0, p.y0), ang(p.x1, p.y1)
	span := math.Mod(a1-a0+2*math.Pi, 2*math.Pi) // clockwise sweep
	if (span > math.Pi) != p.large {
		return false // would be the other arc; flag mismatch shows up as coverage diff
	}
	if math.Mod(ang(x, y)-a0+2*math.Pi, 2*math.Pi) <= span {
		return math.Abs(r-p.rc) <= p.sw/2
	}
	return math.Hypot(x-p.x0, y-p.y0) <= p.sw/2 || math.Hypot(x-p.x1, y-p.y1) <= p.sw/2
}

func TestSVGMatchesLayout(t *testing.T) {
	for _, ver := range spec.Versions {
		for _, st := range []layout.Style{layout.StyleDots, layout.StyleRuns} {
			h := sha256.Sum256([]byte{byte(ver.ID), byte(st)})
			payload := append([]byte(nil), h[:]...)
			mode := codec.ModeRawSHA256
			if ver.ID == 2 {
				payload, mode = append(payload, []byte("a tag that needs the bigger symbol....")...), codec.ModeSHA256Tag
			}
			c, err := codec.Encode(ver, mode, payload)
			if err != nil {
				t.Fatal(err)
			}
			l := layout.New(ver, c, st)
			prims := parse(SVG(l, "#000", "#fff", nil))
			if len(prims) < 50 {
				t.Fatalf("parsed only %d primitives", len(prims))
			}
			rng := rand.New(rand.NewSource(7))
			R := ver.OuterRadius()
			const N = 300000
			diff, ink := 0, 0
			for i := 0; i < N; i++ {
				x, y := (rng.Float64()*2-1)*R, (rng.Float64()*2-1)*R
				want := l.Dark(x, y)
				got := false
				for _, p := range prims {
					if p.hit(x, y) {
						got = true
						break
					}
				}
				if want {
					ink++
				}
				if got != want {
					diff++
				}
			}
			t.Logf("v%d style=%d: %d prims, ink samples %d, disagreements %d", ver.ID, st, len(prims), ink, diff)
			if float64(diff) > 0.0005*float64(N) {
				t.Errorf("v%d style=%d: SVG disagrees with layout at %d/%d points", ver.ID, st, diff, N)
			}
		}
	}
	_ = strings.TrimSpace
}
