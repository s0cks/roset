package detect

import "math"

type mat3 [3][3]float64

func (a mat3) mul(b mat3) (o mat3) {
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				o[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return
}

func (a mat3) T() (o mat3) {
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			o[i][j] = a[j][i]
		}
	}
	return
}

func (a mat3) vec(v [3]float64) (o [3]float64) {
	for i := 0; i < 3; i++ {
		o[i] = a[i][0]*v[0] + a[i][1]*v[1] + a[i][2]*v[2]
	}
	return
}

func (a mat3) inv() (mat3, bool) {
	c := mat3{
		{a[1][1]*a[2][2] - a[1][2]*a[2][1], a[0][2]*a[2][1] - a[0][1]*a[2][2], a[0][1]*a[1][2] - a[0][2]*a[1][1]},
		{a[1][2]*a[2][0] - a[1][0]*a[2][2], a[0][0]*a[2][2] - a[0][2]*a[2][0], a[0][2]*a[1][0] - a[0][0]*a[1][2]},
		{a[1][0]*a[2][1] - a[1][1]*a[2][0], a[0][1]*a[2][0] - a[0][0]*a[2][1], a[0][0]*a[1][1] - a[0][1]*a[1][0]},
	}
	det := a[0][0]*c[0][0] + a[0][1]*c[1][0] + a[0][2]*c[2][0]
	if math.Abs(det) < 1e-300 {
		return mat3{}, false
	}
	for i := range c {
		for j := range c[i] {
			c[i][j] /= det
		}
	}
	return c, true
}

// solveLinear solves a*x = b by Gaussian elimination with partial pivoting.
func solveLinear(a [][]float64, b []float64) ([]float64, bool) {
	n := len(b)
	for c := 0; c < n; c++ {
		p := c
		for r := c + 1; r < n; r++ {
			if math.Abs(a[r][c]) > math.Abs(a[p][c]) {
				p = r
			}
		}
		if math.Abs(a[p][c]) < 1e-14 {
			return nil, false
		}
		a[c], a[p] = a[p], a[c]
		b[c], b[p] = b[p], b[c]
		for r := c + 1; r < n; r++ {
			f := a[r][c] / a[c][c]
			for k := c; k < n; k++ {
				a[r][k] -= f * a[c][k]
			}
			b[r] -= f * b[c]
		}
	}
	x := make([]float64, n)
	for r := n - 1; r >= 0; r-- {
		s := b[r]
		for k := r + 1; k < n; k++ {
			s -= a[r][k] * x[k]
		}
		x[r] = s / a[r][r]
	}
	return x, true
}

// conicFit is the algebraic conic A x^2 + B xy + C y^2 + D x + E y = 1 in
// coordinates normalized by (cx, cy, s).
type conicFit struct {
	cx, cy, s     float64
	A, B, C, D, E float64
}

func fitConic(xs, ys []float64) (conicFit, bool) {
	n := float64(len(xs))
	if n < 20 {
		return conicFit{}, false
	}
	var cx, cy float64
	for i := range xs {
		cx += xs[i]
		cy += ys[i]
	}
	cx /= n
	cy /= n
	var r2 float64
	for i := range xs {
		r2 += (xs[i]-cx)*(xs[i]-cx) + (ys[i]-cy)*(ys[i]-cy)
	}
	s := math.Sqrt(r2 / n)
	if s < 1 {
		return conicFit{}, false
	}
	ata := make([][]float64, 5)
	for i := range ata {
		ata[i] = make([]float64, 5)
	}
	atb := make([]float64, 5)
	for i := range xs {
		u, v := (xs[i]-cx)/s, (ys[i]-cy)/s
		row := [5]float64{u * u, u * v, v * v, u, v}
		for a := 0; a < 5; a++ {
			atb[a] += row[a]
			for b := 0; b < 5; b++ {
				ata[a][b] += row[a] * row[b]
			}
		}
	}
	sol, ok := solveLinear(ata, atb)
	if !ok {
		return conicFit{}, false
	}
	return conicFit{cx, cy, s, sol[0], sol[1], sol[2], sol[3], sol[4]}, true
}

func (c conicFit) residual(x, y float64) float64 {
	u, v := (x-c.cx)/c.s, (y-c.cy)/c.s
	return c.A*u*u + c.B*u*v + c.C*v*v + c.D*u + c.E*v - 1
}

// matrixIn returns the conic as a symmetric 3x3 in coordinates x_n = (x - c0)/s0.
func (c conicFit) matrixIn(c0x, c0y, s0 float64) mat3 {
	m := mat3{{c.A, c.B / 2, c.D / 2}, {c.B / 2, c.C, c.E / 2}, {c.D / 2, c.E / 2, -1}}
	// local <- n:  x_l = (s0 x_n + c0 - c)/s
	t := mat3{{s0 / c.s, 0, (c0x - c.cx) / c.s}, {0, s0 / c.s, (c0y - c.cy) / c.s}, {0, 0, 1}}
	return t.T().mul(m).mul(t)
}

func fitTrimmed(xs, ys []float64, thresh float64) (conicFit, []float64, []float64, bool) {
	px, py := xs, ys
	var cf conicFit
	for it := 0; it < 4; it++ {
		var ok bool
		cf, ok = fitConic(px, py)
		if !ok {
			return conicFit{}, nil, nil, false
		}
		var nx, ny []float64
		for i := range xs {
			if math.Abs(cf.residual(xs[i], ys[i])) <= thresh {
				nx, ny = append(nx, xs[i]), append(ny, ys[i])
			}
		}
		if len(nx) < 100 {
			return conicFit{}, nil, nil, false
		}
		px, py = nx, ny
	}
	cf, ok := fitConic(px, py)
	return cf, px, py, ok
}

// FrameFromRings estimates a projective frame from two concentric circles: the
// bullseye's outer ring (radius 5) and the boundary ring (radius rB).
//
// For concentric circles with radii a, b the image conics satisfy
// C2^-1 C1 ~ H diag(1,1,(a/b)^2) H^-1, so the eigenvector of the simple
// eigenvalue is the *projected centre* (the dominant one: power iteration), and
// the pole-polar relation l = C1 * centre gives the vanishing line. Dividing by
// that line rectifies the view to an affine one, in which the boundary ring is
// an exact ellipse centred on the origin; its shape gives L.
func FrameFromRings(lab *Labels, fnd Finder, rB float64) (Frame, bool) {
	x2, y2 := compPixels(lab, fnd.Labels[2])
	ring, ok := findBoundaryComp(lab, fnd, rB)
	if !ok {
		return Frame{}, false
	}
	bx, by := compPixels(lab, ring)
	cf2, ok := fitConic(x2, y2)
	if !ok {
		return Frame{}, false
	}
	cf1, kx, ky, ok := fitTrimmed(bx, by, 1.4/rB)
	if !ok {
		return Frame{}, false
	}
	c0x, c0y, s0 := fnd.CX, fnd.CY, rB*fnd.U
	c1, c2 := cf1.matrixIn(c0x, c0y, s0), cf2.matrixIn(c0x, c0y, s0)
	c2inv, ok := c2.inv()
	if !ok {
		return Frame{}, false
	}
	m := c2inv.mul(c1)
	v := [3]float64{0, 0, 1}
	for i := 0; i < 60; i++ {
		v = m.vec(v)
		mx := math.Max(math.Abs(v[0]), math.Max(math.Abs(v[1]), math.Abs(v[2])))
		if mx == 0 {
			return Frame{}, false
		}
		for k := range v {
			v[k] /= mx
		}
	}
	if math.Abs(v[2]) < 1e-9 {
		return Frame{}, false
	}
	pcx, pcy := v[0]/v[2], v[1]/v[2]
	if math.Hypot(pcx, pcy) > 0.35 {
		return Frame{}, false
	}
	l := c1.vec([3]float64{pcx, pcy, 1})
	lc := l[0]*pcx + l[1]*pcy + l[2]
	if math.Abs(lc) < 1e-12 {
		return Frame{}, false
	}
	fr := Frame{Cx: c0x + s0*pcx, Cy: c0y + s0*pcy, Gx: l[0] / lc / s0, Gy: l[1] / lc / s0}

	// rectify the ring pixels and fit the centred ellipse A x^2 + B xy + C y^2 = 1
	rect := func() (rx, ry []float64) {
		for i := range kx {
			dx, dy := kx[i]-fr.Cx, ky[i]-fr.Cy
			w := 1 + fr.Gx*dx + fr.Gy*dy
			if w < 0.2 {
				return nil, nil
			}
			rx, ry = append(rx, dx/w), append(ry, dy/w)
		}
		return
	}
	var a, b, c float64
	for it := 0; it < 2; it++ {
		rx, ry := rect()
		if len(rx) < 100 {
			return Frame{}, false
		}
		ata := [][]float64{{0, 0, 0}, {0, 0, 0}, {0, 0, 0}}
		atb := []float64{0, 0, 0}
		for i := range rx {
			row := [3]float64{rx[i] * rx[i], rx[i] * ry[i], ry[i] * ry[i]}
			for p := 0; p < 3; p++ {
				atb[p] += row[p]
				for q := 0; q < 3; q++ {
					ata[p][q] += row[p] * row[q]
				}
			}
		}
		sol, ok := solveLinear(ata, atb)
		if !ok {
			return Frame{}, false
		}
		a, b, c = sol[0], sol[1], sol[2]
		if it == 0 { // drop radial outliers (sync-dot bump) and refit once
			var nx, ny []float64
			for i := range rx {
				f := a*rx[i]*rx[i] + b*rx[i]*ry[i] + c*ry[i]*ry[i] - 1
				if math.Abs(f) <= 1.4/rB {
					nx, ny = append(nx, kx[i]), append(ny, ky[i])
				}
			}
			kx, ky = nx, ny
		}
	}
	det := a*c - b*b/4
	if a <= 0 || det <= 0 {
		return Frame{}, false
	}
	// L = Q^(-1/2) / rB where Q = [[a, b/2],[b/2, c]]
	qi := [2][2]float64{{c / det, -b / 2 / det}, {-b / 2 / det, a / det}}
	s := math.Sqrt(qi[0][0]*qi[1][1] - qi[0][1]*qi[1][0])
	t := math.Sqrt(qi[0][0] + qi[1][1] + 2*s)
	k := 1 / (t * rB)
	fr.L = [2][2]float64{{(qi[0][0] + s) * k, qi[0][1] * k}, {qi[1][0] * k, (qi[1][1] + s) * k}}
	if math.Hypot(fr.Gx, fr.Gy)*rB*fr.Unit() > 0.7 {
		return Frame{}, false
	}
	return fr, true
}
