package render

import (
	"fmt"
	"math"
	"strings"

	"roset/internal/layout"
	"roset/internal/logo"
	"roset/internal/spec"
)

// SVGOptions customises SVG output.
type SVGOptions struct {
	Logo   *logo.Logo // embedded (as vector if it is an SVG) in the clear centre zone
	LogoBG string     // CSS colour behind the logo; "" = the symbol background
}

// SVG renders the layout as a self-contained vector image. fg and bg are CSS colors.
func SVG(l *layout.Layout, fg, bg string, opt *SVGOptions) string {
	v := l.V
	R := v.TotalRadius()
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="%.3f %.3f %.3f %.3f" width="1024" height="1024">`+"\n", -R, -R, 2*R, 2*R)
	fmt.Fprintf(&b, `<title>roset v%d</title>`+"\n", v.ID)
	fmt.Fprintf(&b, `<rect x="%.3f" y="%.3f" width="%.3f" height="%.3f" fill="%s"/>`+"\n", -R, -R, 2*R, 2*R, bg)

	if opt != nil && opt.Logo != nil && v.HasLogo() {
		zbg := bg
		if opt.LogoBG != "" {
			zbg = opt.LogoBG
		}
		fmt.Fprintf(&b, `<defs><clipPath id="rs-logo-zone"><circle r="%.3f"/></clipPath></defs>`+"\n", v.LogoR)
		fmt.Fprintf(&b, `<circle r="%.3f" fill="%s"/>`+"\n", v.LogoR, zbg)
		fmt.Fprintf(&b, `<g clip-path="url(#rs-logo-zone)">`+"\n%s</g>\n", opt.Logo.SVGFragment(v.LogoR))
	}
	// finder bullseye
	for _, band := range v.FinderBands() {
		if band[0] == 0 {
			fmt.Fprintf(&b, `<circle r="%.3f" fill="%s"/>`+"\n", band[1], fg)
		} else {
			fmt.Fprintf(&b, `<circle r="%.3f" fill="none" stroke="%s" stroke-width="%.3f"/>`+"\n",
				(band[0]+band[1])/2, fg, band[1]-band[0])
		}
	}
	// boundary ring and sync dot
	fmt.Fprintf(&b, `<circle r="%.3f" fill="none" stroke="%s" stroke-width="%.3f"/>`+"\n", v.BoundaryRadius(), fg, 2*spec.BoundaryHalf)
	fmt.Fprintf(&b, `<circle cx="0" cy="%.3f" r="%.3f" fill="%s"/>`+"\n", -v.SyncRadius(), spec.SyncDotRadius, fg)

	var dots, arcs, fulls strings.Builder
	ring := func(bits []bool, rc float64) {
		n := len(bits)
		at := func(i int) (float64, float64) {
			a := 2 * math.Pi * float64(i) / float64(n)
			return rc * math.Sin(a), -rc * math.Cos(a)
		}
		if l.Style == layout.StyleDots {
			for i, on := range bits {
				if on {
					x, y := at(i)
					fmt.Fprintf(&dots, `<circle cx="%.3f" cy="%.3f" r="%.3f"/>`+"\n", x, y, spec.DotRadius)
				}
			}
			return
		}
		for _, run := range layout.Runs(bits) {
			switch {
			case run.Len == n:
				fmt.Fprintf(&fulls, `<circle r="%.3f" fill="none" stroke="%s" stroke-width="%.3f"/>`+"\n", rc, fg, 2*spec.DotRadius)
			case run.Len == 1:
				x, y := at(run.Start)
				fmt.Fprintf(&dots, `<circle cx="%.3f" cy="%.3f" r="%.3f"/>`+"\n", x, y, spec.DotRadius)
			default:
				x0, y0 := at(run.Start)
				x1, y1 := at((run.Start + run.Len - 1) % n)
				large := 0
				if 2*math.Pi*float64(run.Len-1)/float64(n) > math.Pi {
					large = 1
				}
				fmt.Fprintf(&arcs, "M%.3f %.3fA%.3f %.3f 0 %d 1 %.3f %.3f", x0, y0, rc, rc, large, x1, y1)
			}
		}
	}
	for j := 0; j < spec.HeaderRings; j++ {
		ring(l.Cells.Header[j], v.HeaderRingRadius(j))
	}
	for k := 0; k < v.Rings; k++ {
		ring(l.Cells.Data[k], v.DataRingRadius(k))
	}
	if dots.Len() > 0 {
		fmt.Fprintf(&b, `<g fill="%s">`+"\n%s</g>\n", fg, dots.String())
	}
	if l.Style == layout.StyleRuns {
		if arcs.Len() > 0 {
			fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="%s" stroke-width="%.3f" stroke-linecap="round"/>`+"\n", arcs.String(), fg, 2*spec.DotRadius)
		}
		b.WriteString(fulls.String())
	}
	b.WriteString("</svg>\n")
	return b.String()
}
