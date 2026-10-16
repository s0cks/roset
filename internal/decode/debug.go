package decode

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// WriteDebug dumps bin.png (thresholded image), overlay.png (every sampled cell
// centre, red = 1, blue = 0, plus the detected centre/sync) and polar.png (the
// symbol unwrapped so rings are rows and sync is at the left edge).
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
	for _, s := range r.samples {
		c := color.RGBA{40, 90, 255, 255}
		if s.Bit {
			c = color.RGBA{255, 40, 40, 255}
		}
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				ov.SetRGBA(int(math.Round(s.X))+dx, int(math.Round(s.Y))+dy, c)
			}
		}
	}
	green := color.RGBA{0, 200, 0, 255}
	for k := -6; k <= 6; k++ {
		ov.SetRGBA(int(r.Cx)+k, int(r.Cy), green)
		ov.SetRGBA(int(r.Cx), int(r.Cy)+k, green)
	}
	for rho := 0.0; rho < r.ver.SyncRadius()+2; rho += 0.1 { // sync direction ray
		x, y := r.frame.ImgPoint(rho, r.Theta0)
		ov.SetRGBA(int(math.Round(x)), int(math.Round(y)), green)
	}
	if err := savePNG(filepath.Join(dir, "overlay.png"), ov); err != nil {
		return err
	}
	const cols, perUnit = 720, 4
	rows := int((r.ver.SyncRadius() + 2) * perUnit)
	p := image.NewGray(image.Rect(0, 0, cols, rows))
	for row := 0; row < rows; row++ {
		rho := float64(row) / perUnit
		for col := 0; col < cols; col++ {
			phi := r.Theta0 + float64(r.Dir)*2*math.Pi*float64(col)/cols
			x, y := r.frame.ImgPoint(rho, phi)
			p.SetGray(col, row, color.Gray{uint8(g.Bilinear(x, y))})
		}
	}
	return savePNG(filepath.Join(dir, "polar.png"), p)
}

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
