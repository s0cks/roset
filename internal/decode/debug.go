package decode

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// WriteDebug dumps bin.png (the thresholded image) and overlay.png (the detected
// border corners and every sampled cell centre: header slots red = 1 / blue = 0,
// data cells shaded by decoded level).
func (r *Report) WriteDebug(dir string) error {
	if !r.haveAtt {
		return errors.New("no attempt reached the sampling stage; nothing to dump")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	g, bin := r.gray, r.bin
	b := image.NewGray(image.Rect(0, 0, bin.W, bin.H))
	for i, v := range bin.Pix {
		if v == 0 {
			b.Pix[i] = 255
		}
	}
	if err := savePNG(filepath.Join(dir, "bin.png"), b); err != nil {
		return err
	}
	ov := image.NewRGBA(image.Rect(0, 0, g.W, g.H))
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			v := uint8(g.Pix[y*g.W+x])
			ov.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	dot := func(x, y float64, c color.RGBA, rad int) {
		cx, cy := int(x+0.5), int(y+0.5)
		for dy := -rad; dy <= rad; dy++ {
			for dx := -rad; dx <= rad; dx++ {
				if dx*dx+dy*dy <= rad*rad && image.Pt(cx+dx, cy+dy).In(ov.Bounds()) {
					ov.SetRGBA(cx+dx, cy+dy, c)
				}
			}
		}
	}
	for _, s := range r.samples {
		c := color.RGBA{0, 90, 255, 255}
		if s.Level > 0 {
			c = color.RGBA{255, uint8(max(0, 90-s.Level*30)), 0, 255}
		}
		dot(s.X, s.Y, c, 1)
	}
	for i, c := range r.corners {
		dot(c[0], c[1], color.RGBA{0, 200, 0, 255}, 3+i) // growing size shows the corner order
	}
	return savePNG(filepath.Join(dir, "overlay.png"), ov)
}

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
