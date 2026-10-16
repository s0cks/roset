package svgmini

import "math"

type pathParser struct {
	s string
	i int
}

func (p *pathParser) skipSep() {
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r' {
			p.i++
		} else {
			break
		}
	}
}

func (p *pathParser) more() bool { p.skipSep(); return p.i < len(p.s) }

func (p *pathParser) peekIsNumber() bool {
	p.skipSep()
	if p.i >= len(p.s) {
		return false
	}
	c := p.s[p.i]
	return c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9')
}

func (p *pathParser) number() (float64, bool) {
	p.skipSep()
	st := p.i
	if p.i < len(p.s) && (p.s[p.i] == '-' || p.s[p.i] == '+') {
		p.i++
	}
	dot := false
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c >= '0' && c <= '9' {
			p.i++
		} else if c == '.' && !dot {
			dot = true
			p.i++
		} else {
			break
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		j := p.i + 1
		if j < len(p.s) && (p.s[j] == '-' || p.s[j] == '+') {
			j++
		}
		if j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
			for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
				j++
			}
			p.i = j
		}
	}
	if p.i == st {
		return 0, false
	}
	v, ok := parseFloat(p.s[st:p.i])
	return v, ok
}

func (p *pathParser) flag() (bool, bool) {
	p.skipSep()
	if p.i < len(p.s) && (p.s[p.i] == '0' || p.s[p.i] == '1') {
		v := p.s[p.i] == '1'
		p.i++
		return v, true
	}
	return false, false
}

func parseFloat(s string) (float64, bool) {
	var v float64
	neg, i := false, 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		neg = s[i] == '-'
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		v = v*10 + float64(s[i]-'0')
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		f := 0.1
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			v += float64(s[i]-'0') * f
			f /= 10
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		eneg := false
		if i < len(s) && (s[i] == '-' || s[i] == '+') {
			eneg = s[i] == '-'
			i++
		}
		e := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			e = e*10 + int(s[i]-'0')
			i++
		}
		if eneg {
			e = -e
		}
		v *= math.Pow(10, float64(e))
	}
	if i != len(s) {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// parsePath flattens SVG path data into polylines (curves split so that each
// segment is roughly tol long). Unparseable trailing data is ignored.
func parsePath(d string, tol float64) [][]pt {
	p := &pathParser{s: d}
	var subs [][]pt
	var cur []pt
	var cx, cy, sx, sy float64 // current point, subpath start
	var lcx, lcy float64       // last cubic/quad control point for S/T
	var cmd, prev byte
	flush := func() {
		if len(cur) > 1 {
			subs = append(subs, cur)
		}
		cur = nil
	}
	add := func(x, y float64) { cur = append(cur, pt{x, y}); cx, cy = x, y }
	for p.more() {
		c := p.s[p.i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			cmd = c
			p.i++
			if cmd == 'Z' || cmd == 'z' {
				if len(cur) > 0 {
					cx, cy = sx, sy
				}
				flush()
				prev = cmd
				continue
			}
		} else if !p.peekIsNumber() || cmd == 0 {
			break
		} else if cmd == 'M' {
			cmd = 'L'
		} else if cmd == 'm' {
			cmd = 'l'
		}
		rel := cmd >= 'a'
		ox, oy := 0.0, 0.0
		if rel {
			ox, oy = cx, cy
		}
		get := func(n int) ([]float64, bool) {
			v := make([]float64, n)
			for k := range v {
				x, ok := p.number()
				if !ok {
					return nil, false
				}
				v[k] = x
			}
			return v, true
		}
		switch cmd | 0x20 {
		case 'm':
			v, ok := get(2)
			if !ok {
				return subs
			}
			flush()
			cx, cy = v[0]+ox, v[1]+oy
			sx, sy = cx, cy
			add(cx, cy)
		case 'l':
			v, ok := get(2)
			if !ok {
				return subs
			}
			if len(cur) == 0 {
				add(cx, cy)
			}
			add(v[0]+ox, v[1]+oy)
		case 'h':
			v, ok := get(1)
			if !ok {
				return subs
			}
			if len(cur) == 0 {
				add(cx, cy)
			}
			add(v[0]+ox, cy)
		case 'v':
			v, ok := get(1)
			if !ok {
				return subs
			}
			if len(cur) == 0 {
				add(cx, cy)
			}
			add(cx, v[0]+oy)
		case 'c', 's':
			var x1, y1, x2, y2, x, y float64
			if cmd|0x20 == 'c' {
				v, ok := get(6)
				if !ok {
					return subs
				}
				x1, y1, x2, y2, x, y = v[0]+ox, v[1]+oy, v[2]+ox, v[3]+oy, v[4]+ox, v[5]+oy
			} else {
				v, ok := get(4)
				if !ok {
					return subs
				}
				if prev|0x20 == 'c' || prev|0x20 == 's' {
					x1, y1 = 2*cx-lcx, 2*cy-lcy
				} else {
					x1, y1 = cx, cy
				}
				x2, y2, x, y = v[0]+ox, v[1]+oy, v[2]+ox, v[3]+oy
			}
			if len(cur) == 0 {
				add(cx, cy)
			}
			x0, y0 := cx, cy
			n := steps(x0, y0, x1, y1, x2, y2, x, y, tol)
			for k := 1; k <= n; k++ {
				t := float64(k) / float64(n)
				u := 1 - t
				add(u*u*u*x0+3*u*u*t*x1+3*u*t*t*x2+t*t*t*x, u*u*u*y0+3*u*u*t*y1+3*u*t*t*y2+t*t*t*y)
			}
			lcx, lcy = x2, y2
		case 'q', 't':
			var x1, y1, x, y float64
			if cmd|0x20 == 'q' {
				v, ok := get(4)
				if !ok {
					return subs
				}
				x1, y1, x, y = v[0]+ox, v[1]+oy, v[2]+ox, v[3]+oy
			} else {
				v, ok := get(2)
				if !ok {
					return subs
				}
				if prev|0x20 == 'q' || prev|0x20 == 't' {
					x1, y1 = 2*cx-lcx, 2*cy-lcy
				} else {
					x1, y1 = cx, cy
				}
				x, y = v[0]+ox, v[1]+oy
			}
			if len(cur) == 0 {
				add(cx, cy)
			}
			x0, y0 := cx, cy
			n := steps(x0, y0, x1, y1, x1, y1, x, y, tol)
			for k := 1; k <= n; k++ {
				t := float64(k) / float64(n)
				u := 1 - t
				add(u*u*x0+2*u*t*x1+t*t*x, u*u*y0+2*u*t*y1+t*t*y)
			}
			lcx, lcy = x1, y1
		case 'a':
			rx, ok1 := p.number()
			ry, ok2 := p.number()
			rot, ok3 := p.number()
			la, ok4 := p.flag()
			sw, ok5 := p.flag()
			x, ok6 := p.number()
			y, ok7 := p.number()
			if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7) {
				return subs
			}
			if len(cur) == 0 {
				add(cx, cy)
			}
			for _, q := range arcPoints(cx, cy, rx, ry, rot, la, sw, x+ox, y+oy, tol) {
				add(q.X, q.Y)
			}
		default:
			flush()
			return subs
		}
		prev = cmd
	}
	flush()
	return subs
}

func steps(x0, y0, x1, y1, x2, y2, x3, y3, tol float64) int {
	l := math.Hypot(x1-x0, y1-y0) + math.Hypot(x2-x1, y2-y1) + math.Hypot(x3-x2, y3-y2)
	n := int(math.Ceil(l / tol))
	if n < 6 {
		n = 6
	}
	if n > 120 {
		n = 120
	}
	return n
}

// arcPoints converts an SVG endpoint arc to polyline points (SVG spec F.6.5).
func arcPoints(x1, y1, rx, ry, rotDeg float64, large, sweep bool, x2, y2, tol float64) []pt {
	if x1 == x2 && y1 == y2 {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []pt{{x2, y2}}
	}
	phi := rotDeg * math.Pi / 180
	cp, sp := math.Cos(phi), math.Sin(phi)
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p, y1p := cp*dx+sp*dy, -sp*dx+cp*dy
	lam := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry)
	if lam > 1 {
		s := math.Sqrt(lam)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cxp, cyp := co*rx*y1p/ry, -co*ry*x1p/rx
	cx, cy := cp*cxp-sp*cyp+(x1+x2)/2, sp*cxp+cp*cyp+(y1+y2)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	th1 := ang(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dth := ang((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dth > 0 {
		dth -= 2 * math.Pi
	} else if sweep && dth < 0 {
		dth += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dth) * math.Max(rx, ry) / tol))
	if n < 8 {
		n = 8
	}
	if n > 200 {
		n = 200
	}
	out := make([]pt, 0, n)
	for k := 1; k <= n; k++ {
		t := th1 + dth*float64(k)/float64(n)
		x, y := rx*math.Cos(t), ry*math.Sin(t)
		out = append(out, pt{cp*x - sp*y + cx, sp*x + cp*y + cy})
	}
	out[len(out)-1] = pt{x2, y2}
	return out
}
