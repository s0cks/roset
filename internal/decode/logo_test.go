package decode

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"roset/internal/codec"
	"roset/internal/layout"
	"roset/internal/logo"
	"roset/internal/render"
	"roset/internal/spec"
)

func makeLogoSym(t testing.TB, verID int, st layout.Style, size int, seed int64, lg *logo.Logo, fg, bg color.NRGBA, lbg *color.NRGBA) sym {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	v, ok := spec.Lookup(verID)
	if !ok || !v.HasLogo() {
		t.Fatalf("version %d is not a logo version", verID)
	}
	n, mode := 32, codec.ModeRawSHA256
	if v.Capacity() > 32 {
		n, mode = 32+30, codec.ModeSHA256Tag
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
	return sym{render.PNG(layout.New(v, c, st), size, fg, bg, &render.Options{Logo: lg, LogoBG: lbg}), payload, mode}
}

func loadLogo(t testing.TB, path string) *logo.Logo {
	t.Helper()
	lg, err := logo.Load(path, logo.FitAuto, 1)
	if err != nil {
		t.Fatal(err)
	}
	return lg
}

func saveLogoPNG(t testing.TB, img image.Image) string {
	p := filepath.Join(t.TempDir(), "logo.png")
	var b bytes.Buffer
	png.Encode(&b, img)
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLogoVersionsClean(t *testing.T) {
	lg := loadLogo(t, "../../testdata/logo.svg")
	dark, light := color.NRGBA{16, 15, 15, 255}, color.NRGBA{206, 205, 195, 255}
	for id := 3; id <= 8; id++ {
		for _, st := range []layout.Style{layout.StyleDots, layout.StyleRuns} {
			s := makeLogoSym(t, id, st, 900, int64(id), lg, ink, paper, nil)
			check(t, fmt.Sprintf("logo v%d style=%d (light theme)", id, st), s.img, s)
		}
		s := makeLogoSym(t, id, layout.StyleRuns, 900, int64(id)+50, lg, light, dark, nil)
		check(t, fmt.Sprintf("logo v%d dark theme", id), s.img, s)
	}
}

func TestLogoUnderDistortion(t *testing.T) {
	lg := loadLogo(t, "../../testdata/logo.svg")
	for _, id := range []int{3, 5, 7, 4, 8} {
		s := makeLogoSym(t, id, layout.StyleRuns, 1000, 70+int64(id), lg, ink, paper, nil)
		rot := affine(s.img, 37, 1, 0, 1000, 0, 0)
		check(t, fmt.Sprintf("logo v%d rot37 + tilt30", id), tilt(rot, 30, 1000), s)
		small := affine(tilt(rot, 25, 1000), 0, 0.55, 0, 550, 0, 0)
		check(t, fmt.Sprintf("logo v%d rot37 tilt25 550px", id), small, s)
		check(t, fmt.Sprintf("logo v%d blur+noise+jpeg", id), jpegRT(noise(blur(small, 1.2), 12, 3), 60), s)
	}
}

// Logos designed to confuse detection or to maximise ink touching the zone edge.
func TestAdversarialLogos(t *testing.T) {
	disc := func(w int, f func(r float64) color.NRGBA) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, w, w))
		for y := 0; y < w; y++ {
			for x := 0; x < w; x++ {
				r := math.Hypot(float64(x)+0.5-float64(w)/2, float64(y)+0.5-float64(w)/2) / (float64(w) / 2)
				img.SetNRGBA(x, y, f(r))
			}
		}
		return img
	}
	black := color.NRGBA{0, 0, 0, 255}
	rng := rand.New(rand.NewSource(9))
	cases := map[string]image.Image{
		"solid black disc": disc(400, func(r float64) color.NRGBA {
			if r <= 1 {
				return black
			}
			return color.NRGBA{}
		}),
		"concentric rings (finder lookalike)": disc(400, func(r float64) color.NRGBA {
			if (r > 0.45 && r < 0.6) || (r > 0.75 && r <= 1) {
				return black
			}
			return color.NRGBA{}
		}),
		"fine checkerboard disc": func() image.Image {
			img := disc(400, func(r float64) color.NRGBA { return color.NRGBA{} })
			for y := 0; y < 400; y++ {
				for x := 0; x < 400; x++ {
					if math.Hypot(float64(x)-200, float64(y)-200) <= 200 && (x/8+y/8)%2 == 0 {
						img.SetNRGBA(x, y, black)
					}
				}
			}
			return img
		}(),
		"random noise square (full bleed, opaque)": func() image.Image {
			img := image.NewNRGBA(image.Rect(0, 0, 300, 300))
			for i := 0; i < len(img.Pix); i += 4 {
				v := uint8(rng.Intn(2) * 255)
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v, v, 255
			}
			return img
		}(),
		"wide wordmark bar": func() image.Image {
			img := image.NewNRGBA(image.Rect(0, 0, 600, 150))
			for y := 40; y < 110; y++ {
				for x := 0; x < 600; x++ {
					img.SetNRGBA(x, y, black)
				}
			}
			return img
		}(),
	}
	for name, im := range cases {
		lg, err := logo.Load(saveLogoPNG(t, im), logo.FitAuto, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []int{3, 6, 7} {
			s := makeLogoSym(t, id, layout.StyleRuns, 900, 200+int64(id), lg, ink, paper, nil)
			check(t, fmt.Sprintf("v%d %s", id, name), s.img, s)
		}
		// and once under rotation + tilt + degradation
		s := makeLogoSym(t, 5, layout.StyleDots, 1000, 99, lg, ink, paper, nil)
		d := jpegRT(noise(blur(affine(tilt(affine(s.img, 101, 1, 0, 1000, 0, 0), 20, 1000), 0, 0.6, 0, 600, 0, 0), 1.0), 10, 5), 65)
		check(t, "v5 "+name+" degraded", d, s)
	}
}

func TestLogoBackdropAndScale(t *testing.T) {
	lg, err := logo.Load("../../testdata/logo.svg", logo.FitAuto, 0.8)
	if err != nil {
		t.Fatal(err)
	}
	dark := color.NRGBA{16, 15, 15, 255}
	s := makeLogoSym(t, 5, layout.StyleRuns, 900, 5, lg, ink, paper, &dark) // dark medallion on a light symbol
	check(t, "dark logo backdrop on light symbol", s.img, s)
	vb, _ := logo.Load("../../testdata/logo.svg", logo.FitViewBox, 1)
	s = makeLogoSym(t, 6, layout.StyleDots, 900, 6, vb, ink, paper, nil)
	check(t, "viewbox fit", s.img, s)
}
