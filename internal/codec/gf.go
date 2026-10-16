package codec

// GF(256) with primitive polynomial x^8+x^4+x^3+x^2+1 (0x11d).
var (
	expTab [512]byte
	logTab [256]int
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		expTab[i] = byte(x)
		logTab[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11d
		}
	}
	for i := 255; i < 512; i++ {
		expTab[i] = expTab[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return expTab[logTab[a]+logTab[b]]
}

func gfDiv(a, b byte) byte {
	if b == 0 {
		panic("gf: divide by zero")
	}
	if a == 0 {
		return 0
	}
	return expTab[logTab[a]+255-logTab[b]]
}
