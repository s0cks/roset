package detect

// Bin is a binary image; 1 = ink (dark).
type Bin struct {
	W, H int
	Pix  []uint8
}

func (b *Bin) Dark(x, y int) bool {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return false
	}
	return b.Pix[y*b.W+x] != 0
}

// Bradley is adaptive (local-mean) thresholding using an integral image:
// a pixel is ink if it is more than t (fraction) darker than its win x win neighbourhood.
func Bradley(g *Gray, win int, t float64) *Bin {
	if win < 3 {
		win = 3
	}
	half := win / 2
	it := NewIntegral(g)
	b := &Bin{W: g.W, H: g.H, Pix: make([]uint8, g.W*g.H)}
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			mean := it.Mean(x-half, y-half, x+half+1, y+half+1)
			if float64(g.Pix[y*g.W+x]) < mean*(1-t) {
				b.Pix[y*g.W+x] = 1
			}
		}
	}
	return b
}

// Otsu is global thresholding at Otsu's optimal level.
func Otsu(g *Gray) *Bin {
	var hist [256]float64
	for _, v := range g.Pix {
		i := int(v + 0.5)
		if i > 255 {
			i = 255
		}
		hist[i]++
	}
	total := float64(len(g.Pix))
	var sum float64
	for i, c := range hist {
		sum += float64(i) * c
	}
	var wB, sumB, best float64
	th := 128
	for i := 0; i < 256; i++ {
		wB += hist[i]
		if wB == 0 {
			continue
		}
		wF := total - wB
		if wF == 0 {
			break
		}
		sumB += float64(i) * hist[i]
		mB, mF := sumB/wB, (sum-sumB)/wF
		if v := wB * wF * (mB - mF) * (mB - mF); v > best {
			best, th = v, i
		}
	}
	b := &Bin{W: g.W, H: g.H, Pix: make([]uint8, g.W*g.H)}
	for i, v := range g.Pix {
		if int(v+0.5) <= th {
			b.Pix[i] = 1
		}
	}
	return b
}
