package decode

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
)

var paper = color.NRGBA{248, 246, 238, 255}

func bilin(src *image.NRGBA, x, y float64, bg color.NRGBA) color.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if x < 0 || y < 0 || x > float64(w-1) || y > float64(h-1) {
		return bg
	}
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, w-1), min(y0+1, h-1)
	fx, fy := x-float64(x0), y-float64(y0)
	var out [3]float64
	for i, c := range [4]struct {
		x, y int
		w    float64
	}{{x0, y0, (1 - fx) * (1 - fy)}, {x1, y0, fx * (1 - fy)}, {x0, y1, (1 - fx) * fy}, {x1, y1, fx * fy}} {
		p := src.NRGBAAt(c.x, c.y)
		_ = i
		out[0] += float64(p.R) * c.w
		out[1] += float64(p.G) * c.w
		out[2] += float64(p.B) * c.w
	}
	return color.NRGBA{uint8(out[0] + .5), uint8(out[1] + .5), uint8(out[2] + .5), 255}
}

// affine rotates by deg, scales, shears and places the symbol centred (+tx,ty) on an out x out canvas.
func affine(src *image.NRGBA, deg, scale, shear float64, out int, tx, ty float64) *image.NRGBA {
	a := deg * math.Pi / 180
	r := [2][2]float64{{math.Cos(a), -math.Sin(a)}, {math.Sin(a), math.Cos(a)}}
	s := [2][2]float64{{scale, shear * scale}, {0, scale}}
	var m [2][2]float64
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			m[i][j] = r[i][0]*s[0][j] + r[i][1]*s[1][j]
		}
	}
	det := m[0][0]*m[1][1] - m[0][1]*m[1][0]
	inv := [2][2]float64{{m[1][1] / det, -m[0][1] / det}, {-m[1][0] / det, m[0][0] / det}}
	sw := float64(src.Bounds().Dx()-1) / 2
	dst := image.NewNRGBA(image.Rect(0, 0, out, out))
	for y := 0; y < out; y++ {
		for x := 0; x < out; x++ {
			dx, dy := float64(x)-float64(out-1)/2-tx, float64(y)-float64(out-1)/2-ty
			dst.SetNRGBA(x, y, bilin(src, sw+inv[0][0]*dx+inv[0][1]*dy, sw+inv[1][0]*dx+inv[1][1]*dy, paper))
		}
	}
	return dst
}

// tilt views the plane from a pinhole camera rotated by deg about the horizontal axis.
func tilt(src *image.NRGBA, deg float64, out int) *image.NRGBA {
	th := deg * math.Pi / 180
	f := 1.2 * float64(out)
	sw := float64(src.Bounds().Dx()-1) / 2
	dst := image.NewNRGBA(image.Rect(0, 0, out, out))
	for y := 0; y < out; y++ {
		for x := 0; x < out; x++ {
			u, v := float64(x)-float64(out-1)/2, float64(y)-float64(out-1)/2
			Y := v * f / (f*math.Cos(th) - v*math.Sin(th))
			X := u * (Y*math.Sin(th) + f) / f
			dst.SetNRGBA(x, y, bilin(src, sw+X*float64(src.Bounds().Dx())/float64(out)*0+X, sw+Y, paper))
		}
	}
	return dst
}

func blur(src *image.NRGBA, sigma float64) *image.NRGBA {
	rad := int(math.Ceil(sigma * 3))
	k := make([]float64, 2*rad+1)
	var sum float64
	for i := range k {
		d := float64(i - rad)
		k[i] = math.Exp(-d * d / (2 * sigma * sigma))
		sum += k[i]
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	pass := func(in *image.NRGBA, horiz bool) *image.NRGBA {
		o := image.NewNRGBA(in.Bounds())
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				var acc [3]float64
				for i := range k {
					xx, yy := x, y
					if horiz {
						xx = min(max(x+i-rad, 0), w-1)
					} else {
						yy = min(max(y+i-rad, 0), h-1)
					}
					p := in.NRGBAAt(xx, yy)
					acc[0] += float64(p.R) * k[i]
					acc[1] += float64(p.G) * k[i]
					acc[2] += float64(p.B) * k[i]
				}
				o.SetNRGBA(x, y, color.NRGBA{uint8(acc[0]/sum + .5), uint8(acc[1]/sum + .5), uint8(acc[2]/sum + .5), 255})
			}
		}
		return o
	}
	return pass(pass(src, true), false)
}

func noise(src *image.NRGBA, sigma float64, seed int64) *image.NRGBA {
	rng := rand.New(rand.NewSource(seed))
	o := image.NewNRGBA(src.Bounds())
	cl := func(v float64) uint8 { return uint8(math.Max(0, math.Min(255, v))) }
	for i := 0; i < len(src.Pix); i += 4 {
		n := rng.NormFloat64() * sigma
		o.Pix[i], o.Pix[i+1], o.Pix[i+2], o.Pix[i+3] = cl(float64(src.Pix[i])+n), cl(float64(src.Pix[i+1])+n), cl(float64(src.Pix[i+2])+n), 255
	}
	return o
}

func jpegRT(src image.Image, q int) image.Image {
	var buf bytes.Buffer
	jpeg.Encode(&buf, src, &jpeg.Options{Quality: q})
	out, _ := jpeg.Decode(&buf)
	return out
}

func gradient(src *image.NRGBA, strength float64) *image.NRGBA {
	o := image.NewNRGBA(src.Bounds())
	w := float64(src.Bounds().Dx())
	for y := 0; y < src.Bounds().Dy(); y++ {
		for x := 0; x < src.Bounds().Dx(); x++ {
			f := 1 - strength*float64(x)/w
			p := src.NRGBAAt(x, y)
			o.SetNRGBA(x, y, color.NRGBA{uint8(float64(p.R) * f), uint8(float64(p.G) * f), uint8(float64(p.B) * f), 255})
		}
	}
	return o
}

// fillDisc paints a paper-coloured disc (a smudge).
func fillDisc(img *image.NRGBA, cx, cy, r float64) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if math.Hypot(float64(x)-cx, float64(y)-cy) <= r {
				img.SetNRGBA(x, y, paper)
			}
		}
	}
}

var _ = rand.New
var _ = bytes.NewBuffer
