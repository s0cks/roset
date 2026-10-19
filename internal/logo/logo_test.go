package logo

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePNG(t *testing.T, w, h int, f func(x, y int) color.NRGBA) string {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, f(x, y))
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	p := filepath.Join(t.TempDir(), "l.png")
	os.WriteFile(p, b.Bytes(), 0o644)
	return p
}

func TestSealFitsZone(t *testing.T) {
	l, err := Load("../../testdata/logo.svg", FitAuto, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !l.IsSVG() || len(l.Warnings) != 0 {
		t.Fatalf("svg=%v warnings=%v", l.IsSVG(), l.Warnings)
	}
	// the seal's ring spans ~72% of the 1254 viewBox; it is centred horizontally but sits
	// ~20 units above the viewBox centre, which is exactly what ink-based fitting corrects for
	if math.Abs(l.CX-627) > 15 || math.Abs(l.CY-607) > 15 || l.Rad < 400 || l.Rad > 520 {
		t.Fatalf("placement circle off: c=(%.1f,%.1f) r=%.1f", l.CX, l.CY, l.Rad)
	}
	const px, Z = 400, 3.45
	img := l.RenderZone(px, Z)
	// nothing may be drawn outside the 97% circle, and the seal must reach close to it
	maxR := 0.0
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			if img.NRGBAAt(x, y).A >= inkAlpha {
				maxR = math.Max(maxR, math.Hypot(float64(x)+0.5-px/2, float64(y)+0.5-px/2))
			}
		}
	}
	if frac := maxR / (px / 2); frac > 0.975 || frac < 0.93 {
		t.Fatalf("ink reaches %.3f of the zone radius, want ~0.97", frac)
	}
}

func TestRectangularLogoNotClipped(t *testing.T) {
	// 300x100 opaque bar: auto-fit must enclose its corners within the zone circle
	p := writePNG(t, 300, 100, func(x, y int) color.NRGBA { return color.NRGBA{200, 30, 30, 255} })
	l, err := Load(p, FitAuto, 1)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(l.Rad-math.Hypot(150, 50)) > 2 {
		t.Fatalf("radius %.1f, want half-diagonal %.1f", l.Rad, math.Hypot(150, 50))
	}
	const px = 300
	img := l.RenderZone(px, 4)
	for _, c := range [][2]int{{px/2 - 130, px/2 - 40}, {px/2 + 130, px/2 + 40}} { // near the bar's corners
		if img.NRGBAAt(c[0], c[1]).A == 0 {
			t.Fatalf("corner %v was clipped", c)
		}
	}
}

func TestTransparentBitmapIgnoresPadding(t *testing.T) {
	p := writePNG(t, 200, 200, func(x, y int) color.NRGBA {
		if math.Hypot(float64(x)-100, float64(y)-100) < 30 {
			return color.NRGBA{0, 0, 0, 255}
		}
		return color.NRGBA{}
	})
	l, err := Load(p, FitAuto, 1)
	if err != nil {
		t.Fatal(err)
	}
	if l.Rad < 29 || l.Rad > 33 { // 30px disc, not the 141px half-diagonal of the canvas
		t.Fatalf("radius %.1f, want ~30", l.Rad)
	}
	v, _ := Load(p, FitViewBox, 1)
	if math.Abs(v.Rad-math.Hypot(100, 100)) > 1 {
		t.Fatalf("viewbox radius %.1f", v.Rad)
	}
}

func TestRejectsUnsafeSVG(t *testing.T) {
	for _, body := range []string{
		`<script>alert(1)</script>`,
		`<rect width="5" height="5" onclick="x()"/>`,
		`<image href="http://evil.example/x.png" width="5" height="5"/>`,
		`<foreignObject/>`,
	} {
		p := filepath.Join(t.TempDir(), "x.svg")
		os.WriteFile(p, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">`+body+`</svg>`), 0o644)
		if _, err := Load(p, FitAuto, 1); err == nil {
			t.Errorf("accepted unsafe SVG body %q", body)
		}
	}
}

func TestFragmentShapes(t *testing.T) {
	l, _ := Load("../../testdata/logo.svg", FitAuto, 1)
	f := l.SVGFragment(5.5)
	if !strings.Contains(f, `<svg `) || !strings.Contains(f, `viewBox="0 0 1254 1254"`) || strings.Contains(f, "<script") {
		t.Fatalf("unexpected fragment head: %.200s", f)
	}
	p := writePNG(t, 20, 20, func(x, y int) color.NRGBA { return color.NRGBA{1, 2, 3, 255} })
	lb, _ := Load(p, FitAuto, 1)
	if !strings.Contains(lb.SVGFragment(4), "data:image/png;base64,") {
		t.Fatal("bitmap logo not embedded as data URI")
	}
}
