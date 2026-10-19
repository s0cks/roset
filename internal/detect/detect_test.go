package detect

import (
	"math"
	"math/rand"
	"testing"
)

func TestHomographyRecovers(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 50; trial++ {
		h := Mat3{
			{1 + rng.Float64()*40, rng.Float64()*10 - 5, 300 + rng.Float64()*100},
			{rng.Float64()*10 - 5, 1 + rng.Float64()*40, 300 + rng.Float64()*100},
			{rng.Float64() * 0.004, rng.Float64() * 0.004, 1},
		}
		var src, dst [][2]float64
		for j := 0; j < 6; j++ {
			a := float64(j) * math.Pi / 3
			x, y := 15*math.Cos(a), 15*math.Sin(a)
			px, py := h.Apply(x, y)
			src, dst = append(src, [2]float64{x, y}), append(dst, [2]float64{px, py})
		}
		got, ok := Homography(src, dst)
		if !ok {
			t.Fatal("homography failed")
		}
		for i := 0; i < 20; i++ {
			x, y := rng.Float64()*30-15, rng.Float64()*30-15
			wx, wy := h.Apply(x, y)
			gx, gy := got.Apply(x, y)
			if math.Hypot(wx-gx, wy-gy) > 1e-6*math.Max(1, math.Hypot(wx, wy)) {
				t.Fatalf("trial %d: (%g,%g) -> (%g,%g), want (%g,%g)", trial, x, y, gx, gy, wx, wy)
			}
		}
		inv, _ := got.Inv()
		x, y := got.Apply(3, -4)
		bx, by := inv.Apply(x, y)
		if math.Hypot(bx-3, by+4) > 1e-6 {
			t.Fatalf("inverse round trip off: (%g,%g)", bx, by)
		}
	}
}

// A thick hexagonal outline, viewed through a homography, must come back with its six corners.
func TestFindHexagonCorners(t *testing.T) {
	const W, H = 600, 600
	h := Mat3{{18, 4, 300}, {-3, 16, 290}, {0.0004, 0.0002, 1}}
	r := 15.0
	var want [6][2]float64
	for j := range want {
		a := float64(j) * math.Pi / 3
		want[j][0], want[j][1] = h.Apply(r*math.Cos(a), r*math.Sin(a))
	}
	inv, _ := h.Inv()
	hexNorm := func(x, y float64) float64 {
		best := math.Inf(-1)
		for i := 0; i < 6; i++ {
			a := -math.Pi/2 + float64(i)*math.Pi/3
			best = math.Max(best, x*math.Cos(a)+y*math.Sin(a))
		}
		return best
	}
	outer := r * math.Sqrt(3) / 2
	b := &Bin{W: W, H: H, Pix: make([]uint8, W*H)}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			u, v := inv.Apply(float64(x), float64(y))
			if n := hexNorm(u, v); n <= outer && n >= outer-0.8 {
				b.Pix[y*W+x] = 1
			}
		}
	}
	lab := Label(b)
	cands := FindHexagons(lab, 40, 4)
	if len(cands) == 0 {
		t.Fatal("no hexagon found")
	}
	c := cands[0]
	// corners come back in some rotation; each wanted corner must match one found corner
	for _, w := range want {
		best := math.Inf(1)
		for _, g := range c.V {
			best = math.Min(best, math.Hypot(w[0]-g[0], w[1]-g[1]))
		}
		if best > 1.0 {
			t.Errorf("corner (%.1f,%.1f) not recovered (nearest %.2f px away)", w[0], w[1], best)
		}
	}
}

func TestFindHexagonsRejectsBlobs(t *testing.T) {
	b := &Bin{W: 200, H: 200, Pix: make([]uint8, 200*200)}
	for y := 50; y < 150; y++ { // a filled square is not a hexagon
		for x := 50; x < 150; x++ {
			b.Pix[y*200+x] = 1
		}
	}
	if got := FindHexagons(Label(b), 20, 4); len(got) != 0 {
		t.Fatalf("a square was accepted as a hexagon (quality %.3f)", got[0].Quality)
	}
}
