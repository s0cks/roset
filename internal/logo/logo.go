// Package logo loads a logo (SVG, PNG, JPEG or GIF) and places it inside the
// symbol's clear centre zone, for both bitmap and vector output.
//
// Placement: by default the logo is scaled so the smallest circle enclosing its
// visible ink (alpha >= 24, centred on the ink's bounding box) fills 97% of the
// zone; round seals therefore fill the zone and rectangular logos fit with
// nothing clipped. FitViewBox instead fits the whole image/viewBox rectangle.
package logo

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"regexp"
	"strings"

	"roset/internal/svgmini"
)

type Fit int

const (
	FitAuto    Fit = iota // enclose the visible ink
	FitViewBox            // fit the whole viewBox / image rectangle
)

const fillFrac = 0.97 // fraction of the zone radius the logo's enclosing circle may use

type Logo struct {
	Warnings []string

	svg    *svgmini.Doc
	svgSrc []byte

	img    *image.NRGBA
	raw    []byte
	mime   string
	X0, Y0 float64 // natural coordinate origin (viewBox origin; 0,0 for bitmaps)
	W, H   float64 // natural size (viewBox size; pixels for bitmaps)

	CX, CY, Rad float64 // placement circle in natural coordinates (before Scale)
	Scale       float64
}

func (l *Logo) IsSVG() bool { return l.svg != nil }

var (
	badSVG = regexp.MustCompile(`(?is)<script|<foreignobject|<!entity|javascript:|\son[a-z]+\s*=|(?:xlink:)?href\s*=\s*["']\s*(?:https?:|//|file:)`)
	rootRe = regexp.MustCompile(`(?is)<svg\b([^>]*)>(.*)</svg\s*>`)
	nsRe   = regexp.MustCompile(`xmlns(?::[\w-]+)?\s*=\s*"[^"]*"`)
)

// Load reads a logo file. scale multiplies the automatic size (1 = fill the zone).
func Load(path string, fit Fit, scale float64) (*Logo, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) > 8<<20 {
		return nil, errors.New("logo file larger than 8 MB")
	}
	if scale <= 0 {
		scale = 1
	}
	l := &Logo{Scale: scale}
	head := strings.ToLower(string(b[:min(len(b), 2048)]))
	if strings.Contains(head, "<svg") {
		if badSVG.Match(b) {
			return nil, errors.New("logo SVG contains scripts, event handlers, entities or external references; refusing to embed it")
		}
		d, err := svgmini.Parse(b)
		if err != nil {
			return nil, err
		}
		l.svg, l.svgSrc = d, b
		l.X0, l.Y0, l.W, l.H = d.VB[0], d.VB[1], d.VB[2], d.VB[3]
		l.Warnings = d.Warnings
		l.measureSVG()
	} else {
		im, format, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("logo is neither SVG nor a PNG/JPEG/GIF image: %w", err)
		}
		n := image.NewNRGBA(im.Bounds())
		draw.Draw(n, n.Bounds(), im, im.Bounds().Min, draw.Src)
		l.img, l.raw, l.mime = n, b, "image/"+format
		l.W, l.H = float64(n.Bounds().Dx()), float64(n.Bounds().Dy())
		l.measureBitmap()
	}
	if l.Rad <= 0 {
		return nil, errors.New("logo has no visible content")
	}
	if fit == FitViewBox {
		l.CX, l.CY, l.Rad = l.X0+l.W/2, l.Y0+l.H/2, math.Hypot(l.W, l.H)/2
	}
	return l, nil
}

const inkAlpha = 24

func (l *Logo) measureSVG() {
	const n = 512
	s := n / math.Max(l.W, l.H)
	ox, oy := (n-l.W*s)/2, (n-l.H*s)/2
	m := svgmini.Translate(ox, oy).Mul(svgmini.Scale(s, s)).Mul(svgmini.Translate(-l.X0, -l.Y0))
	img := l.svg.Render(n, n, m)
	cx, cy, rad, ok := inkCircle(img, 1)
	if !ok {
		return
	}
	l.CX, l.CY, l.Rad = l.X0+(cx-ox)/s, l.Y0+(cy-oy)/s, rad/s
}

func (l *Logo) measureBitmap() {
	step := max(1, int(math.Max(l.W, l.H)/1024))
	cx, cy, rad, ok := inkCircle(l.img, step)
	if ok {
		l.CX, l.CY, l.Rad = cx, cy, rad
	}
}

// inkCircle returns the centre of the ink's bounding box and the largest
// distance from it to any ink pixel corner (so the circle really encloses the ink).
func inkCircle(img *image.NRGBA, step int) (cx, cy, rad float64, ok bool) {
	b := img.Bounds()
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			if img.Pix[img.PixOffset(x, y)+3] >= inkAlpha {
				minX, maxX = math.Min(minX, float64(x)), math.Max(maxX, float64(x+1))
				minY, maxY = math.Min(minY, float64(y)), math.Max(maxY, float64(y+1))
			}
		}
	}
	if math.IsInf(minX, 1) {
		return 0, 0, 0, false
	}
	cx, cy = (minX+maxX)/2, (minY+maxY)/2
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			if img.Pix[img.PixOffset(x, y)+3] >= inkAlpha {
				for _, c := range [4][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
					rad = math.Max(rad, math.Hypot(float64(x)+c[0]-cx, float64(y)+c[1]-cy))
				}
			}
		}
	}
	return cx, cy, rad, true
}

// k is natural-coordinate units per symbol unit for a zone of radius zoneR.
func (l *Logo) k(zoneR float64) float64 { return l.Rad / (l.Scale * fillFrac * zoneR) }

// RenderZone renders the logo into a px x px NRGBA covering the square
// [-zoneR, zoneR]^2 of the symbol (the caller masks it to the zone circle).
func (l *Logo) RenderZone(px int, zoneR float64) *image.NRGBA {
	k := l.k(zoneR)
	a := float64(px) / (2 * zoneR * k) // pixels per natural unit
	if l.svg != nil {
		m := svgmini.Affine{a, 0, 0, a, float64(px)/2 - l.CX*a, float64(px)/2 - l.CY*a}
		return l.svg.Render(px, px, m)
	}
	out := image.NewNRGBA(image.Rect(0, 0, px, px))
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			nx := l.CX + (float64(x)+0.5-float64(px)/2)/a
			ny := l.CY + (float64(y)+0.5-float64(px)/2)/a
			r, g, b, al := l.sample(nx-0.5, ny-0.5)
			if al > 0 {
				i := out.PixOffset(x, y)
				out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = clamp8(r/al*255), clamp8(g/al*255), clamp8(b/al*255), clamp8(al*255)
			}
		}
	}
	return out
}

func clamp8(v float64) uint8 { return uint8(math.Max(0, math.Min(255, v+0.5))) }

// sample is bilinear in premultiplied space; returns premultiplied r,g,b (0..1) and alpha (0..1).
func (l *Logo) sample(x, y float64) (r, g, b, a float64) {
	w, h := l.img.Bounds().Dx(), l.img.Bounds().Dy()
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			xx, yy := x0+dx, y0+dy
			if xx < 0 || yy < 0 || xx >= w || yy >= h {
				continue
			}
			wt := (1 - fx + float64(dx)*(2*fx-1)) * (1 - fy + float64(dy)*(2*fy-1))
			i := l.img.PixOffset(xx, yy)
			pa := float64(l.img.Pix[i+3]) / 255
			r += wt * float64(l.img.Pix[i]) / 255 * pa
			g += wt * float64(l.img.Pix[i+1]) / 255 * pa
			b += wt * float64(l.img.Pix[i+2]) / 255 * pa
			a += wt * pa
		}
	}
	return
}

// SVGFragment returns SVG markup that draws the logo in symbol units, centred
// on the origin, to be clipped by the caller to the zone circle. SVG logos stay vector.
func (l *Logo) SVGFragment(zoneR float64) string {
	k := l.k(zoneR)
	var b strings.Builder
	fmt.Fprintf(&b, `<g transform="scale(%.8g) translate(%.6g %.6g)">`+"\n", 1/k, -l.CX, -l.CY)
	if l.svg != nil {
		m := rootRe.FindSubmatch(l.svgSrc)
		inner, ns := "", ""
		if m != nil {
			inner = string(m[2])
			ns = strings.Join(nsRe.FindAllString(string(m[1]), -1), " ")
		}
		if !strings.Contains(ns, `xmlns="`) {
			ns += ` xmlns="http://www.w3.org/2000/svg"`
		}
		fmt.Fprintf(&b, `<svg %s x="%.6g" y="%.6g" width="%.6g" height="%.6g" viewBox="%.6g %.6g %.6g %.6g" preserveAspectRatio="none" overflow="hidden">`+"\n%s\n</svg>\n",
			ns, l.X0, l.Y0, l.W, l.H, l.X0, l.Y0, l.W, l.H, inner)
	} else {
		fmt.Fprintf(&b, `<image x="0" y="0" width="%.6g" height="%.6g" preserveAspectRatio="none" href="data:%s;base64,%s"/>`+"\n",
			l.W, l.H, l.mime, base64.StdEncoding.EncodeToString(l.raw))
	}
	b.WriteString("</g>\n")
	return b.String()
}
