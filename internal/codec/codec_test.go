package codec

import (
	"bytes"
	"math/rand"
	"testing"

	"roset/internal/spec"
)

func TestRSCorrectsUpToT(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, nsym := range []int{32, 48} {
		rs := NewRS(nsym)
		for trial := 0; trial < 200; trial++ {
			msg := make([]byte, 32)
			rng.Read(msg)
			cw := rs.Encode(msg)
			bad := append([]byte(nil), cw...)
			nerr := rng.Intn(nsym/2 + 1)
			for _, p := range rng.Perm(len(bad))[:nerr] {
				bad[p] ^= byte(1 + rng.Intn(255))
			}
			got, n, err := rs.Decode(bad)
			if err != nil || n != nerr || !bytes.Equal(got, cw) {
				t.Fatalf("nsym=%d nerr=%d: err=%v n=%d", nsym, nerr, err, n)
			}
		}
	}
}

func TestRSRejectsTooMany(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	rs := NewRS(32)
	fails := 0
	for trial := 0; trial < 200; trial++ {
		msg := make([]byte, 32)
		rng.Read(msg)
		cw := rs.Encode(msg)
		for _, p := range rng.Perm(len(cw))[:25] {
			cw[p] ^= byte(1 + rng.Intn(255))
		}
		if got, _, err := rs.Decode(cw); err != nil {
			fails++
		} else if bytes.Equal(got[:32], msg) {
			t.Fatal("impossible: decoded 25 errors correctly")
		}
	}
	if fails < 190 {
		t.Fatalf("expected nearly all heavy corruptions to be rejected, rejected %d/200", fails)
	}
}

func randomPayload(rng *rand.Rand, v spec.Version) []byte {
	n := 32
	if v.Capacity() > 32 {
		n = 33 + rng.Intn(v.Capacity()-32)
	}
	p := make([]byte, n)
	rng.Read(p)
	return p
}

func TestEncodeDecodeCells(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, v := range spec.Versions {
		for trial := 0; trial < 20; trial++ {
			payload := randomPayload(rng, v)
			mode := ModeSHA256Tag
			c, err := Encode(v, mode, payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.Data) != v.TotalCells() {
				t.Fatalf("version %d: %d cells, want %d", v.ID, len(c.Data), v.TotalCells())
			}
			for _, l := range c.Data {
				if int(l) >= v.Levels() {
					t.Fatalf("version %d: level %d out of range", v.ID, l)
				}
			}
			res, err := Decode(c)
			if err != nil || !bytes.Equal(res.Payload, payload) || res.Mode != mode || res.Corrected != 0 {
				t.Fatalf("version %d: round trip failed: %v", v.ID, err)
			}
		}
	}
}

// Levels should be roughly balanced (whitening), so no big blank or solid areas.
func TestLevelBalance(t *testing.T) {
	for _, v := range spec.Versions {
		c, _ := Encode(v, ModeRawSHA256, make([]byte, 32)) // worst case: all-zero payload
		count := make([]int, v.Levels())
		for _, l := range c.Data {
			count[l]++
		}
		for l, n := range count {
			f := float64(n) / float64(len(c.Data))
			if f < 0.8/float64(v.Levels()) || f > 1.2/float64(v.Levels()) {
				t.Errorf("version %d: level %d has %.0f%% of the cells", v.ID, l, 100*f)
			}
		}
	}
}

// Corrupting one whole petal must still decode, and corrupting up to NSym/2 random bytes too.
func TestPetalLossAndByteErrors(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for _, v := range spec.Versions {
		payload := randomPayload(rng, v)
		for petal := 0; petal < spec.NumPetals; petal++ {
			c, _ := Encode(v, ModeSHA256Tag, payload)
			n := v.Geo().PetalN
			for i := petal * n; i < (petal+1)*n; i++ {
				c.Data[i] = uint8(rng.Intn(v.Levels()))
			}
			res, err := Decode(c)
			if err != nil || !bytes.Equal(res.Payload, payload) {
				t.Fatalf("version %d: losing petal %d broke decoding: %v", v.ID, petal, err)
			}
		}
		c, _ := Encode(v, ModeSHA256Tag, payload)
		p := getPlan(v)
		for _, g := range rng.Perm(v.N)[:v.NSym/2] {
			base := byteCells(v, p, g)
			c.Data[base] = (c.Data[base] + 1) % uint8(v.Levels())
		}
		res, err := Decode(c)
		if err != nil || !bytes.Equal(res.Payload, payload) {
			t.Fatalf("version %d: %d byte errors broke decoding: %v", v.ID, v.NSym/2, err)
		}
	}
}

// One level of confusion between neighbouring levels must flip at most one bit.
func TestGrayCoding(t *testing.T) {
	for l := 0; l < 3; l++ {
		d := gray(l) ^ gray(l+1)
		if d&(d-1) != 0 {
			t.Fatalf("levels %d and %d differ in more than one bit", l, l+1)
		}
	}
}

func TestHeaderVoting(t *testing.T) {
	h := Header{Version: 6, Mode: 1, Length: 50}
	cp := packHeader(h)
	// wreck copy 0 and flip a bit in copy 1: majority and per-copy fallback must still agree
	for i := range cp[0] {
		cp[0][i] = !cp[0][i]
	}
	cp[1][5] = !cp[1][5]
	got, err := ParseHeader(cp)
	if err != nil || got != h {
		t.Fatalf("got %+v err %v", got, err)
	}
	for i := range cp[2] {
		cp[2][i] = !cp[2][i]
	}
	if _, err := ParseHeader(cp); err == nil {
		t.Fatal("expected a CRC failure when every copy is damaged")
	}
}
