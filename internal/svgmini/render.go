package svgmini

import (
	"image"
	"image/color"
	"math"
	"sort"
)

type cross struct {
	x   float64
	dir int
}

// Render rasterizes the document into a w x h image with transparent
// background. m maps document (viewBox) coordinates to pixel coordinates.
// Anti-aliasing: 4 vertical sub-scanlines per pixel with exact horizontal coverage.
func (d *Doc) Render(w, h int, m Affine) *image.NRGBA {
	const ss = 4
	acc := make([]float32, w*h*4) // premultiplied RGBA
	cov := make([]float32, w)
	for _, sh := range d.Shapes {
		// to pixel space
		polys := make([][]pt, len(sh.subs))
		ymin, ymax := math.Inf(1), math.Inf(-1)
		for i, s := range sh.subs {
			t := make([]pt, len(s))
			for j, p := range s {
				t[j].X, t[j].Y = m.Apply(p.X, p.Y)
				ymin, ymax = math.Min(ymin, t[j].Y), math.Max(ymax, t[j].Y)
			}
			polys[i] = t
		}
		y0, y1 := int(math.Max(0, math.Floor(ymin))), int(math.Min(float64(h-1), math.Floor(ymax)))
		if y1 < y0 {
			continue
		}
		nsr := (y1 - y0 + 1) * ss
		rows := make([][]cross, nsr)
		for _, poly := range polys {
			for i := range poly {
				p, q := poly[i], poly[(i+1)%len(poly)]
				if p.Y == q.Y {
					continue
				}
				dir := 1
				if q.Y < p.Y {
					p, q, dir = q, p, -1
				}
				r0 := int(math.Ceil((p.Y-float64(y0))*ss - 0.5))
				r1 := int(math.Ceil((q.Y-float64(y0))*ss-0.5)) - 1
				if r0 < 0 {
					r0 = 0
				}
				if r1 > nsr-1 {
					r1 = nsr - 1
				}
				for r := r0; r <= r1; r++ {
					y := float64(y0) + (float64(r)+0.5)/ss
					x := p.X + (y-p.Y)*(q.X-p.X)/(q.Y-p.Y)
					rows[r] = append(rows[r], cross{x, dir})
				}
			}
		}
		for py := y0; py <= y1; py++ {
			touched := false
			for k := 0; k < ss; k++ {
				cs := rows[(py-y0)*ss+k]
				if len(cs) < 2 {
					continue
				}
				sort.Slice(cs, func(a, b int) bool { return cs[a].x < cs[b].x })
				wind := 0
				for i := 0; i < len(cs)-1; i++ {
					if sh.evenOdd {
						wind ^= 1
					} else {
						wind += cs[i].dir
					}
					if wind != 0 {
						addSpan(cov, cs[i].x, cs[i+1].x)
						touched = true
					}
				}
			}
			if !touched {
				continue
			}
			for x := 0; x < w; x++ {
				c := cov[x]
				if c == 0 {
					continue
				}
				cov[x] = 0
				a := float32(math.Min(1, float64(c)/ss) * sh.a)
				i := (py*w + x) * 4
				ia := 1 - a
				acc[i] = float32(sh.r)*a + acc[i]*ia
				acc[i+1] = float32(sh.g)*a + acc[i+1]*ia
				acc[i+2] = float32(sh.b)*a + acc[i+2]*ia
				acc[i+3] = a + acc[i+3]*ia
			}
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		a := acc[i*4+3]
		if a <= 0 {
			continue
		}
		img.Pix[i*4] = u8(acc[i*4] / a)
		img.Pix[i*4+1] = u8(acc[i*4+1] / a)
		img.Pix[i*4+2] = u8(acc[i*4+2] / a)
		img.Pix[i*4+3] = u8(a)
	}
	_ = color.NRGBA{}
	return img
}

func u8(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

// addSpan adds the horizontal coverage of [xa,xb) to the row buffer.
func addSpan(cov []float32, xa, xb float64) {
	w := float64(len(cov))
	if xb <= 0 || xa >= w || xb <= xa {
		return
	}
	xa, xb = math.Max(xa, 0), math.Min(xb, w)
	ia, ib := int(xa), int(xb)
	if ib >= len(cov) {
		ib = len(cov) - 1
	}
	if ia == ib {
		cov[ia] += float32(xb - xa)
		return
	}
	cov[ia] += float32(float64(ia+1) - xa)
	for i := ia + 1; i < ib; i++ {
		cov[i]++
	}
	cov[ib] += float32(xb - float64(ib))
}
