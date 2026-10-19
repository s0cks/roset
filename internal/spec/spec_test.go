package spec

import (
	"math"
	"testing"
)

func TestPetalsTile(t *testing.T) {
	for _, k := range []int{4, 5, 6, 7} {
		g := Geo(k)
		if len(g.Cells) != 6*g.PetalN {
			t.Fatalf("K=%d: %d cells, want %d", k, len(g.Cells), 6*g.PetalN)
		}
		owner := map[[2]int]int{}
		for q := -k; q <= k; q++ {
			for r := -k; r <= k; r++ {
				if hexDist(q, r) <= k {
					owner[[2]int{q, r}] = -1 // centre patch
				}
			}
		}
		for _, c := range g.Cells {
			key := [2]int{c.Q, c.R}
			if p, dup := owner[key]; dup {
				t.Fatalf("K=%d: cell %v claimed by petal %d and %d", k, key, p, c.Petal)
			}
			owner[key] = c.Petal
		}
		// no gaps: every site nearer than sqrt(3)|S0| - K must belong to some patch
		sx, sy := lattice(k+1, k)
		lim := math.Sqrt(3)*math.Hypot(sx, sy) - float64(k)
		span := int(lim) + 2
		for q := -span; q <= span; q++ {
			for r := -span; r <= span; r++ {
				x, y := lattice(q, r)
				if math.Hypot(x, y) < lim {
					if _, ok := owner[[2]int{q, r}]; !ok {
						t.Fatalf("K=%d: gap at (%d,%d)", k, q, r)
					}
				}
			}
		}
	}
}

func TestGeometrySane(t *testing.T) {
	for _, k := range []int{4, 5, 6, 7} {
		g := Geo(k)
		// cells keep clear of the inner hexagon and the header ring
		for _, c := range g.Cells {
			h := HexNorm(c.X, c.Y)
			if h < g.AInner+InnerStroke/2+DotRadius+0.2 {
				t.Errorf("K=%d: cell (%d,%d) too close to the inner hexagon (h=%.2f)", k, c.Q, c.R, h)
			}
			if h > g.AHeader-DotRadius*2 {
				t.Errorf("K=%d: cell (%d,%d) too close to the header ring (h=%.2f)", k, c.Q, c.R, h)
			}
		}
		// header slot pitch
		x0, y0 := g.HeaderPos(0)
		x1, y1 := g.HeaderPos(1)
		if p := math.Hypot(x1-x0, y1-y0); p < MinSlotPitch-1e-9 {
			t.Errorf("K=%d: header slot pitch %.3f", k, p)
		}
		// round trip of the lattice lookup
		for i, c := range g.Cells {
			q, r := g.NearestLattice(c.X, c.Y)
			if q != c.Q || r != c.R || g.CellAt(q, r) != i {
				t.Fatalf("K=%d: lattice round trip failed for cell %d", k, i)
			}
		}
	}
}

func TestVersionTable(t *testing.T) {
	if len(Versions) == 0 || len(Versions) > 15 {
		t.Fatalf("%d versions; the header has room for 15", len(Versions))
	}
	for _, v := range Versions {
		if v.NSym < MinParity || v.N > 255 {
			t.Errorf("version %d: bad RS(%d,%d)", v.ID, v.N, v.Payload)
		}
		// losing one whole petal must stay within the correction budget
		if lost := (v.N + 5) / 6; lost > v.NSym/2 {
			t.Errorf("version %d loses %d bytes with a petal; RS corrects only %d", v.ID, lost, v.NSym/2)
		}
		if v.N > NumPetals*v.BytesPerPetal() {
			t.Errorf("version %d: codeword does not fit", v.ID)
		}
	}
}
