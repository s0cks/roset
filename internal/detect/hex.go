package detect

import (
	"math"
	"sort"
)

// HexCand is a component whose outline is (close to) a convex hexagon: the
// symbol's solid border. V holds the six corners in hull order.
type HexCand struct {
	V       [6][2]float64
	Area    float64
	Quality float64 // hexagon area / convex hull area (1 = perfect)
}

type pt struct{ x, y float64 }

func cross(o, a, b pt) float64 { return (a.x-o.x)*(b.y-o.y) - (a.y-o.y)*(b.x-o.x) }

// convexHull is Andrew's monotone chain; the result is counter-clockwise in
// mathematical axes (so clockwise on screen with y down).
func convexHull(p []pt) []pt {
	sort.Slice(p, func(i, j int) bool {
		if p[i].x != p[j].x {
			return p[i].x < p[j].x
		}
		return p[i].y < p[j].y
	})
	var h []pt
	for _, q := range p {
		for len(h) >= 2 && cross(h[len(h)-2], h[len(h)-1], q) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, q)
	}
	lo := len(h) + 1
	for i := len(p) - 2; i >= 0; i-- {
		q := p[i]
		for len(h) >= lo && cross(h[len(h)-2], h[len(h)-1], q) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, q)
	}
	return h[:len(h)-1]
}

func polyArea(p []pt) float64 {
	var a float64
	for i := range p {
		j := (i + 1) % len(p)
		a += p[i].x*p[j].y - p[j].x*p[i].y
	}
	return math.Abs(a) / 2
}

// simplify removes hull vertices, always the one whose removal loses the least
// area, until n remain. It returns the indices of the survivors.
func simplify(h []pt, n int) []int {
	idx := make([]int, len(h))
	for i := range idx {
		idx[i] = i
	}
	for len(idx) > n {
		best, bi := math.Inf(1), 0
		for i := range idx {
			a, b, c := h[idx[(i+len(idx)-1)%len(idx)]], h[idx[i]], h[idx[(i+1)%len(idx)]]
			if ar := math.Abs(cross(a, b, c)); ar < best {
				best, bi = ar, i
			}
		}
		idx = append(idx[:bi], idx[bi+1:]...)
	}
	return idx
}

type line struct{ px, py, dx, dy float64 } // point + unit direction

// fitLine is a total-least-squares line through p.
func fitLine(p []pt) (line, bool) {
	if len(p) < 2 {
		return line{}, false
	}
	var cx, cy float64
	for _, q := range p {
		cx += q.x
		cy += q.y
	}
	n := float64(len(p))
	cx, cy = cx/n, cy/n
	var sxx, sxy, syy float64
	for _, q := range p {
		dx, dy := q.x-cx, q.y-cy
		sxx, sxy, syy = sxx+dx*dx, sxy+dx*dy, syy+dy*dy
	}
	th := 0.5 * math.Atan2(2*sxy, sxx-syy)
	return line{cx, cy, math.Cos(th), math.Sin(th)}, true
}

func intersect(a, b line) (pt, bool) {
	den := a.dx*b.dy - a.dy*b.dx
	if math.Abs(den) < 1e-9 {
		return pt{}, false
	}
	t := ((b.px-a.px)*b.dy - (b.py-a.py)*b.dx) / den
	return pt{a.px + t*a.dx, a.py + t*a.dy}, true
}

// FindHexagons returns the components (largest hull first) whose convex hull is
// a hexagon to within a few percent. Corners are refined by fitting a line to
// each side of the hull and intersecting neighbours, which removes the inward
// bias of rounded or blurred corners.
func FindHexagons(lab *Labels, minSide, maxOut int) []HexCand {
	type cand struct {
		l    int32
		area float64
	}
	var cs []cand
	for i, c := range lab.Comps {
		if c.BW() < minSide || c.BH() < minSide {
			continue
		}
		cs = append(cs, cand{int32(i + 1), float64(c.BW() * c.BH())})
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].area > cs[j].area })
	var out []HexCand
	for _, c := range cs {
		if len(out) >= maxOut || len(out) >= 6 && c.area < 0.02*cs[0].area {
			break
		}
		if h, ok := fitHexagon(lab, c.l); ok {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Area > out[j].Area })
	return out
}

func fitHexagon(lab *Labels, l int32) (HexCand, bool) {
	c := lab.Comp(l)
	var pts []pt
	for y := c.MinY; y <= c.MaxY; y++ {
		row := lab.L[y*lab.W : (y+1)*lab.W]
		lo, hi := -1, -1
		for x := c.MinX; x <= c.MaxX; x++ {
			if row[x] == l {
				if lo < 0 {
					lo = x
				}
				hi = x
			}
		}
		if lo >= 0 {
			pts = append(pts, pt{float64(lo) - 0.5, float64(y)}, pt{float64(hi) + 0.5, float64(y)})
		}
	}
	if len(pts) < 12 {
		return HexCand{}, false
	}
	hull := convexHull(pts)
	if len(hull) < 6 {
		return HexCand{}, false
	}
	idx := simplify(hull, 6)
	var raw [6]pt
	for i, k := range idx {
		raw[i] = hull[k]
	}
	hullArea := polyArea(hull)
	q := polyArea(raw[:]) / hullArea
	if q < 0.95 {
		return HexCand{}, false
	}
	// refine: fit each side from the hull points strictly between its corners
	var lines [6]line
	for i := 0; i < 6; i++ {
		a, b := idx[i], idx[(i+1)%6]
		var side []pt
		for k := a; ; k = (k + 1) % len(hull) {
			side = append(side, hull[k])
			if k == b {
				break
			}
		}
		trim := len(side) / 6
		if len(side)-2*trim >= 2 {
			side = side[trim : len(side)-trim]
		}
		ln, ok := fitLine(side)
		if !ok {
			return HexCand{}, false
		}
		lines[i] = ln
	}
	var out HexCand
	for i := 0; i < 6; i++ {
		v, ok := intersect(lines[(i+5)%6], lines[i])
		if !ok || math.Hypot(v.x-raw[i].x, v.y-raw[i].y) > 0.1*math.Sqrt(hullArea) {
			v = raw[i]
		}
		out.V[i] = [2]float64{v.x, v.y}
	}
	out.Area, out.Quality = hullArea, q
	return out, true
}
