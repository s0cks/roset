package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"roset/internal/layout"
	"roset/internal/logo"
	"roset/internal/spec"
)

// SVGOptions customises SVG output.
type SVGOptions struct {
	Logo   *logo.Logo // embedded (as vector if it is an SVG) in the clear centre disc
	LogoBG string     // CSS colour behind the logo; "" = the symbol background
}

// mixHex blends two #rrggbb colours: t=0 gives bg, t=1 gives fg.
func mixHex(fg, bg string, t float64) string {
	p := func(s string) [3]float64 {
		var c [3]float64
		s = strings.TrimPrefix(s, "#")
		for i := 0; i < 3 && len(s) >= 6; i++ {
			v, _ := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
			c[i] = float64(v)
		}
		return c
	}
	f, b := p(fg), p(bg)
	var o [3]int
	for i := range o {
		o[i] = int(b[i] + (f[i]-b[i])*t + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", o[0], o[1], o[2])
}

func hexPoints(apothem float64) string {
	r := spec.ApothemToCircumradius(apothem)
	var pts []string
	for j := 0; j < 6; j++ {
		a := float64(j) * math.Pi / 3
		pts = append(pts, fmt.Sprintf("%.3f,%.3f", r*math.Cos(a), r*math.Sin(a)))
	}
	return strings.Join(pts, " ")
}

// SVG renders the layout as a self-contained vector image. fg and bg are #rrggbb colours.
func SVG(l *layout.Layout, fg, bg string, opt *SVGOptions) string {
	v, g := l.V, l.G
	R := g.Half
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="%.3f %.3f %.3f %.3f" width="1024" height="1024">`+"\n", -R, -R, 2*R, 2*R)
	fmt.Fprintf(&b, `<title>roset v%d</title>`+"\n", v.ID)
	fmt.Fprintf(&b, `<rect x="%.3f" y="%.3f" width="%.3f" height="%.3f" fill="%s"/>`+"\n", -R, -R, 2*R, 2*R, bg)

	if opt != nil && opt.Logo != nil {
		zbg := bg
		if opt.LogoBG != "" {
			zbg = opt.LogoBG
		}
		fmt.Fprintf(&b, `<defs><clipPath id="rs-logo-zone"><circle r="%.3f"/></clipPath></defs>`+"\n", g.LogoR)
		fmt.Fprintf(&b, `<circle r="%.3f" fill="%s"/>`+"\n", g.LogoR, zbg)
		fmt.Fprintf(&b, `<g clip-path="url(#rs-logo-zone)">`+"\n%s</g>\n", opt.Logo.SVGFragment(g.LogoR))
	}
	// inner hexagon and border
	fmt.Fprintf(&b, `<polygon points="%s" fill="none" stroke="%s" stroke-width="%.3f" stroke-linejoin="miter"/>`+"\n", hexPoints(g.AInner), fg, spec.InnerStroke)
	fmt.Fprintf(&b, `<polygon points="%s" fill="none" stroke="%s" stroke-width="%.3f" stroke-linejoin="miter"/>`+"\n", hexPoints(g.ABorder), fg, spec.BorderStroke)

	// header ring: dots, or runs as strokes along the ring
	n := spec.HeaderSlots
	var hd, hs strings.Builder
	if l.Style == layout.StyleDots {
		for t := 0; t < n; t++ {
			if l.HeaderBit(t) {
				x, y := g.HeaderPos(t)
				fmt.Fprintf(&hd, `<circle cx="%.3f" cy="%.3f" r="%.3f"/>`+"\n", x, y, spec.DotRadius)
			}
		}
	} else {
		for _, run := range l.HeaderRuns() {
			switch {
			case run.B == n:
				fmt.Fprintf(&hs, `<polygon points="%s" fill="none" stroke="%s" stroke-width="%.3f"/>`+"\n", hexPoints(g.AHeader), fg, 2*spec.DotRadius)
			case run.B == 1:
				x, y := g.HeaderPos(run.A)
				fmt.Fprintf(&hd, `<circle cx="%.3f" cy="%.3f" r="%.3f"/>`+"\n", x, y, spec.DotRadius)
			default:
				for k := 0; k < run.B-1; k++ {
					x0, y0 := g.HeaderPos((run.A + k) % n)
					x1, y1 := g.HeaderPos((run.A + k + 1) % n)
					fmt.Fprintf(&hs, `<path d="M%.3f %.3fL%.3f %.3f" stroke="%s" stroke-width="%.3f" stroke-linecap="round" fill="none"/>`+"\n", x0, y0, x1, y1, fg, 2*spec.DotRadius)
				}
			}
		}
	}
	if hd.Len() > 0 {
		fmt.Fprintf(&b, `<g fill="%s">`+"\n%s</g>\n", fg, hd.String())
	}
	b.WriteString(hs.String())

	// data cells, grouped by level
	pairs := l.Pairs()
	linked := map[int]bool{}
	if l.Style == layout.StyleRuns {
		for _, p := range pairs {
			linked[p.A], linked[p.B] = true, true
		}
	}
	for lv := 1; lv < v.Levels(); lv++ {
		col := mixHex(fg, bg, l.Darkness(uint8(lv)))
		var dots, links strings.Builder
		for i, c := range g.Cells {
			if int(l.Cells.Data[i]) == lv && !linked[i] {
				fmt.Fprintf(&dots, `<circle cx="%.3f" cy="%.3f" r="%.3f"/>`+"\n", c.X, c.Y, spec.DotRadius)
			}
		}
		if l.Style == layout.StyleRuns {
			var d strings.Builder
			for _, p := range pairs {
				if int(l.Cells.Data[p.A]) != lv {
					continue
				}
				a, c := g.Cells[p.A], g.Cells[p.B]
				fmt.Fprintf(&d, "M%.3f %.3fL%.3f %.3f", a.X, a.Y, c.X, c.Y)
			}
			if d.Len() > 0 {
				fmt.Fprintf(&links, `<path d="%s" fill="none" stroke="%s" stroke-width="%.3f" stroke-linecap="round"/>`+"\n", d.String(), col, 2*spec.DotRadius)
			}
		}
		if dots.Len() > 0 {
			fmt.Fprintf(&b, `<g fill="%s">`+"\n%s</g>\n", col, dots.String())
		}
		b.WriteString(links.String())
	}
	b.WriteString("</svg>\n")
	return b.String()
}
