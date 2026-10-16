package svgmini

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type pt struct{ X, Y float64 }

type shape struct {
	subs    [][]pt // flattened subpaths in document (viewBox) coordinates
	r, g, b float64
	a       float64
	evenOdd bool
}

// Doc is a parsed SVG: a viewBox and a flat list of filled shapes.
type Doc struct {
	VB       [4]float64 // x, y, w, h
	Shapes   []shape
	Warnings []string
	warned   map[string]bool
}

func (d *Doc) warn(format string, a ...any) {
	m := fmt.Sprintf(format, a...)
	if d.warned == nil {
		d.warned = map[string]bool{}
	}
	if !d.warned[m] {
		d.warned[m] = true
		d.Warnings = append(d.Warnings, m)
	}
}

type paint struct {
	kind    int // 0 none, 1 colour
	r, g, b float64
	a       float64
}

type style struct {
	fill     paint
	evenOdd  bool
	opacity  float64
	fillOp   float64
	hasStrok bool
	ctm      Affine
}

var skipElems = map[string]bool{"defs": true, "clippath": true, "mask": true, "symbol": true, "pattern": true,
	"marker": true, "lineargradient": true, "radialgradient": true, "filter": true, "title": true,
	"desc": true, "metadata": true, "style": true, "script": true, "foreignobject": true}

var drawable = map[string]bool{"path": true, "rect": true, "circle": true, "ellipse": true, "polygon": true, "polyline": true}

// Parse reads an SVG document.
func Parse(src []byte) (*Doc, error) {
	dec := xml.NewDecoder(bytes.NewReader(src))
	dec.Strict = false
	d := &Doc{}
	var stack []style
	skip := 0
	haveRoot := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("svg: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if skip > 0 || skipElems[name] {
				if name == "style" {
					d.warn("<style> blocks (CSS) are ignored; use fill attributes")
				}
				skip++
				continue
			}
			attrs := map[string]string{}
			for _, a := range t.Attr {
				attrs[strings.ToLower(a.Name.Local)] = a.Value
			}
			if st := attrs["style"]; st != "" {
				for _, decl := range strings.Split(st, ";") {
					if k, v, ok := strings.Cut(decl, ":"); ok {
						attrs[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
					}
				}
			}
			var parent style
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			} else {
				parent = style{fill: paint{kind: 1, a: 1}, opacity: 1, fillOp: 1, ctm: Identity}
			}
			st := d.deriveStyle(parent, attrs)
			if name == "svg" && !haveRoot {
				haveRoot = true
				d.VB = parseViewBox(attrs)
				if d.VB[2] <= 0 || d.VB[3] <= 0 {
					return nil, errors.New("svg: cannot determine size (need viewBox or width/height)")
				}
			}
			stack = append(stack, st)
			if drawable[name] {
				d.addShape(name, attrs, st)
			} else if name == "text" || name == "image" || name == "use" || name == "tspan" {
				d.warn("<%s> is not rendered in PNG output", name)
			}
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if !haveRoot {
		return nil, errors.New("svg: no <svg> root element")
	}
	return d, nil
}

func (d *Doc) deriveStyle(p style, a map[string]string) style {
	s := p
	if v, ok := a["fill"]; ok {
		s.fill = d.parseColor(v)
	}
	if v, ok := a["fill-rule"]; ok {
		s.evenOdd = strings.TrimSpace(v) == "evenodd"
	}
	if v, ok := a["opacity"]; ok {
		s.opacity = p.opacity * clamp01(num(v, 1))
	}
	if v, ok := a["fill-opacity"]; ok {
		s.fillOp = clamp01(num(v, 1))
	}
	if v, ok := a["stroke"]; ok {
		s.hasStrok = strings.TrimSpace(v) != "none" && strings.TrimSpace(v) != ""
	}
	if v, ok := a["transform"]; ok {
		s.ctm = p.ctm.Mul(parseTransform(v))
	}
	return s
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }

func num(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	pct := strings.HasSuffix(s, "%")
	s = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(s, "%"), "px"), "pt")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	if pct {
		return v / 100
	}
	return v
}

func parseViewBox(a map[string]string) [4]float64 {
	if vb, ok := a["viewbox"]; ok {
		f := strings.FieldsFunc(vb, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
		if len(f) == 4 {
			var o [4]float64
			for i := range o {
				o[i] = num(f[i], 0)
			}
			return o
		}
	}
	return [4]float64{0, 0, num(a["width"], 0), num(a["height"], 0)}
}

var named = map[string][3]float64{
	"black": {0, 0, 0}, "white": {1, 1, 1}, "red": {1, 0, 0}, "green": {0, 128.0 / 255, 0}, "blue": {0, 0, 1},
	"gray": {128.0 / 255, 128.0 / 255, 128.0 / 255}, "grey": {128.0 / 255, 128.0 / 255, 128.0 / 255},
	"yellow": {1, 1, 0}, "orange": {1, 165.0 / 255, 0}, "purple": {128.0 / 255, 0, 128.0 / 255},
	"silver": {192.0 / 255, 192.0 / 255, 192.0 / 255}, "currentcolor": {0, 0, 0},
}

func (d *Doc) parseColor(v string) paint {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case v == "none" || v == "transparent":
		return paint{}
	case strings.HasPrefix(v, "#"):
		h := v[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for _, c := range h {
				b.WriteRune(c)
				b.WriteRune(c)
			}
			h = b.String()
		}
		if len(h) == 6 || len(h) == 8 {
			x, err := strconv.ParseUint(h, 16, 64)
			if err == nil {
				if len(h) == 6 {
					return paint{1, float64(x>>16&255) / 255, float64(x>>8&255) / 255, float64(x&255) / 255, 1}
				}
				return paint{1, float64(x>>24&255) / 255, float64(x>>16&255) / 255, float64(x>>8&255) / 255, float64(x&255) / 255}
			}
		}
	case strings.HasPrefix(v, "rgb"):
		in := v[strings.Index(v, "(")+1:]
		in = strings.TrimSuffix(strings.TrimSpace(in), ")")
		f := strings.FieldsFunc(in, func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(f) >= 3 {
			c := func(s string) float64 {
				if strings.HasSuffix(s, "%") {
					return clamp01(num(s, 0))
				}
				return clamp01(num(s, 0) / 255)
			}
			a := 1.0
			if len(f) >= 4 {
				a = clamp01(num(f[3], 1))
			}
			return paint{1, c(f[0]), c(f[1]), c(f[2]), a}
		}
	default:
		if c, ok := named[v]; ok {
			return paint{1, c[0], c[1], c[2], 1}
		}
	}
	d.warn("fill %q is not supported (gradients/patterns/unknown colour names); shape skipped", v)
	return paint{}
}

var tfRe = regexp.MustCompile(`(\w+)\s*\(([^)]*)\)`)

func parseTransform(s string) Affine {
	m := Identity
	for _, g := range tfRe.FindAllStringSubmatch(s, -1) {
		f := strings.FieldsFunc(g[2], func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
		n := make([]float64, len(f))
		for i := range f {
			n[i] = num(f[i], 0)
		}
		at := func(i int, def float64) float64 {
			if i < len(n) {
				return n[i]
			}
			return def
		}
		var t Affine
		switch strings.ToLower(g[1]) {
		case "matrix":
			if len(n) != 6 {
				continue
			}
			t = Affine{n[0], n[1], n[2], n[3], n[4], n[5]}
		case "translate":
			t = Translate(at(0, 0), at(1, 0))
		case "scale":
			t = Scale(at(0, 1), at(1, at(0, 1)))
		case "rotate":
			t = Rotate(at(0, 0))
			if len(n) == 3 {
				t = Translate(n[1], n[2]).Mul(t).Mul(Translate(-n[1], -n[2]))
			}
		case "skewx":
			t = Affine{1, 0, math.Tan(at(0, 0) * math.Pi / 180), 1, 0, 0}
		case "skewy":
			t = Affine{1, math.Tan(at(0, 0) * math.Pi / 180), 0, 1, 0, 0}
		default:
			continue
		}
		m = m.Mul(t)
	}
	return m
}

func (d *Doc) addShape(name string, a map[string]string, st style) {
	if st.hasStrok {
		d.warn("strokes are not rendered in PNG output (<%s> has a stroke); use SVG output or convert strokes to outlines", name)
	}
	if st.fill.kind == 0 {
		return
	}
	tol := math.Max(d.VB[2], d.VB[3]) / 400
	var subs [][]pt
	switch name {
	case "path":
		subs = parsePath(a["d"], tol)
	case "rect":
		x, y, w, h := num(a["x"], 0), num(a["y"], 0), num(a["width"], 0), num(a["height"], 0)
		rx, ry := num(a["rx"], -1), num(a["ry"], -1)
		if rx < 0 && ry >= 0 {
			rx = ry
		}
		if ry < 0 && rx >= 0 {
			ry = rx
		}
		rx, ry = math.Max(0, math.Min(rx, w/2)), math.Max(0, math.Min(ry, h/2))
		if rx > 0 && ry > 0 {
			subs = parsePath(fmt.Sprintf("M%g %gH%gA%g %g 0 0 1 %g %gV%gA%g %g 0 0 1 %g %gH%gA%g %g 0 0 1 %g %gV%gA%g %g 0 0 1 %g %gZ",
				x+rx, y, x+w-rx, rx, ry, x+w, y+ry, y+h-ry, rx, ry, x+w-rx, y+h, x+rx, rx, ry, x, y+h-ry, y+ry, rx, ry, x+rx, y), tol)
		} else {
			subs = [][]pt{{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}}
		}
	case "circle", "ellipse":
		cx, cy := num(a["cx"], 0), num(a["cy"], 0)
		rx, ry := num(a["r"], 0), num(a["r"], 0)
		if name == "ellipse" {
			rx, ry = num(a["rx"], 0), num(a["ry"], 0)
		}
		n := int(math.Max(32, math.Min(256, math.Pi*(rx+ry)/tol)))
		sub := make([]pt, n)
		for i := range sub {
			t := 2 * math.Pi * float64(i) / float64(n)
			sub[i] = pt{cx + rx*math.Cos(t), cy + ry*math.Sin(t)}
		}
		subs = [][]pt{sub}
	case "polygon", "polyline":
		f := strings.FieldsFunc(a["points"], func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
		var sub []pt
		for i := 0; i+1 < len(f); i += 2 {
			sub = append(sub, pt{num(f[i], 0), num(f[i+1], 0)})
		}
		subs = [][]pt{sub}
	}
	var out [][]pt
	for _, s := range subs {
		if len(s) < 3 {
			continue
		}
		t := make([]pt, len(s))
		for i, p := range s {
			t[i].X, t[i].Y = st.ctm.Apply(p.X, p.Y)
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return
	}
	d.Shapes = append(d.Shapes, shape{subs: out, r: st.fill.r, g: st.fill.g, b: st.fill.b,
		a: st.fill.a * st.fillOp * st.opacity, evenOdd: st.evenOdd})
}
