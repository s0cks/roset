package render

import (
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"roset/internal/codec"
	"roset/internal/layout"
	"roset/internal/spec"
)

var (
	ink   = color.NRGBA{22, 22, 28, 255}
	paper = color.NRGBA{248, 246, 238, 255}
)

func sample(t testing.TB, id int, st layout.Style) *layout.Layout {
	v, ok := spec.Lookup(id)
	if !ok {
		t.Fatalf("no version %d", id)
	}
	payload := make([]byte, v.Capacity())
	rand.New(rand.NewSource(int64(id))).Read(payload)
	c, err := codec.Encode(v, codec.ModeSHA256Tag, payload)
	if err != nil {
		t.Fatal(err)
	}
	return layout.New(v, c, st)
}

// ROSET_SAMPLES=/tmp go test ./internal/render -run Samples
func TestSamples(t *testing.T) {
	dir := os.Getenv("ROSET_SAMPLES")
	if dir == "" {
		t.Skip("set ROSET_SAMPLES=<dir> to write sample PNG/SVG files")
	}
	for _, id := range []int{1, 6, 9} {
		for _, st := range []layout.Style{layout.StyleRuns, layout.StyleDots} {
			l := sample(t, id, st)
			name := filepath.Join(dir, "roset-v"+string(rune('0'+id))+map[layout.Style]string{layout.StyleRuns: "-runs", layout.StyleDots: "-dots"}[st])
			f, _ := os.Create(name + ".png")
			png.Encode(f, PNG(l, 1000, ink, paper, nil))
			f.Close()
			os.WriteFile(name+".svg", []byte(SVG(l, "#16161c", "#faf8f0", nil)), 0o644)
		}
	}
}
