package decode

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"testing"

	"roset/internal/codec"
	"roset/internal/layout"
	"roset/internal/logo"
	"roset/internal/render"
	"roset/internal/spec"
)

var ink = color.NRGBA{22, 22, 28, 255}

type sym struct {
	img     *image.NRGBA
	payload []byte
	mode    int
	v       spec.Version
	size    int
}

var cache = map[string]sym{}

func payloadFor(v spec.Version, rng *rand.Rand) ([]byte, int) {
	n, mode := 32, codec.ModeRawSHA256
	if v.Capacity() > 32 {
		n, mode = 32+30, codec.ModeSHA256Tag
	}
	p := make([]byte, n)
	rng.Read(p)
	if n == 32 {
		h := sha256.Sum256(p)
		p = h[:]
	}
	return p, mode
}

func makeSym(t testing.TB, ver int, st layout.Style, size int, seed int64) sym {
	return makeLogoSym(t, ver, st, size, seed, nil, ink, paper, nil)
}

func makeLogoSym(t testing.TB, ver int, st layout.Style, size int, seed int64, lg *logo.Logo, fg, bg color.NRGBA, lbg *color.NRGBA) sym {
	t.Helper()
	key := fmt.Sprint(ver, st, size, seed, lg != nil, fg, bg, lbg)
	if s, ok := cache[key]; ok {
		return s
	}
	v, ok := spec.Lookup(ver)
	if !ok {
		t.Fatalf("no version %d", ver)
	}
	payload, mode := payloadFor(v, rand.New(rand.NewSource(seed)))
	c, err := codec.Encode(v, mode, payload)
	if err != nil {
		t.Fatal(err)
	}
	s := sym{render.PNG(layout.New(v, c, st), size, fg, bg, &render.Options{Logo: lg, LogoBG: lbg}), payload, mode, v, size}
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
	t.Logf("%-36s ok  (unit=%.1fpx pass=%s rs-fixed=%d)", name, rep.Unit, rep.Binarizer, res.Corrected)
	return true
}

func loadLogo(t testing.TB) *logo.Logo {
	t.Helper()
	lg, err := logo.Load("../../testdata/logo.svg", logo.FitAuto, 1)
	if err != nil {
		t.Fatal(err)
	}
	return lg
}

// petalCentre is the pixel position of the middle of petal p in a symbol rendered at s.size.
func (s sym) petalCentre(p int) (x, y, r float64) {
	g := s.v.Geo()
	scale := float64(s.size) / (2 * g.Half)
	var sx, sy float64
	for _, c := range g.Cells[p*g.PetalN : (p+1)*g.PetalN] {
		sx, sy = sx+c.X, sy+c.Y
	}
	n := float64(g.PetalN)
	return float64(s.size)/2 + sx/n*scale, float64(s.size)/2 + sy/n*scale, (float64(g.K) + 0.9) * scale
}

func TestCleanAllVersions(t *testing.T) {
	for _, v := range spec.Versions {
		for _, st := range []layout.Style{layout.StyleDots, layout.StyleRuns} {
			s := makeSym(t, v.ID, st, 800, int64(v.ID))
			check(t, fmt.Sprintf("clean v%d(K%d,%dlvl) style=%d", v.ID, v.K, v.Levels(), st), s.img, s)
		}
	}
}

func TestRotations(t *testing.T) {
	for _, id := range []int{1, 6} {
		s := makeSym(t, id, layout.StyleRuns, 800, 11)
		for deg := 0.0; deg < 360; deg += 37 {
			check(t, fmt.Sprintf("v%d rotate %.0f", id, deg), affine(s.img, deg, 1, 0, 800, 0, 0), s)
		}
	}
}

func TestScales(t *testing.T) {
	for _, id := range []int{1, 6} {
		s := makeSym(t, id, layout.StyleRuns, 800, 12)
		for _, px := range []int{360, 500, 650} {
			check(t, fmt.Sprintf("v%d downscale to %dpx", id, px), affine(s.img, 21, float64(px)/800, 0, px, 0, 0), s)
		}
	}
	big := makeSym(t, 6, layout.StyleDots, 800, 13)
	check(t, "upscale 2400px canvas", affine(big.img, 0, 3, 0, 2400, 0, 0), big)
}

func TestAffineViews(t *testing.T) {
	s := makeSym(t, 6, layout.StyleRuns, 900, 14)
	check(t, "shear 0.25", affine(s.img, 10, 0.8, 0.25, 900, 0, 0), s)
	check(t, "squash (rot 30, shear .45)", affine(affine(s.img, 0, 0.7, 0, 900, 0, 0), 30, 1, 0.45, 900, 0, 0), s)
}

func TestDegradations(t *testing.T) {
	for _, id := range []int{1, 6} {
		s := makeSym(t, id, layout.StyleRuns, 900, 15)
		check(t, fmt.Sprintf("v%d blur sigma 1.5 @750px", id), blur(affine(s.img, 13, 0.83, 0, 750, 0, 0), 1.5), s)
		check(t, fmt.Sprintf("v%d noise sigma 15", id), noise(affine(s.img, 5, 0.7, 0, 630, 0, 0), 15, 1), s)
		check(t, fmt.Sprintf("v%d jpeg q60 @630px", id), jpegRT(affine(s.img, 40, 0.7, 0, 630, 0, 0), 60), s)
		check(t, fmt.Sprintf("v%d lighting gradient 30%%", id), gradient(affine(s.img, 0, 0.8, 0, 720, 0, 0), 0.30), s)
	}
	s := makeSym(t, 6, layout.StyleRuns, 900, 16)
	check(t, "combo blur+noise+jpeg", jpegRT(noise(blur(affine(s.img, 77, 0.7, 0.1, 630, 0, 0), 1.2), 8, 2), 70), s)
}

// One whole petal wiped out must still decode (the codeword is spread across petals).
func TestPetalOcclusion(t *testing.T) {
	for _, id := range []int{1, 6, 9} {
		s := makeSym(t, id, layout.StyleRuns, 900, 17)
		for p := 0; p < 6; p += 2 {
			img := affine(s.img, 0, 1, 0, 900, 0, 0)
			x, y, r := s.petalCentre(p)
			fillDisc(img, x, y, r)
			check(t, fmt.Sprintf("v%d petal %d wiped", id, p), img, s)
		}
	}
}

func TestClutterAndOffset(t *testing.T) {
	s := makeSym(t, 6, layout.StyleDots, 900, 18)
	img := affine(s.img, 123, 0.6, 0, 1400, 310, -180)
	rng := rand.New(rand.NewSource(5))
	dark := color.NRGBA{30, 30, 30, 255}
	for i := 0; i < 25; i++ { // random dark squares and bars away from the symbol
		x, y := rng.Intn(1300), rng.Intn(1300)
		if math.Hypot(float64(x)-1010, float64(y)-520) < 380 {
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
	s := makeSym(t, 6, layout.StyleRuns, 800, 19)
	m := image.NewNRGBA(s.img.Bounds())
	inv := image.NewNRGBA(s.img.Bounds())
	for y := 0; y < 800; y++ {
		for x := 0; x < 800; x++ {
			m.SetNRGBA(x, y, s.img.NRGBAAt(799-x, y))
			q := s.img.NRGBAAt(x, y)
			inv.SetNRGBA(x, y, color.NRGBA{255 - q.R, 255 - q.G, 255 - q.B, 255})
		}
	}
	check(t, "mirrored", m, s)
	check(t, "inverted (light ink on dark)", inv, s)
	// light-on-dark rendered natively, as a user would with --fg/--bg
	d := makeLogoSym(t, 6, layout.StyleRuns, 800, 20, nil, color.NRGBA{206, 205, 195, 255}, color.NRGBA{16, 15, 15, 255}, nil)
	check(t, "dark theme", d.img, d)
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

// Tilt up to 40 degrees is required, 50 is logged.
func TestPerspectiveEnvelope(t *testing.T) {
	s := makeSym(t, 6, layout.StyleRuns, 900, 21)
	for _, deg := range []float64{10, 20, 30, 40, 50} {
		res, _, err := Decode(tilt(s.img, deg, 900), Options{})
		ok := err == nil && bytes.Equal(res.Payload, s.payload)
		t.Logf("perspective tilt %2.0f deg: ok=%v", deg, ok)
		if deg <= 30 && !ok {
			t.Errorf("tilt %.0f deg should decode", deg)
		}
	}
}

func TestPerspectiveCombined(t *testing.T) {
	s := makeSym(t, 6, layout.StyleRuns, 900, 22)
	rot := affine(s.img, 63, 1, 0, 900, 0, 0)
	check(t, "rot63 + tilt25", tilt(rot, 25, 900), s)
	small := affine(tilt(rot, 20, 900), 0, 0.7, 0, 630, 0, 0)
	check(t, "rot63 + tilt20 + 630px", small, s)
	check(t, "rot63 + tilt20 + blur/noise/jpeg", jpegRT(noise(blur(small, 1.0), 8, 3), 70), s)
}

func TestLogoSymbols(t *testing.T) {
	lg := loadLogo(t)
	for _, id := range []int{1, 6, 7, 9, 10} {
		s := makeLogoSym(t, id, layout.StyleRuns, 900, 23, lg, ink, paper, nil)
		check(t, fmt.Sprintf("logo v%d", id), s.img, s)
		check(t, fmt.Sprintf("logo v%d rot+tilt", id), tilt(affine(s.img, 71, 1, 0, 900, 0, 0), 25, 900), s)
	}
	// a solid black disc as the "logo" must not disturb detection: the logo ink is never used
	dark := color.NRGBA{0, 0, 0, 255}
	s := makeLogoSym(t, 6, layout.StyleDots, 900, 24, lg, ink, paper, &dark)
	check(t, "logo on dark backdrop", s.img, s)
}
