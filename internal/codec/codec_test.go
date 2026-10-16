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

func TestEncodeDecodeCells(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, v := range spec.Versions {
		for trial := 0; trial < 50; trial++ {
			n := 32
			if v.Capacity() > 32 {
				n = 33 + rng.Intn(v.Capacity()-32)
			}
			payload := make([]byte, n)
			rng.Read(payload)
			mode := ModeRawSHA256
			if n != 32 {
				mode = ModeSHA256Tag
			}
			c, err := Encode(v, mode, payload)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Decode(c)
			if err != nil || !bytes.Equal(res.Payload, payload) || res.Mode != mode || res.Version != v.ID {
				t.Fatalf("v%d: %v", v.ID, err)
			}
		}
	}
}

func TestBurstAndHeaderDamage(t *testing.T) {
	v := spec.Versions[0]
	payload := bytes.Repeat([]byte{0xA5}, 32)
	c, _ := Encode(v, ModeRawSHA256, payload)
	// wipe 60 consecutive cells (a smudge) on the data rings
	flip := 60
	for k := range c.Data {
		for i := range c.Data[k] {
			if flip > 0 && k == 3 && i > 5 {
				c.Data[k][i] = !c.Data[k][i]
				flip--
			}
		}
	}
	// damage each header copy in different places
	c.Header[0][0] = !c.Header[0][0]
	c.Header[1][7] = !c.Header[1][7]
	c.Header[2][20] = !c.Header[2][20]
	res, err := Decode(c)
	if err != nil || !bytes.Equal(res.Payload, payload) {
		t.Fatalf("burst decode failed: %v", err)
	}
	t.Logf("repaired %d bytes", res.Corrected)
}
