package decode

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"strconv"
	"testing"

	"roset/internal/layout"
)

// ROSET_ENVELOPE=1 [ROSET_ENVELOPE_VER=6] go test ./internal/decode -run Envelope -v
//
// Prints how far each degradation can go before a symbol of that version stops
// decoding. Compare versions of the same patch radius to see what extra levels
// per cell cost: 1 vs 6 (K=5, 2 vs 4 levels), 9 is the 80-byte K=5 symbol.
func TestEnvelope(t *testing.T) {
	if os.Getenv("ROSET_ENVELOPE") == "" {
		t.Skip("set ROSET_ENVELOPE=1 to map the robustness envelope")
	}
	ver := 6
	if v := os.Getenv("ROSET_ENVELOPE_VER"); v != "" {
		ver, _ = strconv.Atoi(v)
	}
	try := func(img image.Image, want sym) string {
		res, _, err := Decode(img, Options{})
		if err == nil && bytes.Equal(res.Payload, want.payload) {
			return fmt.Sprintf("ok(rs=%d)", res.Corrected)
		}
		if err == nil {
			return "WRONG"
		}
		return "fail"
	}
	s := makeSym(t, ver, layout.StyleRuns, 1000, 31)
	fmt.Printf("version %d: K=%d, %d levels, RS(%d,%d)\n", s.v.ID, s.v.K, s.v.Levels(), s.v.N, s.v.Payload)
	line := func(name string, vals []float64, f func(v float64) image.Image) {
		out := name + ":"
		for _, v := range vals {
			out += fmt.Sprintf("  %g=%s", v, try(f(v), s))
		}
		fmt.Println(out)
	}
	line("image size px          ", []float64{300, 240, 200, 170, 140, 120, 100}, func(v float64) image.Image {
		return affine(s.img, 17, v/1000, 0, int(v), 0, 0)
	})
	line("blur sigma @500px      ", []float64{1, 1.5, 2, 2.5, 3, 4}, func(v float64) image.Image {
		return blur(affine(s.img, 9, 0.5, 0, 500, 0, 0), v)
	})
	line("noise sigma @500px     ", []float64{10, 20, 30, 45, 60, 90}, func(v float64) image.Image {
		return noise(affine(s.img, 9, 0.5, 0, 500, 0, 0), v, 4)
	})
	line("jpeg quality @500px    ", []float64{60, 40, 25, 15, 8, 4}, func(v float64) image.Image {
		return jpegRT(affine(s.img, 9, 0.5, 0, 500, 0, 0), int(v))
	})
	line("lighting gradient      ", []float64{0.2, 0.35, 0.5, 0.65}, func(v float64) image.Image {
		return gradient(affine(s.img, 0, 0.5, 0, 500, 0, 0), v)
	})
	line("petals wiped (of 6)    ", []float64{1, 2, 3}, func(v float64) image.Image {
		img := affine(s.img, 0, 1, 0, 1000, 0, 0)
		for p := 0; p < int(v); p++ {
			x, y, r := s.petalCentre(p)
			fillDisc(img, x, y, r)
		}
		return img
	})
	line("tilt degrees           ", []float64{30, 40, 50, 55, 60, 65}, func(v float64) image.Image {
		return tilt(s.img, v, 1000)
	})
}
