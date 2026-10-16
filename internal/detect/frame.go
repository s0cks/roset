package detect

import (
	"math"

	"roset/internal/spec"
)

// Frame maps the symbol's normalized coordinates (units; origin at the centre;
// angle clockwise from "up") to image pixels. It is a homography written as an
// affine map followed by a projective correction about the projected centre:
//
//	r = L*q                      (rectified pixel offset)
//	p = C + r / (1 - G.r)        (G = 0 for a purely affine view)
//
// An affine frame comes from second moments of axisymmetric ink (an
// axisymmetric shape's covariance is k*A*A^T under an affine view, so
// Sigma^(1/2) recovers A up to rotation, which the sync dot resolves). A
// projective frame additionally uses the two concentric circles (bullseye ring
// and boundary ring): their image conics determine the projected centre and
// the vanishing line (see FrameFromRings).
type Frame struct {
	Cx, Cy float64
	L      [2][2]float64
	Gx, Gy float64
}

func (f Frame) ImgQ(qx, qy float64) (x, y float64) {
	rx := f.L[0][0]*qx + f.L[0][1]*qy
	ry := f.L[1][0]*qx + f.L[1][1]*qy
	den := 1 - (f.Gx*rx + f.Gy*ry)
	return f.Cx + rx/den, f.Cy + ry/den
}

func (f Frame) ImgPoint(rho, phi float64) (x, y float64) {
	return f.ImgQ(rho*math.Sin(phi), -rho*math.Cos(phi))
}

// ToNorm maps an image point to normalized (rho, phi).
func (f Frame) ToNorm(x, y float64) (rho, phi float64) {
	dx, dy := x-f.Cx, y-f.Cy
	w := 1 + f.Gx*dx + f.Gy*dy
	rx, ry := dx/w, dy/w
	det := f.L[0][0]*f.L[1][1] - f.L[0][1]*f.L[1][0]
	qx := (f.L[1][1]*rx - f.L[0][1]*ry) / det
	qy := (-f.L[1][0]*rx + f.L[0][0]*ry) / det
	return math.Hypot(qx, qy), math.Atan2(qx, -qy)
}

// Unit is the geometric-mean pixels per unit at the centre.
func (f Frame) Unit() float64 {
	return math.Sqrt(math.Abs(f.L[0][0]*f.L[1][1] - f.L[0][1]*f.L[1][0]))
}

func frameFromMoments(cx, cy, sxx, sxy, syy, kappa float64) (Frame, bool) {
	det := sxx*syy - sxy*sxy
	if det <= 1e-9 || kappa <= 0 {
		return Frame{}, false
	}
	s := math.Sqrt(det)
	t := math.Sqrt(sxx + syy + 2*s)
	k := 1 / (t * math.Sqrt(kappa))
	return Frame{Cx: cx, Cy: cy, L: [2][2]float64{{(sxx + s) * k, sxy * k}, {sxy * k, (syy + s) * k}}}, true
}

func finderKappa(bands [][2]float64) float64 {
	var m2, a float64
	for _, b := range bands {
		m2 += math.Pi * (math.Pow(b[1], 4) - math.Pow(b[0], 4)) / 4
		a += math.Pi * (b[1]*b[1] - b[0]*b[0])
	}
	return m2 / a
}

// FrameFromFinder estimates an affine frame from the bullseye's pixels. bands
// are the version's finder bands (the components in f.Labels, zeros ignored).
func FrameFromFinder(lab *Labels, f Finder, bands [][2]float64) (Frame, bool) {
	big := lab.Comp(f.Labels[2])
	in := func(l int32) bool {
		return l != 0 && (l == f.Labels[0] || l == f.Labels[1] || l == f.Labels[2])
	}
	var n, sx, sy, sxx, sxy, syy float64
	for y := big.MinY; y <= big.MaxY; y++ {
		for x := big.MinX; x <= big.MaxX; x++ {
			if in(lab.L[y*lab.W+x]) {
				n++
				sx += float64(x)
				sy += float64(y)
			}
		}
	}
	if n == 0 {
		return Frame{}, false
	}
	cx, cy := sx/n, sy/n
	for y := big.MinY; y <= big.MaxY; y++ {
		for x := big.MinX; x <= big.MaxX; x++ {
			if in(lab.L[y*lab.W+x]) {
				dx, dy := float64(x)-cx, float64(y)-cy
				sxx += dx * dx
				sxy += dx * dy
				syy += dy * dy
			}
		}
	}
	return frameFromMoments(cx, cy, sxx/n, sxy/n, syy/n, finderKappa(bands))
}

// findBoundaryComp finds the boundary ring: a large ink component centred on
// the bullseye whose size is about that of the expected ring.
func findBoundaryComp(lab *Labels, f Finder, rB float64) (int32, bool) {
	u := f.U
	ringArea := 2 * math.Pi * rB * 2 * spec.BoundaryHalf * u * u
	var best int32
	bestArea := 0
	for i, c := range lab.Comps {
		a := float64(c.Area)
		if a < 0.4*ringArea || a > 3*ringArea {
			continue
		}
		// perspective shifts the ring's centroid away from the projected centre,
		// so require only that the bullseye sits well inside the ring's box
		if math.Hypot(c.CX()-f.CX, c.CY()-f.CY) > 0.7*rB*u {
			continue
		}
		bw, bh := float64(c.BW()), float64(c.BH())
		if f.CX < float64(c.MinX)+0.15*bw || f.CX > float64(c.MaxX)-0.15*bw ||
			f.CY < float64(c.MinY)+0.15*bh || f.CY > float64(c.MaxY)-0.15*bh {
			continue
		}
		ext := float64(max(c.BW(), c.BH()))
		if ext < 1.2*rB*u || ext > 3.0*rB*u {
			continue
		}
		if c.Area > bestArea {
			best, bestArea = int32(i+1), c.Area
		}
	}
	return best, best != 0
}

func compPixels(lab *Labels, l int32) (xs, ys []float64) {
	c := lab.Comp(l)
	for y := c.MinY; y <= c.MaxY; y++ {
		for x := c.MinX; x <= c.MaxX; x++ {
			if lab.L[y*lab.W+x] == l {
				xs = append(xs, float64(x))
				ys = append(ys, float64(y))
			}
		}
	}
	return
}

// RefineByBoundary re-estimates an affine frame from the boundary ring, which
// spans the whole symbol and so constrains scale and shear far better than the
// small bullseye. The sync dot is a bump on the ring; wedges around it and at
// its 90-degree rotations are excluded so the retained ring stays symmetric
// (and thus its second moments stay isotropic in the normalized frame).
func RefineByBoundary(lab *Labels, f Frame, fnd Finder, rB float64) (Frame, bool) {
	ring, ok := findBoundaryComp(lab, fnd, rB)
	if !ok {
		return Frame{}, false
	}
	xs, ys := compPixels(lab, ring)
	const nb = 72
	var sum [nb]float64
	var cnt [nb]int
	phis := make([]float64, len(xs))
	for i := range xs {
		rho, phi := f.ToNorm(xs[i], ys[i])
		phis[i] = math.Mod(phi+2*math.Pi, 2*math.Pi)
		b := int(phis[i] / (2 * math.Pi) * nb)
		sum[b] += rho - rB
		cnt[b]++
	}
	bestB, bestV := 0, math.Inf(-1)
	for b := 0; b < nb; b++ {
		if cnt[b] > 0 && sum[b]/float64(cnt[b]) > bestV {
			bestB, bestV = b, sum[b]/float64(cnt[b])
		}
	}
	var n, sx, sy float64
	var kx, ky []float64
	for i := range xs {
		b := int(phis[i] / (2 * math.Pi) * nb)
		skip := false
		for k := 0; k < 4; k++ {
			d := (b - bestB - k*nb/4 + 2*nb) % nb
			if d <= 1 || d >= nb-1 {
				skip = true
			}
		}
		if skip {
			continue
		}
		kx, ky = append(kx, xs[i]), append(ky, ys[i])
		n++
		sx += xs[i]
		sy += ys[i]
	}
	if n < 100 {
		return Frame{}, false
	}
	cx, cy := sx/n, sy/n
	var sxx, sxy, syy float64
	for i := range kx {
		dx, dy := kx[i]-cx, ky[i]-cy
		sxx += dx * dx
		sxy += dx * dy
		syy += dy * dy
	}
	ro, ri := rB+spec.BoundaryHalf, rB-spec.BoundaryHalf
	return frameFromMoments(cx, cy, sxx/n, sxy/n, syy/n, (ro*ro+ri*ri)/4)
}
