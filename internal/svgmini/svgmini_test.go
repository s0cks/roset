package svgmini

import (
	"image"
	"math"
	"strconv"
	"testing"
)

func area(img *image.NRGBA) float64 {
	var s float64
	for i := 3; i < len(img.Pix); i += 4 {
		s += float64(img.Pix[i]) / 255
	}
	return s
}

func render(t *testing.T, svg string, size int) (*Doc, *image.NRGBA) {
	t.Helper()
	d, err := Parse([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	s := float64(size) / math.Max(d.VB[2], d.VB[3])
	return d, d.Render(size, size, Scale(s, s).Mul(Translate(-d.VB[0], -d.VB[1])))
}

func near(t *testing.T, name string, got, want, relTol float64) {
	t.Helper()
	if math.Abs(got-want) > relTol*want {
		t.Errorf("%s: area %.1f, want %.1f (±%.1f%%)", name, got, want, relTol*100)
	}
}

func TestShapesAndAreas(t *testing.T) {
	// 100x100 viewBox rendered at 400px: scale 4, so area (units^2) * 16 px^2
	wrap := func(body string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">` + body + `</svg>`
	}
	_, img := render(t, wrap(`<circle cx="50" cy="50" r="40"/>`), 400)
	near(t, "circle", area(img), math.Pi*40*40*16, 0.005)

	_, img = render(t, wrap(`<rect x="10" y="20" width="30" height="40" fill="#ff0000"/>`), 400)
	near(t, "rect", area(img), 30*40*16, 0.002)

	_, img = render(t, wrap(`<path fill-rule="evenodd" d="M10 10H90V90H10Z M30 30H70V70H30Z"/>`), 400)
	near(t, "evenodd donut", area(img), (80*80-40*40)*16, 0.002)

	_, img = render(t, wrap(`<path d="M10 10H90V90H10Z M30 30H70V70H30Z"/>`), 400) // nonzero, same direction: no hole
	near(t, "nonzero same-dir", area(img), 80*80*16, 0.002)

	// two arcs forming a circle, using compact flag syntax "a40 40 0 10 80 0" (flags 1,0 run together)
	_, img = render(t, wrap(`<path d="M10 50a40 40 0 1080 0a40 40 0 10-80 0z"/>`), 400)
	near(t, "arc circle (compact flags)", area(img), math.Pi*40*40*16, 0.006)

	// cubic-bezier circle (kappa) with relative + smooth commands
	k := 0.5522847498 * 40
	_, img = render(t, wrap(`<path d="M90 50c0 `+f(k)+` `+f(-40+k)+` 40 -40 40s-40 `+f(-40+k)+` -40 -40s`+f(40-k)+` -40 40 -40s40 `+f(40-k)+` 40 40z"/>`), 400)
	near(t, "bezier circle", area(img), math.Pi*40*40*16, 0.01)

	// transforms: scaled+translated square
	_, img = render(t, wrap(`<g transform="translate(10 10) scale(2)"><rect width="20" height="20"/></g>`), 400)
	near(t, "transform", area(img), 40*40*16, 0.002)
	_, img = render(t, wrap(`<rect width="20" height="20" transform="rotate(45 50 50) translate(40 40)"/>`), 400)
	near(t, "rotate", area(img), 20*20*16, 0.01)

	// opacity and fill inheritance
	_, img = render(t, wrap(`<g fill="#00f" opacity="0.5"><rect width="50" height="50"/></g>`), 400)
	near(t, "opacity", area(img), 0.5*50*50*16, 0.006)

	// style attribute overrides, fill=none skipped
	_, img = render(t, wrap(`<rect width="50" height="50" style="fill:none"/><rect x="50" width="10" height="10" style="fill:#123"/>`), 400)
	near(t, "style attr", area(img), 10*10*16, 0.003)
}

func f(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }

func TestWarnings(t *testing.T) {
	d, err := Parse([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><defs><linearGradient id="g"/></defs>
<rect width="5" height="5" fill="url(#g)"/><path d="M0 0H5" stroke="red"/><text>hi</text></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Warnings) < 3 {
		t.Fatalf("expected warnings for gradient, stroke and text, got %v", d.Warnings)
	}
}

func TestRejectsNonSVG(t *testing.T) {
	if _, err := Parse([]byte(`<html></html>`)); err == nil {
		t.Fatal("expected error")
	}
}
