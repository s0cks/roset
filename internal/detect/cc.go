package detect

type Comp struct {
	Area                   int
	SumX, SumY             float64
	MinX, MinY, MaxX, MaxY int
}

func (c Comp) CX() float64 { return c.SumX / float64(c.Area) }
func (c Comp) CY() float64 { return c.SumY / float64(c.Area) }
func (c Comp) BW() int     { return c.MaxX - c.MinX + 1 }
func (c Comp) BH() int     { return c.MaxY - c.MinY + 1 }

// Labels is an 8-connected component labelling of ink pixels. Label 0 is background;
// component with label l is Comps[l-1].
type Labels struct {
	W, H  int
	L     []int32
	Comps []Comp
}

func (lb *Labels) Comp(l int32) Comp { return lb.Comps[l-1] }

func Label(b *Bin) *Labels {
	w, h := b.W, b.H
	lab := make([]int32, w*h)
	parent := []int32{0}
	find := func(x int32) int32 {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int32) int32 {
		ra, rb := find(a), find(b)
		if ra == rb {
			return ra
		}
		if ra < rb {
			parent[rb] = ra
			return ra
		}
		parent[ra] = rb
		return rb
	}
	nbrs := [4][2]int{{-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if b.Pix[y*w+x] == 0 {
				continue
			}
			var cur int32
			for _, d := range nbrs {
				nx, ny := x+d[0], y+d[1]
				if nx < 0 || nx >= w || ny < 0 {
					continue
				}
				l := lab[ny*w+nx]
				if l == 0 {
					continue
				}
				if cur == 0 {
					cur = find(l)
				} else {
					cur = union(cur, l)
				}
			}
			if cur == 0 {
				cur = int32(len(parent))
				parent = append(parent, cur)
			}
			lab[y*w+x] = cur
		}
	}
	out := &Labels{W: w, H: h, L: lab}
	remap := make([]int32, len(parent))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l := lab[y*w+x]
			if l == 0 {
				continue
			}
			r := find(l)
			id := remap[r]
			if id == 0 {
				out.Comps = append(out.Comps, Comp{MinX: x, MinY: y, MaxX: x, MaxY: y})
				id = int32(len(out.Comps))
				remap[r] = id
			}
			lab[y*w+x] = id
			c := &out.Comps[id-1]
			c.Area++
			c.SumX += float64(x)
			c.SumY += float64(y)
			c.MinX, c.MaxX = min(c.MinX, x), max(c.MaxX, x)
			c.MinY, c.MaxY = min(c.MinY, y), max(c.MaxY, y)
		}
	}
	return out
}
