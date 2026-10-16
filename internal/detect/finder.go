package detect

import (
	"math"
	"sort"
)

// Finder is a detected bullseye: three nested ink components (disc, ring, ring)
// sharing a centroid with area ratios close to the spec's 1 : 2.67 : 4.44.
type Finder struct {
	CX, CY float64
	Labels [3]int32
	U      float64 // rough px per unit
	Score  float64 // lower is better
}

func FindFinders(lab *Labels, maxOut int) []Finder {
	type cand struct {
		l int32
		c Comp
	}
	var cs []cand
	limit := lab.W * lab.H / 4
	for i, c := range lab.Comps {
		if c.Area >= 10 && c.Area <= limit {
			cs = append(cs, cand{int32(i + 1), c})
		}
	}
	if len(cs) > 3000 {
		sort.Slice(cs, func(i, j int) bool { return cs[i].c.Area > cs[j].c.Area })
		cs = cs[:3000]
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].c.Area < cs[j].c.Area })

	var out []Finder
	for _, a := range cs {
		bw, bh := float64(a.c.BW()), float64(a.c.BH())
		asp := math.Max(bw, bh) / math.Min(bw, bh)
		fill := float64(a.c.Area) / (bw * bh)
		if asp > 2.2 || fill < 0.45 || fill > 0.95 {
			continue
		}
		ra := math.Sqrt(float64(a.c.Area) / math.Pi)
		ax, ay := a.c.CX(), a.c.CY()
		best := Finder{Score: math.Inf(1)}
		for _, b := range cs {
			ratio := float64(b.c.Area) / float64(a.c.Area)
			if ratio < 1.6 || ratio > 4.6 {
				continue
			}
			dab := math.Hypot(b.c.CX()-ax, b.c.CY()-ay)
			if dab > 0.5*ra {
				continue
			}
			for _, c := range cs {
				r2 := float64(c.c.Area) / float64(b.c.Area)
				if r2 < 1.2 || r2 > 2.4 {
					continue
				}
				dac := math.Hypot(c.c.CX()-ax, c.c.CY()-ay)
				if dac > 0.6*ra {
					continue
				}
				s := (dab+dac)/ra + math.Abs(math.Log(ratio/2.667)) + math.Abs(math.Log(r2/1.667))
				if s < best.Score {
					wa, wb, wc := float64(a.c.Area), float64(b.c.Area), float64(c.c.Area)
					tot := wa + wb + wc
					best = Finder{
						CX:     (a.c.SumX + b.c.SumX + c.c.SumX) / tot,
						CY:     (a.c.SumY + b.c.SumY + c.c.SumY) / tot,
						Labels: [3]int32{a.l, b.l, c.l},
						U:      ra / 1.5,
						Score:  s,
					}
				}
			}
		}
		if !math.IsInf(best.Score, 1) {
			out = append(out, best)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score < out[j].Score })
	if maxOut > 0 && len(out) > maxOut {
		out = out[:maxOut]
	}
	return out
}

// FindFinderPairs finds the logo-symbol finder: two nested rings sharing a
// centroid (the middle is a logo, so there is no solid disc to match). bands
// are the version's two finder bands. Labels = {0, inner ring, outer ring}.
func FindFinderPairs(lab *Labels, bands [][2]float64, maxOut int) []Finder {
	if len(bands) != 2 {
		return nil
	}
	a0, a1, b0, b1 := bands[0][0], bands[0][1], bands[1][0], bands[1][1]
	areaA, areaB := math.Pi*(a1*a1-a0*a0), math.Pi*(b1*b1-b0*b0)
	wantArea, wantBox := areaB/areaA, b1/a1
	fillWant := areaA / (4 * a1 * a1)

	type cand struct {
		l int32
		c Comp
	}
	var cs []cand
	limit := lab.W * lab.H / 4
	for i, c := range lab.Comps {
		if c.Area >= 40 && c.Area <= limit {
			cs = append(cs, cand{int32(i + 1), c})
		}
	}
	if len(cs) > 3000 {
		sort.Slice(cs, func(i, j int) bool { return cs[i].c.Area > cs[j].c.Area })
		cs = cs[:3000]
	}
	var out []Finder
	for _, a := range cs {
		bw, bh := float64(a.c.BW()), float64(a.c.BH())
		if math.Max(bw, bh)/math.Min(bw, bh) > 2.2 {
			continue
		}
		if fill := float64(a.c.Area) / (bw * bh); fill < 0.5*fillWant || fill > 1.8*fillWant {
			continue
		}
		ua := math.Sqrt(float64(a.c.Area) / areaA)
		ax, ay := a.c.CX(), a.c.CY()
		best := Finder{Score: math.Inf(1)}
		for _, b := range cs {
			ratio := float64(b.c.Area) / float64(a.c.Area)
			if ratio < 0.7*wantArea || ratio > 1.4*wantArea {
				continue
			}
			boxRatio := math.Max(float64(b.c.BW()), float64(b.c.BH())) / math.Max(bw, bh)
			if boxRatio < 0.85*wantBox || boxRatio > 1.2*wantBox {
				continue
			}
			d := math.Hypot(b.c.CX()-ax, b.c.CY()-ay)
			if d > 0.6*ua {
				continue
			}
			sc := d/ua + math.Abs(math.Log(ratio/wantArea)) + math.Abs(math.Log(boxRatio/wantBox))
			if sc < best.Score {
				tot := float64(a.c.Area + b.c.Area)
				best = Finder{
					CX:     (a.c.SumX + b.c.SumX) / tot,
					CY:     (a.c.SumY + b.c.SumY) / tot,
					Labels: [3]int32{0, a.l, b.l},
					U:      ua,
					Score:  sc,
				}
			}
		}
		if !math.IsInf(best.Score, 1) {
			out = append(out, best)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score < out[j].Score })
	if maxOut > 0 && len(out) > maxOut {
		out = out[:maxOut]
	}
	return out
}
