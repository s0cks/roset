// roset encodes a SHA-256 digest as a concentric ring-and-dot symbol and
// decodes it back from an image (scan, screenshot or photo).
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"roset/internal/codec"
	"roset/internal/decode"
	"roset/internal/layout"
	"roset/internal/logo"
	"roset/internal/render"
	"roset/internal/spec"
)

const usage = `roset - encode a SHA-256 as a ring-and-dot code, and read it back from images

Usage:
  roset encode [flags] <sha256-hex | ->      encode a digest (- reads hex from stdin)
  roset encode [flags] --file <path>         hash a file, then encode its digest
  roset decode [flags] <image | ->           find and decode a symbol in a PNG/JPEG/GIF

encode flags:
  -o <file>        output (.svg or .png); default: SVG on stdout
  --size <px>      PNG size in pixels (default 1024)
  --style <s>      dots | runs  (runs merges adjacent dots into single strokes; default runs)
  --fg <color>     ink colour as #rrggbb (default #16161c)
  --bg <color>     background colour as #rrggbb (default #faf8f0)
  --tag <text>     attach up to 48 bytes of extra data (mode 1, larger symbol)
  --file <path>    hash this file with SHA-256 instead of taking hex
  --logo <file>    put a logo in the centre (SVG, PNG, JPEG or GIF)
  --logo-size <s>  small | medium | large clear zone (default medium: ~20% of the width)
  --logo-fit <f>   auto (fit the logo's visible ink, default) | viewbox (fit the whole image)
  --logo-scale <x> multiply the logo's automatic size (default 1)
  --logo-bg <c>    backdrop colour behind the logo as #rrggbb (default: --bg)

decode flags:
  --verify <hex>   exit 3 unless the decoded digest equals this
  --json           print a JSON record instead of the bare hex digest
  -v               print details (version, mode, RS repairs, frame) on stderr
  --debug-dir <d>  write bin.png, overlay.png and polar.png for diagnosing failures

Exit codes: 0 ok, 1 failure (no symbol / uncorrectable), 2 usage error, 3 --verify mismatch.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "encode":
		code = runEncode(os.Args[2:])
	case "decode":
		code = runDecode(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "roset: unknown command %q\n\n%s", os.Args[1], usage)
		code = 2
	}
	os.Exit(code)
}

func fail(code int, format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "roset: "+format+"\n", a...)
	return code
}

func parseColor(s string) (color.NRGBA, string, error) {
	t := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(t) == 3 {
		t = string([]byte{t[0], t[0], t[1], t[1], t[2], t[2]})
	}
	b, err := hex.DecodeString(t)
	if err != nil || len(b) != 3 {
		return color.NRGBA{}, "", fmt.Errorf("bad colour %q (want #rrggbb)", s)
	}
	return color.NRGBA{b[0], b[1], b[2], 255}, "#" + t, nil
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	out := fs.String("o", "", "")
	size := fs.Int("size", 1024, "")
	styleS := fs.String("style", "runs", "")
	fgS := fs.String("fg", "#16161c", "")
	bgS := fs.String("bg", "#faf8f0", "")
	tag := fs.String("tag", "", "")
	file := fs.String("file", "", "")
	logoPath := fs.String("logo", "", "")
	logoSize := fs.String("logo-size", "medium", "")
	logoFit := fs.String("logo-fit", "auto", "")
	logoScale := fs.Float64("logo-scale", 1, "")
	logoBGS := fs.String("logo-bg", "", "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	style, err := layout.ParseStyle(*styleS)
	if err != nil {
		return fail(2, "%v", err)
	}
	fg, fgCSS, err := parseColor(*fgS)
	if err != nil {
		return fail(2, "%v", err)
	}
	bg, bgCSS, err := parseColor(*bgS)
	if err != nil {
		return fail(2, "%v", err)
	}

	var digest []byte
	switch {
	case *file != "":
		f, err := os.Open(*file)
		if err != nil {
			return fail(1, "%v", err)
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return fail(1, "%v", err)
		}
		digest = h.Sum(nil)
	case fs.NArg() == 1:
		in := fs.Arg(0)
		if in == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fail(1, "%v", err)
			}
			in = string(b)
		}
		digest, err = hex.DecodeString(strings.TrimSpace(strings.ToLower(in)))
		if err != nil || len(digest) != 32 {
			return fail(2, "expected a 64-character SHA-256 hex digest")
		}
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	mode, payload := codec.ModeRawSHA256, digest
	if *tag != "" {
		mode, payload = codec.ModeSHA256Tag, append(append([]byte(nil), digest...), *tag...)
	}
	var lg *logo.Logo
	var logoBG *color.NRGBA
	logoBGCSS := ""
	zoneR := 0.0
	if *logoPath != "" {
		r, ok := spec.LogoSizes[*logoSize]
		if !ok {
			return fail(2, "--logo-size must be small, medium or large")
		}
		zoneR = r
		fit := logo.FitAuto
		switch *logoFit {
		case "auto":
		case "viewbox":
			fit = logo.FitViewBox
		default:
			return fail(2, "--logo-fit must be auto or viewbox")
		}
		if lg, err = logo.Load(*logoPath, fit, *logoScale); err != nil {
			return fail(1, "logo: %v", err)
		}
		if *logoBGS != "" {
			c, css, err := parseColor(*logoBGS)
			if err != nil {
				return fail(2, "%v", err)
			}
			logoBG, logoBGCSS = &c, css
		}
	}
	ver, err := codec.PickVersion(len(payload), zoneR)
	if err != nil {
		return fail(2, "%v", err)
	}
	cells, err := codec.Encode(ver, mode, payload)
	if err != nil {
		return fail(1, "%v", err)
	}
	l := layout.New(ver, cells, style)

	ext := strings.ToLower(filepath.Ext(*out))
	switch {
	case *out == "" || ext == ".svg":
		svg := render.SVG(l, fgCSS, bgCSS, &render.SVGOptions{Logo: lg, LogoBG: logoBGCSS})
		if *out == "" {
			fmt.Print(svg)
		} else if err := os.WriteFile(*out, []byte(svg), 0o644); err != nil {
			return fail(1, "%v", err)
		}
	case ext == ".png":
		if *size < 64 || *size > 8192 {
			return fail(2, "--size must be between 64 and 8192")
		}
		f, err := os.Create(*out)
		if err != nil {
			return fail(1, "%v", err)
		}
		defer f.Close()
		if lg != nil {
			for _, w := range lg.Warnings {
				fmt.Fprintf(os.Stderr, "roset: logo warning: %s\n", w)
			}
		}
		if err := png.Encode(f, render.PNG(l, *size, fg, bg, &render.Options{Logo: lg, LogoBG: logoBG})); err != nil {
			return fail(1, "%v", err)
		}
	default:
		return fail(2, "unsupported output extension %q (use .svg or .png)", ext)
	}
	fmt.Fprintf(os.Stderr, "roset: encoded %d payload bytes as version %d (mode %d, %d rings)\n", len(payload), ver.ID, mode, ver.Rings)
	return 0
}

type record struct {
	Version   int     `json:"version"`
	Mode      int     `json:"mode"`
	Hash      string  `json:"hash,omitempty"`
	Tag       string  `json:"tag,omitempty"`
	TagHex    string  `json:"tag_hex,omitempty"`
	Payload   string  `json:"payload_hex"`
	Corrected int     `json:"rs_corrected_bytes"`
	UnitPx    float64 `json:"unit_px"`
	Frame     string  `json:"frame"`
	Binarizer string  `json:"binarizer"`
}

func printable(b []byte) bool {
	for _, r := range string(b) {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return len(b) > 0
}

func runDecode(args []string) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	verify := fs.String("verify", "", "")
	asJSON := fs.Bool("json", false, "")
	verbose := fs.Bool("v", false, "")
	dbg := fs.String("debug-dir", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	var r io.Reader = os.Stdin
	if p := fs.Arg(0); p != "-" {
		f, err := os.Open(p)
		if err != nil {
			return fail(1, "%v", err)
		}
		defer f.Close()
		r = f
	}
	img, format, err := image.Decode(r)
	if err != nil {
		return fail(1, "cannot read image (PNG, JPEG, GIF supported; rasterize SVG first): %v", err)
	}
	res, rep, derr := decode.Decode(img, decode.Options{})
	if *dbg != "" {
		if err := rep.WriteDebug(*dbg); err != nil {
			fmt.Fprintf(os.Stderr, "roset: debug dump: %v\n", err)
		}
	}
	if derr != nil {
		return fail(1, "%v", derr)
	}
	if *verbose {
		fmt.Fprintf(os.Stderr, "roset: %s image, version %d, mode %d, %d byte(s) repaired by Reed-Solomon\n", format, res.Version, res.Mode, res.Corrected)
		fmt.Fprintf(os.Stderr, "roset: %.1f px/unit, %s frame, %s threshold, inverted=%v, mirrored=%v, %d attempt(s)\n",
			rep.Unit, rep.FrameKind, rep.Binarizer, rep.Inverted, rep.Dir < 0, rep.Attempts)
	}
	hash, tag := res.Hash()
	if res.Mode > codec.ModeSHA256Tag || hash == nil {
		// unknown future mode: expose the raw payload, never guess its meaning
		hash, tag = nil, res.Payload
		fmt.Fprintf(os.Stderr, "roset: mode %d is not understood by this version; showing raw payload\n", res.Mode)
	}
	hexHash := hex.EncodeToString(hash)
	if *verify != "" {
		want := strings.ToLower(strings.TrimSpace(*verify))
		if hexHash != want {
			return fail(3, "MISMATCH: decoded %s, expected %s", hexHash, want)
		}
		fmt.Fprintln(os.Stderr, "roset: digest matches")
	}
	if *asJSON {
		rec := record{Version: res.Version, Mode: res.Mode, Hash: hexHash, Payload: hex.EncodeToString(res.Payload),
			Corrected: res.Corrected, UnitPx: rep.Unit, Frame: rep.FrameKind, Binarizer: rep.Binarizer}
		if len(tag) > 0 {
			rec.TagHex = hex.EncodeToString(tag)
			if printable(tag) {
				rec.Tag = string(tag)
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rec); err != nil {
			return fail(1, "%v", err)
		}
		return 0
	}
	if hash == nil {
		fmt.Println(hex.EncodeToString(tag))
		return 0
	}
	fmt.Println(hexHash)
	if len(tag) > 0 && *verbose {
		if printable(tag) {
			fmt.Fprintf(os.Stderr, "roset: tag: %q\n", tag)
		} else {
			fmt.Fprintf(os.Stderr, "roset: tag (hex): %x\n", tag)
		}
	}
	return 0
}

var _ = errors.New
