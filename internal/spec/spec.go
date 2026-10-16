// Package spec defines the roset symbol geometry and version table.
//
// All lengths are in "units" (1 unit = ring pitch). The symbol is centered on
// the origin; angles are measured clockwise from "up".
//
// Versions 1-2 have a solid bullseye in the middle. Versions 3-8 replace the
// bullseye's centre with a clear circular *logo zone* (radius LogoR) surrounded
// by two finder rings; everything outside shifts outward accordingly. The logo
// zone is not part of the data, so a logo costs no error-correction budget.
package spec

import "math"

const (
	HeaderBits    = 32 // ver(4) mode(4) len(8) crc16(16)
	HeaderRings   = 3  // three rotated copies, bitwise majority vote
	HeaderSlots   = 32
	HeaderR0      = 7.0 // radius of first header ring in a plain (no-logo) symbol
	HeaderRotStep = 11  // copy j is rotated by j*HeaderRotStep slots
	DataR0        = 11.0
	MinSlotArc    = 1.2 // minimum arc length of a data slot
	DotRadius     = 0.36
	BoundaryHalf  = 0.4 // boundary ring half width
	SyncDotRadius = 0.9
	SyncOffset    = 1.3 // sync dot centre = boundary radius + SyncOffset (angle 0)
	QuietZone     = 2.0

	plainFinderOuter = 5.5
)

// FinderBands are the dark radial bands of the plain (no-logo) bullseye.
var plainBands = [][2]float64{{0, 1.5}, {2.5, 3.5}, {4.5, 5.5}}

// LogoSizes are the clear-zone radii offered, in units.
var LogoSizes = map[string]float64{"small": 4.0, "medium": 5.5, "large": 7.0}

// Version describes a symbol size and its error-correction layout.
type Version struct {
	ID    int
	Rings int     // number of data rings
	N     int     // Reed-Solomon codeword bytes
	NSym  int     // parity bytes
	LogoR float64 // clear logo-zone radius; 0 = plain bullseye
}

// Versions is built at init: for each logo size (0 = none) a 32-byte-payload
// symbol and an 80-byte-payload symbol, with the fewest data rings that hold
// the codeword. IDs: 1,2 plain; 3,4 small logo; 5,6 medium; 7,8 large.
var Versions = buildVersions()

func buildVersions() []Version {
	bases := [][2]int{{64, 32}, {128, 48}}
	var out []Version
	id := 1
	for _, lr := range []float64{0, LogoSizes["small"], LogoSizes["medium"], LogoSizes["large"]} {
		for _, b := range bases {
			v := Version{ID: id, N: b[0], NSym: b[1], LogoR: lr}
			for v.Rings = 1; v.TotalCells()/8 < v.N; v.Rings++ {
			}
			out = append(out, v)
			id++
		}
	}
	return out
}

func Lookup(id int) (Version, bool) {
	for _, v := range Versions {
		if v.ID == id {
			return v, true
		}
	}
	return Version{}, false
}

func (v Version) Capacity() int { return v.N - v.NSym }
func (v Version) HasLogo() bool { return v.LogoR > 0 }

// FinderBands returns the finder's dark radial bands, innermost first.
func (v Version) FinderBands() [][2]float64 {
	if !v.HasLogo() {
		return plainBands
	}
	return [][2]float64{{v.LogoR + 1, v.LogoR + 2}, {v.LogoR + 3, v.LogoR + 4}}
}

func (v Version) FinderOuter() float64 {
	b := v.FinderBands()
	return b[len(b)-1][1]
}

// Shift is how far this version's header/data/boundary are pushed outward relative to a plain symbol.
func (v Version) Shift() float64 { return v.FinderOuter() - plainFinderOuter }

func (v Version) HeaderRingRadius(j int) float64 { return HeaderR0 + v.Shift() + float64(j) }
func (v Version) DataRingRadius(k int) float64   { return DataR0 + v.Shift() + float64(k) }
func (v Version) BoundaryRadius() float64        { return DataR0 + v.Shift() + float64(v.Rings) + 1 }
func (v Version) SyncRadius() float64            { return v.BoundaryRadius() + SyncOffset }
func (v Version) OuterRadius() float64           { return v.SyncRadius() + SyncDotRadius }
func (v Version) TotalRadius() float64           { return v.OuterRadius() + QuietZone }

// SlotCounts returns the number of angular slots on each data ring.
func (v Version) SlotCounts() []int {
	out := make([]int, v.Rings)
	for k := range out {
		out[k] = int(math.Floor(2 * math.Pi * v.DataRingRadius(k) / MinSlotArc))
	}
	return out
}

func (v Version) TotalCells() int {
	t := 0
	for _, n := range v.SlotCounts() {
		t += n
	}
	return t
}
