# roset

Encode a SHA-256 digest as a **honeycomb code** — a flower of seven hexagonal patches with your logo in the
middle one — and decode it back from a screenshot, scan or phone photo. Pure Go, standard library only,
single static binary.

```text
roset encode -o code.png  976da1b3...2a56            # digest as hex
roset encode -o code.svg  --file release.tar.gz       # hash a file, then encode it
roset encode -o tagged.png --file f.txt --tag "release-2026.10"   # digest + up to 48 bytes of data
roset encode --logo seal.svg -o code.png              # logo (SVG/PNG/JPEG/GIF) in the centre
roset encode --levels 4 -o gray.png 976da1b3...2a56   # four gray levels per cell
roset decode photo.jpg                                # prints the digest
roset decode --verify <hex> photo.jpg                 # exit 0 match, 3 mismatch, 1 not found
roset decode --json --debug-dir dbg/ photo.jpg        # JSON record + bin/overlay diagnostics
```

`--style runs` (default) merges neighbouring cells of the **same level** into capsule strokes; `--style dots`
draws every cell separately. Both carry identical data. `--fg/--bg` recolour the symbol (the decoder also
accepts light-on-dark and mirrored images).

## The symbol

Lengths are in *units*; 1 unit is the centre-to-centre pitch of the small data cells. Centre outward:

| Zone | Purpose |
|---|---|
| Logo disc | clear area for the logo; never sampled, so a logo costs no error-correction budget |
| Inner hexagon | solid hexagonal stroke around the logo disc |
| **Six petals** | the data: six hex-lattice patches of `3K²+3K+1` cells around the centre patch |
| Header ring | a hexagonal ring of binary dots/runs: 96 slots = 3 copies of the 32-bit header |
| Border | a solid hexagonal stroke; its six outer corners locate the symbol |

The petals are the patches of hex-lattice cells within hex distance `K` of `S_i`, where `S_0 = (K+1, K)` in
axial coordinates and `S_i` is `S_0` rotated by `i·60°`. Because `3K²+3K+1 = (K+1)² + (K+1)K + K²`, those
vectors generate a sublattice whose fundamental domain is exactly one patch, so the flower tiles with no gaps
or overlaps (`TestPetalsTile` proves it for every K). The consequence is a slight pinwheel twist: the cell
lattice is rotated about 26° relative to the hexagonal frame, and the flower is chiral (a mirror image is
detected and handled).

`--logo-size` picks `K`: small 4, medium 5 (default), large 6, xlarge 7. The logo disc is about 13%, 16%, 18%
and 19% of the symbol's width.

### Cells, levels and capacity

Each cell holds **1 bit (2 levels: background or ink)** or **2 bits (4 levels: 0, 1/3, 2/3, full ink)**,
chosen with `--levels auto|2|4`. Levels are Gray-coded so confusing two neighbouring levels flips one bit.
`auto` (default) uses 2 levels unless the payload needs 4.

Codeword bytes are dealt to the petals round-robin (`byte % 6`), shuffled within each petal, whitened with a
fixed PRBS and packed into cells. So **one whole petal can be destroyed and the symbol still decodes**
(checked for every version); the spare capacity of each version is spent on extra Reed-Solomon parity.

| Version | K | Levels | Payload | Reed-Solomon |
|---|---|---|---|---|
| 1 | 5 | 2 | 32 B | RS(66,32) |
| 2 | 6 | 2 | 32 B | RS(90,32) |
| 3 / 4 | 7 | 2 | 32 / 80 B | RS(126,32) / RS(126,80) |
| 5 | 4 | 4 | 32 B | RS(90,32) |
| 6 / 9 | 5 | 4 | 32 / 80 B | RS(132,32) / RS(132,80) |
| 7 / 10 | 6 | 4 | 32 / 80 B | RS(186,32) / RS(186,80) |
| 8 / 11 | 7 | 4 | 32 / 80 B | RS(252,32) / RS(252,80) |

The encoder picks the smallest version that fits the chosen `--logo-size`. With 2 levels, small (K=4) has too
few cells for any payload and 80-byte tags need K=7, so `auto` switches to 4 levels for those.

**Header (32 bits):** `version(4) | mode(4) | length(8) | crc16(16)`, three copies in consecutive blocks of
the ring, bitwise majority vote (each copy is also tried alone). Modes: 0 raw SHA-256, 1 SHA-256 + tag,
2-14 reserved (the decoder returns the raw payload, never guesses), 15 escape.

## Logos in the centre

`--logo <file>` composites the logo into the disc inside the inner hexagon. By default the smallest circle
enclosing the logo's *visible ink* is scaled to fill 97% of the disc (`--logo-fit viewbox` fits the whole
image rectangle; `--logo-scale` shrinks or enlarges). `--logo-bg #rrggbb` sets the backdrop colour. SVG output
embeds an SVG logo as live vector; PNG output with an SVG logo uses a built-in mini rasterizer
(`internal/svgmini`: filled shapes, paths with curves and arcs, groups, transforms; it **warns** about strokes,
gradients, text, CSS and `<use>`). SVG logos containing scripts, event handlers, entities, `foreignObject` or
external hrefs are refused.

## How decoding works (pure Go, `internal/detect`, `internal/decode`)

1. Grayscale, downscale to <=1600 px, adaptive (Bradley) or Otsu threshold, 8-connected components.
2. For each large component take the convex hull of its outline, reduce it to six vertices, fit a line to each
   side and intersect neighbours. A component whose hull is a hexagon is a border candidate; the logo and
   cell ink are never used for detection.
3. A **homography** from the border's six corners to the canonical hexagon. Because only the outline is used,
   perspective is handled exactly. All 6 rotations x 2 mirrorings are tried; the header CRC identifies the
   right one, so no sync mark is needed.
4. The header ring is read by luminance (ink and paper are measured from the border stroke and the margin),
   CRC-checked, and the version gives the patch radius.
5. Every data cell's luminance is sampled and clustered (1-D k-means seeded between ink and paper) into the
   version's levels. The data is whitened, so all levels are used about equally, which is what makes this
   blind calibration work without reference swatches.
6. De-whiten, un-shuffle, Reed-Solomon (Berlekamp-Massey + Chien + Forney).
7. Any failure falls through to the next threshold, polarity, border candidate, patch radius and orientation.

## Measured robustness (synthetic; `ROSET_ENVELOPE=1 ROSET_ENVELOPE_VER=<n> go test ./internal/decode -run Envelope -v`)

Same geometry (K=5), binary (version 1) versus four levels (version 6):

| Degradation | 2 levels (v1) | 4 levels (v6) |
|---|---|---|
| Resolution (symbol width) | 100 px (the smallest tried; ~2.4 px/unit) | 120 px comfortably, 100 px with 22 RS repairs |
| Gaussian blur, sigma at 500 px width | 4 | 3 (4 fails) |
| Noise sigma (of 255) at 500 px | 90 | 60 (90 fails) |
| JPEG quality at 500 px | 4 | 4 |
| Lighting gradient (darkening across the width) | 65% | 65% (47 of the 50 correctable bytes used) |
| Whole petals wiped | 1 | 2 |
| Perspective tilt | 65 degrees (the largest tried) | 65 degrees |

So 2 levels is the most robust, while 4 levels trades blur/noise margin for capacity and extra parity. Both
cope with rotation, mirroring, inverted colours, clutter, off-centre placement and a rotation + tilt +
blur + noise + JPEG combination (see `internal/decode/decode_test.go`).

**Caveat:** these are *simulated* distortions of rendered symbols. SVG output was additionally rasterized with
an independent renderer (`cairosvg`) and decoded. Real phone photos and print-and-photograph runs have not
been tested; expect lens distortion, glare and motion blur to cost margin. 4 levels in particular depends on
the camera not clipping or compressing grays, so prefer 2 levels for anything printed.
Use `--debug-dir` on a failing photo (`bin.png`, `overlay.png`).

## Not done yet

- Decoding from SVG files (rasterize first).
- Stroked / gradient / text logos in **PNG** output (they work in SVG output).
- Soft-decision / erasure decoding; it matters more now that cells carry several levels.
- Reference swatches or local illumination correction for 4 levels under strong lighting gradients (a
  single global k-means is used today).
- The six notches between the petals and the border are empty; they could carry extra header copies.
- Edge-coded data (strokes as data); the strokes are structural only.
- Modes 2-15, colour-coded cells, strong lens distortion correction.

## Development

```text
go test ./...                                   # spec (tiling proof), codec, rendering (incl. SVG-vs-layout), decoder
ROSET_SAMPLES=/tmp go test ./internal/render -run Samples      # write sample PNG/SVG
ROSET_ENVELOPE=1 go test ./internal/decode -run Envelope -v    # print the robustness table
```

Layout: `internal/spec` (hex geometry, petal tiling, versions) - `internal/codec` (GF/RS/CRC/header/framing,
levels) - `internal/layout` (what ink is where) - `internal/render` (PNG, SVG) - `internal/logo` (load, auto-fit,
zone rendering, SVG embedding) - `internal/svgmini` (SVG rasterizer) - `internal/detect` (CV primitives:
threshold, components, hexagon fit, homography) - `internal/decode` (pipeline, debug dumps).
`testdata/logo.svg` is a stand-in sample seal. The previous concentric-ring format is tagged `rings-v1`.
