package detect

import "math"

// Mat3 is a 3x3 matrix; as a homography it maps (x,y,1) column vectors.
type Mat3 [3][3]float64

func (a Mat3) Mul(b Mat3) (o Mat3) {
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				o[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return
}

// Apply maps a point through the homography.
func (a Mat3) Apply(x, y float64) (float64, float64) {
	w := a[2][0]*x + a[2][1]*y + a[2][2]
	return (a[0][0]*x + a[0][1]*y + a[0][2]) / w, (a[1][0]*x + a[1][1]*y + a[1][2]) / w
}

func (a Mat3) Inv() (Mat3, bool) {
	c := Mat3{
		{a[1][1]*a[2][2] - a[1][2]*a[2][1], a[0][2]*a[2][1] - a[0][1]*a[2][2], a[0][1]*a[1][2] - a[0][2]*a[1][1]},
		{a[1][2]*a[2][0] - a[1][0]*a[2][2], a[0][0]*a[2][2] - a[0][2]*a[2][0], a[0][2]*a[1][0] - a[0][0]*a[1][2]},
		{a[1][0]*a[2][1] - a[1][1]*a[2][0], a[0][1]*a[2][0] - a[0][0]*a[2][1], a[0][0]*a[1][1] - a[0][1]*a[1][0]},
	}
	det := a[0][0]*c[0][0] + a[0][1]*c[1][0] + a[0][2]*c[2][0]
	if math.Abs(det) < 1e-300 {
		return Mat3{}, false
	}
	for i := range c {
		for j := range c[i] {
			c[i][j] /= det
		}
	}
	return c, true
}

// solveLinear solves the n x n system a x = b by Gaussian elimination with partial pivoting.
func solveLinear(a [][]float64, b []float64) ([]float64, bool) {
	n := len(b)
	for i := 0; i < n; i++ {
		p := i
		for r := i + 1; r < n; r++ {
			if math.Abs(a[r][i]) > math.Abs(a[p][i]) {
				p = r
			}
		}
		if math.Abs(a[p][i]) < 1e-12 {
			return nil, false
		}
		a[i], a[p] = a[p], a[i]
		b[i], b[p] = b[p], b[i]
		for r := i + 1; r < n; r++ {
			f := a[r][i] / a[i][i]
			for c := i; c < n; c++ {
				a[r][c] -= f * a[i][c]
			}
			b[r] -= f * b[i]
		}
	}
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		s := b[i]
		for c := i + 1; c < n; c++ {
			s -= a[i][c] * x[c]
		}
		x[i] = s / a[i][i]
	}
	return x, true
}

// normalizer returns the similarity that centres pts on their centroid and
// scales them to mean distance sqrt(2) (Hartley normalization).
func normalizer(pts [][2]float64) Mat3 {
	var cx, cy float64
	for _, p := range pts {
		cx += p[0]
		cy += p[1]
	}
	n := float64(len(pts))
	cx, cy = cx/n, cy/n
	var d float64
	for _, p := range pts {
		d += math.Hypot(p[0]-cx, p[1]-cy)
	}
	s := 1.0
	if d > 0 {
		s = math.Sqrt2 * n / d
	}
	return Mat3{{s, 0, -s * cx}, {0, s, -s * cy}, {0, 0, 1}}
}

// Homography fits the projective map taking src[i] to dst[i] (least squares over
// four or more correspondences).
func Homography(src, dst [][2]float64) (Mat3, bool) {
	if len(src) != len(dst) || len(src) < 4 {
		return Mat3{}, false
	}
	ts, td := normalizer(src), normalizer(dst)
	ata := make([][]float64, 8)
	for i := range ata {
		ata[i] = make([]float64, 8)
	}
	atb := make([]float64, 8)
	add := func(row [8]float64, rhs float64) {
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				ata[i][j] += row[i] * row[j]
			}
			atb[i] += row[i] * rhs
		}
	}
	for i := range src {
		sx, sy := ts.Apply(src[i][0], src[i][1])
		dx, dy := td.Apply(dst[i][0], dst[i][1])
		add([8]float64{sx, sy, 1, 0, 0, 0, -dx * sx, -dx * sy}, dx)
		add([8]float64{0, 0, 0, sx, sy, 1, -dy * sx, -dy * sy}, dy)
	}
	h, ok := solveLinear(ata, atb)
	if !ok {
		return Mat3{}, false
	}
	hn := Mat3{{h[0], h[1], h[2]}, {h[3], h[4], h[5]}, {h[6], h[7], 1}}
	tdi, ok := td.Inv()
	if !ok {
		return Mat3{}, false
	}
	return tdi.Mul(hn).Mul(ts), true
}
