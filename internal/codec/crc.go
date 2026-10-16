package codec

// CRC16 is CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF).
func CRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

type xorshift32 struct{ s uint32 }

func (x *xorshift32) next() uint32 {
	s := x.s
	s ^= s << 13
	s ^= s >> 17
	s ^= s << 5
	x.s = s
	return s
}
