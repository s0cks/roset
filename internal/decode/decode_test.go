package decode

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
	"testing"

	"roset/internal/codec"
	"roset/internal/layout"
	"roset/internal/render"
	"roset/internal/spec"
)

var (
	ink   = color.NRGBA{22, 22, 28, 255}
	paper = color.NRGBA{248, 246, 238, 255}
)

type sym struct {
	img     *image.NRGBA
	payload []byte
	mode    int
}

var cache = map[string]sym{}

func makeSym(t testing.TB, ver int, st layout.Style, size int, seed int64) sym {
	key := fmt.Sprint(ver, st, size, seed)
	if s, ok := cache[key]; ok {
		return s
	}
	rng := rand.New(rand.NewSource(seed))
	v, _ := spec.Lookup(ver)
	n := 32
	mode := codec.ModeRawSHA256
	if ver == 2 {
		n = 32 + 40
		mode = codec.ModeSHA256Tag
	}
	payload := make([]byte, n)
	rng.Read(payload)
	if n == 32 {
		h := sha256.Sum256(payload)
		payload = h[:]
	}
	c, err := codec.Encode(v, mode, payload)
	if err != nil {
		t.Fatal(err)
	}
	s := sym{render.PNG(layout.New(v, c, st), size, ink, paper, nil), payload, mode}
	cache[key] = s
	return s
}

func check(t *testing.T, name string, img image.Image, want sym) bool {
	t.Helper()
	res, rep, err := Decode(img, Options{})
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return false
	}
	if !bytes.Equal(res.Payload, want.payload) || res.Mode != want.mode {
		t.Errorf("%s: WRONG payload decoded", name)
		return false
	}
	t.Logf("%-34s ok  (unit=%.1fpx frame=%s pass=%s rs-fixed=%d)", name, rep.Unit, rep.FrameKind, rep.Binarizer, res.Corrected)
	return true
}

// ---- distortion helpers ----

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

func fillWedge(img *image.NRGBA, cx, cy, r0, r1, a0, a1 float64) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			r := math.Hypot(dx, dy)
			a := math.Atan2(dx, -dy)
			if a < 0 {
				a += 2 * math.Pi
			}
			if r >= r0 && r <= r1 && a >= a0 && a <= a1 {
				img.SetNRGBA(x, y, paper)
			}
		}
	}
}

// ---- tests ----

func TestCleanAllVariants(t *testing.T) {
	for _, ver := range []int{1, 2} {
		for _, st := range []layout.Style{layout.StyleDots, layout.StyleRuns} {
			for seed := int64(1); seed <= 3; seed++ {
				s := makeSym(t, ver, st, 800, seed)
				check(t, fmt.Sprintf("clean v%d style=%d seed=%d", ver, st, seed), s.img, s)
			}
		}
	}
}

func TestRotations(t *testing.T) {
	s := makeSym(t, 1, layout.StyleRuns, 800, 11)
	for deg := 0.0; deg < 360; deg += 37 {
		check(t, fmt.Sprintf("rotate %.0f", deg), affine(s.img, deg, 1, 0, 800, 0, 0), s)
	}
}

func TestScales(t *testing.T) {
	s := makeSym(t, 1, layout.StyleRuns, 800, 12)
	for _, px := range []int{200, 260, 360, 500} {
		check(t, fmt.Sprintf("downscale to %dpx", px), affine(s.img, 21, float64(px)/800, 0, px, 0, 0), s)
	}
	big := makeSym(t, 1, layout.StyleDots, 800, 13)
	check(t, "upscale 2400px canvas", affine(big.img, 0, 3, 0, 2400, 0, 0), big)
}

func TestAffineViews(t *testing.T) {
	s := makeSym(t, 1, layout.StyleRuns, 800, 14)
	check(t, "shear 0.25", affine(s.img, 10, 0.8, 0.25, 800, 0, 0), s)
	check(t, "squash x0.7 (rot 30)", affine(affine(s.img, 0, 1, 0, 800, 0, 0), 30, 1, 0.45, 800, 0, 0), s)
}

func TestDegradations(t *testing.T) {
	s := makeSym(t, 1, layout.StyleRuns, 800, 15)
	check(t, "blur sigma 2.0 @600px", blur(affine(s.img, 13, 0.75, 0, 600, 0, 0), 2.0), s)
	check(t, "gaussian noise sigma 25", noise(affine(s.img, 5, 0.6, 0, 500, 0, 0), 25, 1), s)
	check(t, "jpeg q45 @500px", jpegRT(affine(s.img, 40, 0.625, 0, 500, 0, 0), 45), s)
	check(t, "lighting gradient 55%", gradient(affine(s.img, 0, 0.7, 0, 560, 0, 0), 0.55), s)
	check(t, "blur+noise+jpeg combo", jpegRT(noise(blur(affine(s.img, 77, 0.55, 0.1, 460, 0, 0), 1.4), 14, 2), 55), s)
}

func TestOcclusion(t *testing.T) {
	s := makeSym(t, 1, layout.StyleRuns, 800, 16)
	img := affine(s.img, 0, 1, 0, 800, 0, 0)
	u := 800 / (2 * s.verRadius())
	fillWedge(img, 399.5, 399.5, 12*u, 19*u, 1.0, 1.6) // ~35 deg smudge across data rings
	check(t, "35deg smudge over data rings", img, s)
}

func (s sym) verRadius() float64 { return spec.Versions[0].TotalRadius() }

func TestClutterAndOffset(t *testing.T) {
	s := makeSym(t, 1, layout.StyleDots, 800, 17)
	img := affine(s.img, 123, 0.5, 0, 1400, 310, -180)
	rng := rand.New(rand.NewSource(5))
	dark := color.NRGBA{30, 30, 30, 255}
	for i := 0; i < 25; i++ { // random dark squares and bars away from the symbol
		x, y := rng.Intn(1300), rng.Intn(1300)
		if math.Hypot(float64(x)-1010, float64(y)-520) < 330 {
			continue
		}
		w, h := 10+rng.Intn(60), 3+rng.Intn(40)
		for yy := y; yy < y+h && yy < 1400; yy++ {
			for xx := x; xx < x+w && xx < 1400; xx++ {
				img.SetNRGBA(xx, yy, dark)
			}
		}
	}
	check(t, "offset in clutter", img, s)
}

func TestMirrorAndInverted(t *testing.T) {
	s := makeSym(t, 1, layout.StyleRuns, 800, 18)
	m := image.NewNRGBA(s.img.Bounds())
	inv := image.NewNRGBA(s.img.Bounds())
	for y := 0; y < 800; y++ {
		for x := 0; x < 800; x++ {
			p := s.img.NRGBAAt(799-x, y)
			m.SetNRGBA(x, y, p)
			q := s.img.NRGBAAt(x, y)
			inv.SetNRGBA(x, y, color.NRGBA{255 - q.R, 255 - q.G, 255 - q.B, 255})
		}
	}
	check(t, "mirrored", m, s)
	check(t, "inverted (light ink on dark)", inv, s)
}

func TestNoSymbol(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 400, 400))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	if _, _, err := Decode(img, Options{}); err == nil {
		t.Fatal("expected an error for a blank image")
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < len(img.Pix); i += 4 {
		v := uint8(rng.Intn(256))
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = v, v, v
	}
	if _, _, err := Decode(img, Options{}); err == nil {
		t.Fatal("expected an error for pure noise")
	}
}

// Projective frame from the two concentric circles: tilt up to 40 degrees is required, 50 is logged.
func TestPerspectiveEnvelope(t *testing.T) {
	s := makeSym(t, 1, layout.StyleRuns, 800, 19)
	for _, deg := range []float64{10, 20, 30, 40, 50} {
		img := tilt(s.img, deg, 800)
		res, _, err := Decode(img, Options{})
		ok := err == nil && bytes.Equal(res.Payload, s.payload)
		t.Logf("perspective tilt %2.0f deg: ok=%v", deg, ok)
		if deg <= 40 && !ok {
			t.Errorf("tilt %.0f deg should decode", deg)
		}
	}
}

func TestPerspectiveCombined(t *testing.T) {
	s := makeSym(t, 1, layout.StyleRuns, 900, 21)
	rot := affine(s.img, 63, 1, 0, 900, 0, 0)
	check(t, "rot63 + tilt30", tilt(rot, 30, 900), s)
	small := affine(tilt(rot, 25, 900), 0, 0.6, 0, 560, 0, 0)
	check(t, "rot63 + tilt25 + 560px", small, s)
	check(t, "rot63 + tilt25 + blur/noise/jpeg", jpegRT(noise(blur(small, 1.2), 12, 3), 60), s)
	v2 := makeSym(t, 2, layout.StyleDots, 1000, 22)
	check(t, "v2 dots rot140 + tilt25", tilt(affine(v2.img, 140, 1, 0, 1000, 0, 0), 25, 1000), v2)
}
