package decode

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"testing"

	"roset/internal/layout"
	"roset/internal/spec"
)

// RINGSUM_ENVELOPE=1 go test ./internal/decode -run Envelope -v
func TestEnvelope(t *testing.T) {
	if os.Getenv("RINGSUM_ENVELOPE") == "" {
		t.Skip("set RINGSUM_ENVELOPE=1 to map the robustness envelope")
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
	s := makeSym(t, 1, layout.StyleRuns, 1000, 31)
	if os.Getenv("RINGSUM_ENVELOPE_LOGO") != "" { // measure a medium-logo symbol (version 5) instead
		lg := loadLogo(t, "../../testdata/logo.svg")
		s = makeLogoSym(t, 5, layout.StyleRuns, 1000, 31, lg, ink, paper, nil)
	}
	line := func(name string, vals []float64, f func(v float64) image.Image) {
		out := name + ":"
		for _, v := range vals {
			out += fmt.Sprintf("  %g=%s", v, try(f(v), s))
		}
		fmt.Println(out)
	}
	line("image size px          ", []float64{200, 160, 130, 110, 90, 70}, func(v float64) image.Image {
		return affine(s.img, 17, v/1000, 0, int(v), 0, 0)
	})
	line("blur sigma @500px      ", []float64{2, 3, 4, 5, 6}, func(v float64) image.Image {
		return blur(affine(s.img, 9, 0.5, 0, 500, 0, 0), v)
	})
	line("noise sigma @500px     ", []float64{30, 50, 70, 90}, func(v float64) image.Image {
		return noise(affine(s.img, 9, 0.5, 0, 500, 0, 0), v, 4)
	})
	line("jpeg quality @420px    ", []float64{40, 25, 15, 8, 4}, func(v float64) image.Image {
		return jpegRT(affine(s.img, 9, 0.42, 0, 420, 0, 0), int(v))
	})
	line("smudge degrees (data)  ", []float64{30, 60, 80, 100, 130}, func(v float64) image.Image {
		img := affine(s.img, 0, 1, 0, 1000, 0, 0)
		u := 1000 / (2 * s.verRadius())
		r0, r1 := 10.5, 18.6
		if os.Getenv("RINGSUM_ENVELOPE_LOGO") != "" {
			u = 1000 / (2 * spec.Versions[4].TotalRadius())
			r0, r1 = 15, 20.5
		}
		fillWedge(img, 499.5, 499.5, r0*u, r1*u, 0.5, 0.5+v*3.14159/180)
		return img
	})
	line("tilt degrees           ", []float64{40, 50, 60, 65, 70}, func(v float64) image.Image {
		return tilt(s.img, v, 1000)
	})
}
