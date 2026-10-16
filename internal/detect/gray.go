// Package detect holds the pure-Go computer-vision primitives: grayscale
// conversion, adaptive thresholding, connected components, bullseye finding and
// affine frame estimation. No cgo, no third-party dependencies.
package detect

import (
	"image"
	"math"
)

type Gray struct {
	W, H int
	Pix  []float32 // 0..255
}

// FromImage converts to luminance (compositing alpha over white) and, if the
// longer side exceeds maxDim, area-averages it down. It returns the gray image
// and the applied scale factor (<=1).
func FromImage(img image.Image, maxDim int) (*Gray, float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	g := &Gray{W: w, H: h, Pix: make([]float32, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, gg, bb, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			w16 := float64(65535 - a)
			l := 0.299*(float64(r)+w16) + 0.587*(float64(gg)+w16) + 0.114*(float64(bb)+w16)
			g.Pix[y*w+x] = float32(l / 257)
		}
	}
	m := w
	if h > m {
		m = h
	}
	if maxDim > 0 && m > maxDim {
		f := float64(maxDim) / float64(m)
		return g.resize(f), f
	}
	return g, 1
}

func (g *Gray) Invert() *Gray {
	o := &Gray{W: g.W, H: g.H, Pix: make([]float32, len(g.Pix))}
	for i, v := range g.Pix {
		o.Pix[i] = 255 - v
	}
	return o
}

func (g *Gray) resize(f float64) *Gray {
	nw, nh := int(float64(g.W)*f+0.5), int(float64(g.H)*f+0.5)
	it := NewIntegral(g)
	o := &Gray{W: nw, H: nh, Pix: make([]float32, nw*nh)}
	for y := 0; y < nh; y++ {
		y0 := int(math.Floor(float64(y) / f))
		y1 := int(math.Ceil(float64(y+1) / f))
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < nw; x++ {
			x0 := int(math.Floor(float64(x) / f))
			x1 := int(math.Ceil(float64(x+1) / f))
			if x1 <= x0 {
				x1 = x0 + 1
			}
			o.Pix[y*nw+x] = float32(it.Mean(x0, y0, x1, y1))
		}
	}
	return o
}

// Bilinear samples with pixel centers at integer coordinates; outside = 255.
func (g *Gray) Bilinear(x, y float64) float64 {
	if x < 0 || y < 0 || x > float64(g.W-1) || y > float64(g.H-1) {
		return 255
	}
	x0, y0 := int(x), int(y)
	x1, y1 := x0+1, y0+1
	if x1 >= g.W {
		x1 = g.W - 1
	}
	if y1 >= g.H {
		y1 = g.H - 1
	}
	fx, fy := x-float64(x0), y-float64(y0)
	p := func(xx, yy int) float64 { return float64(g.Pix[yy*g.W+xx]) }
	return (p(x0, y0)*(1-fx)+p(x1, y0)*fx)*(1-fy) + (p(x0, y1)*(1-fx)+p(x1, y1)*fx)*fy
}

// Integral is a summed-area table.
type Integral struct {
	W, H int
	S    []float64
}

func NewIntegral(g *Gray) *Integral {
	w, h := g.W, g.H
	it := &Integral{W: w, H: h, S: make([]float64, (w+1)*(h+1))}
	for y := 0; y < h; y++ {
		row := 0.0
		for x := 0; x < w; x++ {
			row += float64(g.Pix[y*w+x])
			it.S[(y+1)*(w+1)+x+1] = it.S[y*(w+1)+x+1] + row
		}
	}
	return it
}

// Sum over [x0,x1) x [y0,y1), clipped to the image.
func (it *Integral) Sum(x0, y0, x1, y1 int) float64 {
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, it.W), min(y1, it.H)
	if x1 <= x0 || y1 <= y0 {
		return 0
	}
	w := it.W + 1
	return it.S[y1*w+x1] - it.S[y0*w+x1] - it.S[y1*w+x0] + it.S[y0*w+x0]
}

func (it *Integral) Mean(x0, y0, x1, y1 int) float64 {
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, it.W), min(y1, it.H)
	n := (x1 - x0) * (y1 - y0)
	if n <= 0 {
		return 255
	}
	return it.Sum(x0, y0, x1, y1) / float64(n)
}
