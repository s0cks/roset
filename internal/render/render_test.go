package render

import (
	"crypto/sha256"
	"image/color"
	"image/png"
	"os"
	"testing"

	"roset/internal/codec"
	"roset/internal/layout"
	"roset/internal/spec"
)

func TestWriteSamples(t *testing.T) {
	if os.Getenv("RINGSUM_SAMPLES") == "" {
		t.Skip()
	}
	h := sha256.Sum256([]byte("hello roset"))
	c, _ := codec.Encode(spec.Versions[0], codec.ModeRawSHA256, h[:])
	for name, st := range map[string]layout.Style{"dots": layout.StyleDots, "runs": layout.StyleRuns} {
		l := layout.New(spec.Versions[0], c, st)
		img := PNG(l, 900, color.NRGBA{20, 20, 24, 255}, color.NRGBA{250, 248, 240, 255}, nil)
		f, _ := os.Create(os.Getenv("RINGSUM_SAMPLES") + "/sample_" + name + ".png")
		png.Encode(f, img)
		f.Close()
		os.WriteFile(os.Getenv("RINGSUM_SAMPLES")+"/sample_"+name+".svg", []byte(SVG(l, "#141418", "#faf8f0", nil)), 0o644)
	}
}
