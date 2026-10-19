// Package render draws a layout as PNG (rasterized) or SVG (vector).
package render

import (
	"image"
	"image/color"
	"runtime"
	"sync"

	"roset/internal/layout"
	"roset/internal/logo"
)

// Options customises PNG output.
type Options struct {
	Logo   *logo.Logo   // composited into the clear centre disc
	LogoBG *color.NRGBA // backdrop behind the logo; nil = the symbol background
}

// PNG rasterizes the layout into a size x size image with 3x3 supersampling.
// An ink coverage t blends the background towards the ink colour.
func PNG(l *layout.Layout, size int, fg, bg color.NRGBA, opt *Options) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 3
	scale := float64(size) / (2 * l.G.Half)
	half := float64(size) / 2

	var zone *image.NRGBA
	var zLen int
	zR := l.G.LogoR
	zbg := bg
	if opt != nil && opt.Logo != nil {
		zLen = (int(2*zR*scale) + 1) * ss
		zone = opt.Logo.RenderZone(zLen, zR)
		if opt.LogoBG != nil {
			zbg = *opt.LogoBG
		}
	}

	workers := runtime.NumCPU()
	var wg sync.WaitGroup
	rows := make(chan int, size)
	for y := 0; y < size; y++ {
		rows <- y
	}
	close(rows)
	mix := func(t float64) [3]float64 {
		return [3]float64{
			float64(bg.R) + (float64(fg.R)-float64(bg.R))*t,
			float64(bg.G) + (float64(fg.G)-float64(bg.G))*t,
			float64(bg.B) + (float64(fg.B)-float64(bg.B))*t,
		}
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for py := range rows {
				for px := 0; px < size; px++ {
					var sr, sg, sb float64
					for sy := 0; sy < ss; sy++ {
						for sx := 0; sx < ss; sx++ {
							x := (float64(px) + (float64(sx)+0.5)/ss - half) / scale
							y := (float64(py) + (float64(sy)+0.5)/ss - half) / scale
							var c [3]float64
							if zone != nil && x*x+y*y <= zR*zR {
								ix := min(zLen-1, max(0, int((x+zR)/(2*zR)*float64(zLen))))
								iy := min(zLen-1, max(0, int((y+zR)/(2*zR)*float64(zLen))))
								i := zone.PixOffset(ix, iy)
								a := float64(zone.Pix[i+3]) / 255
								c = [3]float64{
									float64(zone.Pix[i])*a + float64(zbg.R)*(1-a),
									float64(zone.Pix[i+1])*a + float64(zbg.G)*(1-a),
									float64(zone.Pix[i+2])*a + float64(zbg.B)*(1-a),
								}
							} else {
								c = mix(l.Ink(x, y))
							}
							sr, sg, sb = sr+c[0], sg+c[1], sb+c[2]
						}
					}
					n := float64(ss * ss)
					img.SetNRGBA(px, py, color.NRGBA{uint8(sr/n + 0.5), uint8(sg/n + 0.5), uint8(sb/n + 0.5), 255})
				}
			}
		}()
	}
	wg.Wait()
	return img
}
