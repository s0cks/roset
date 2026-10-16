# roset

Encode a SHA-256 digest as a concentric **ring-and-dot code** (think QR, but circular), and decode it
back from a screenshot, scan or phone photo. Pure Go, standard library only, single static binary.

```text
roset encode -o code.png  976da1b3...2a56            # digest as hex
roset encode -o code.svg  --file release.tar.gz       # hash a file, then encode it
roset encode -o tagged.png --file f.txt --tag "release-2026.10"   # digest + up to 48 bytes of data
roset encode --logo seal.svg -o code.png              # logo (SVG/PNG/JPEG/GIF) in the centre
roset decode photo.jpg                                # prints the digest
roset decode --verify <hex> photo.jpg                 # exit 0 match, 3 mismatch, 1 not found
roset decode --json --debug-dir dbg/ photo.jpg        # JSON record + bin/overlay/polar diagnostics
```

`--style runs` (default) merges adjacent dots into single capsule strokes; `--style dots` draws every
dot separately. Both carry identical data. `--fg/--bg` recolour the symbol (the decoder also accepts
light-on-dark and mirrored images).

## Logos in the centre

`--logo <file>` reserves a clear circular zone in the middle of the symbol and puts the logo there.
The zone is *not* part of the data, so a logo costs no error-correction budget (unlike a QR logo, which
overwrites modules). The finder becomes two rings around the zone, so logo symbols are separate versions:

| `--logo-size` | Zone radius | Zone diameter vs symbol width | Versions (32 B / 80 B payload) |
|---|---|---|---|
| small | 4.0 units | ~16% | 3 / 4 |
| medium (default) | 5.5 | ~20% | 5 / 6 |
| large | 7.0 | ~25% | 7 / 8 |

Larger zones barely enlarge the symbol (outer rings are longer, so fewer rings are needed).

- **Fit:** by default the smallest circle enclosing the logo's *visible ink* is scaled to fill 97% of the zone,
  so round seals fill it, off-centre artwork is re-centred, and rectangular logos fit with nothing clipped.
  `--logo-fit viewbox` fits the whole image rectangle instead; `--logo-scale 0.8` shrinks, `1.2` enlarges.
- **Backdrop:** `--logo-bg #rrggbb` sets the colour behind the logo (default: the symbol background). A logo
  with light strokes (e.g. `#CECDC3`) vanishes on a light symbol; use a dark theme (`--bg #100F0F --fg #CECDC3`)
  or a dark backdrop (`--logo-bg #100F0F`).
- **SVG output** embeds an SVG logo as live vector (clipped to the zone); PNG/JPEG logos are embedded as data URIs.
- **PNG output with an SVG logo** uses a built-in mini rasterizer (`internal/svgmini`) because the Go standard
  library cannot render SVG. It draws filled shapes: path (all commands incl. curves and arcs), rect, circle,
  ellipse, polygon, polyline, groups, transforms, fill / fill-rule / opacity. It is checked against `cairosvg`
  on a real logo (mean difference 0.05/255). Anything it cannot draw (strokes, gradients, text, CSS `<style>`,
  `<use>`) prints a **warning**; convert those to outlines, supply a PNG, or use SVG output (which embeds
  the logo verbatim and renders everything).
- **Safety:** SVG logos containing `<script>`, event handlers, `<!ENTITY>`, `<foreignObject>` or external
  `href`s are refused rather than embedded.

## Symbol layout

Units are ring pitches; angle runs clockwise from "up". The picture, centre outward:

| Zone | Radius (units) | Purpose |
|---|---|---|
| Bullseye finder | 0-1.5, 2.5-3.5, 4.5-5.5 ink (plain); two rings at LogoR+1..+2 and +3..+4 (logo versions) | rotation-invariant detection; centre + scale + view geometry |
| Logo zone | r < LogoR (logo versions only) | clear area for the logo; never sampled |
| Header rings x3 | 7, 8, 9 (+ shift for logo versions; 32 slots each) | version / mode / length / CRC-16, three rotated copies, majority vote |
| Data rings | 11 ... 10+K (+ shift) | payload cells; slot count per ring chosen so cells stay ~1.2 units wide |
| Boundary ring | 12+K (+ shift; solid, 0.8 wide) | fixes scale / shear / perspective |
| Sync dot | boundary + 1.3, angle 0 | defines slot 0 (orientation) |
| Quiet zone | 2 units | clear margin |

**Header (32 bits):** `version(4) | mode(4) | length(8) | crc16(16)`.

**Modes** (the "mode bit" you asked for, widened to 4 bits so it can grow):

| Mode | Meaning | Status |
|---|---|---|
| 0 | raw SHA-256 (32 bytes) | implemented |
| 1 | SHA-256 + tag bytes | implemented |
| 2-14 | reserved (e.g. algorithm-tagged digests) | decoder returns the raw payload, never guesses |
| 15 | escape: extended header follows | reserved |

**Versions:** odd versions carry 32 bytes (RS(64,32), corrects 16 bad bytes); even versions carry 80 bytes
(RS(128,80), corrects 24). 1-2 have the plain bullseye; 3-8 have a logo zone (see above). Data rings are
chosen as the fewest that hold the codeword. The encoder picks the smallest version that fits.

**Payload pipeline:** zero-pad to capacity -> Reed-Solomon over GF(256) (poly 0x11d) -> split into
8-cell groups -> deterministic group shuffle (a smudge hits few bytes) -> XOR with a fixed PRBS
(no big blank or solid areas) -> cells.

## How decoding works (all pure Go, `internal/detect`, `internal/decode`)

1. Grayscale, downscale to <=1600 px, adaptive (Bradley) or Otsu threshold.
2. 8-connected components; find three nested components sharing a centroid with the bullseye's
   area ratios (1 : 2.67 : 4.44), or, for logo versions, two nested rings (the logo's own ink is never
   used for detection, and the boundary ring must also be found).
3. **Projective frame** from two concentric circles (bullseye ring + boundary ring): the generalized
   eigenvector of their image conics is the projected centre, the pole-polar relation gives the
   vanishing line, dividing by it rectifies the view to affine, and an ellipse fit gives the rest.
   Fallbacks: affine boundary-ring frame, then affine bullseye-only frame.
4. Find the sync dot's angle, sample every cell in the symbol's own polar frame (5-point vote).
5. Header CRC -> unshuffle/de-whiten -> Reed-Solomon (Berlekamp-Massey + Chien + Forney).
6. Any failure falls through to the next thresholding, polarity, frame, slot direction (mirror),
   finder candidate and version.

## Measured robustness (synthetic, v1 symbol, `RINGSUM_ENVELOPE=1 go test ./internal/decode -run Envelope -v`)

| Degradation | Still decodes up to |
|---|---|
| Resolution | ~90 px across (about 2.2 px per unit) |
| Gaussian blur | sigma 4 px at 500 px width |
| Sensor-style noise | sigma 90 (of 255) at 500 px |
| JPEG | quality 4 at 420 px |
| Occlusion (smudge over data rings) | ~60 degrees of arc; 80 degrees fails |
| Perspective tilt (pinhole, f = 1.2 x width) | 50 degrees; 60 fails |
| Rotation / mirror / inverted colours / clutter / off-centre | all pass |

Logo symbols (version 5, the seal above) measure within a hair of that: ~110 px minimum, the same blur, noise,
JPEG and smudge limits, and tilt up to 40 degrees (50 fails). The logo suite also covers every logo version,
light and dark themes, a dark backdrop, and adversarial logos (solid black disc, finder-lookalike rings,
fine checkerboard, full-bleed noise, wide wordmark), alone and under rotation + tilt + blur + noise + JPEG.

**Important caveat:** these are *simulated* distortions of rendered symbols. I have not tested real
phone photos or printed-and-photographed symbols (no camera in the build environment). Expect real
lens distortion, glare and motion blur to cost margin; use `--debug-dir` on any failing photo and
send me `bin.png` / `overlay.png` if you want it tuned.

## Not done yet

- Decoding from SVG files (rasterize first: `rsvg-convert`, Inkscape, a browser). SVG *output* is verified two
  ways: a test compares its geometry to the layout model, and symbols (with an embedded logo) rasterized by an
  independent renderer (`cairosvg`) decode correctly.
- Stroked / gradient / text logos in **PNG** output (see Logos; they work in SVG output).
- Soft-decision / erasure decoding (would roughly double the correctable damage for free).
- A pluggable GoCV/OpenCV backend; the pure-Go detector is the only one.
- Modes 2-15, symbol versions beyond v2, colour/size-coded dots (more bits per cell).
- Strong lens distortion (barrel/pincushion) correction.

## Development

```text
go test ./...                                   # codec, rendering (incl. SVG-vs-layout), decoder suite
RINGSUM_SAMPLES=/tmp go test ./internal/render -run Samples   # write sample PNG/SVG
RINGSUM_ENVELOPE=1 go test ./internal/decode -run Envelope -v # print the robustness table
```

Layout: `internal/spec` (geometry, versions) - `internal/codec` (GF/RS/CRC/header/framing) -
`internal/layout` (what is drawn where) - `internal/render` (PNG, SVG) - `internal/logo` (load, auto-fit,
zone rendering, SVG embedding) - `internal/svgmini` (SVG rasterizer) - `internal/detect` (CV primitives) -
`internal/decode` (pipeline, debug dumps) - `cmd/roset` (CLI). `testdata/logo.svg` is the sample seal.
