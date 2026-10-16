// Package svgmini is a deliberately small SVG-to-bitmap rasterizer: filled
// shapes only (path, rect, circle, ellipse, polygon, polyline), groups with
// transforms, fill / fill-rule / opacity. It exists because the Go standard
// library has no SVG support; anything it cannot draw is reported in
// Doc.Warnings instead of being silently dropped.
package svgmini

import "math"

// Affine is x' = a*x + c*y + e, y' = b*x + d*y + f stored as {a,b,c,d,e,f}.
type Affine [6]float64

var Identity = Affine{1, 0, 0, 1, 0, 0}

// Mul returns m∘n: n is applied first, then m.
func (m Affine) Mul(n Affine) Affine {
	return Affine{
		m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

func (m Affine) Apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

func Translate(x, y float64) Affine { return Affine{1, 0, 0, 1, x, y} }
func Scale(x, y float64) Affine     { return Affine{x, 0, 0, y, 0, 0} }
func Rotate(deg float64) Affine {
	r := deg * math.Pi / 180
	return Affine{math.Cos(r), math.Sin(r), -math.Sin(r), math.Cos(r), 0, 0}
}
