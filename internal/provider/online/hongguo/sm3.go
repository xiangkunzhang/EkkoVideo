package hongguo

import (
	"encoding/binary"
	"math/bits"
)

var sm3IV = [8]uint32{
	0x7380166f, 0x4914b2b9, 0x172442d7, 0xda8a0600,
	0xa96f30bc, 0x163138aa, 0xe38dee4d, 0xb0fb0e4e,
}

func sm3Sum(data []byte) [32]byte {
	msgLen := uint64(len(data)) * 8
	padded := make([]byte, len(data)+1, ((len(data)+9+63)/64)*64)
	copy(padded, data)
	padded[len(data)] = 0x80
	for (len(padded)+8)%64 != 0 {
		padded = append(padded, 0)
	}
	var lenBuf [8]byte
	binary.BigEndian.PutUint64(lenBuf[:], msgLen)
	padded = append(padded, lenBuf[:]...)

	v := sm3IV
	var w [68]uint32
	var w1 [64]uint32

	for off := 0; off < len(padded); off += 64 {
		blk := padded[off : off+64]
		for j := 0; j < 16; j++ {
			w[j] = binary.BigEndian.Uint32(blk[j*4 : j*4+4])
		}
		for j := 16; j < 68; j++ {
			x := w[j-16] ^ w[j-9] ^ bits.RotateLeft32(w[j-3], 15)
			p1 := x ^ bits.RotateLeft32(x, 15) ^ bits.RotateLeft32(x, 23)
			w[j] = p1 ^ bits.RotateLeft32(w[j-13], 7) ^ w[j-6]
		}
		for j := 0; j < 64; j++ {
			w1[j] = w[j] ^ w[j+4]
		}

		a, b, c, d, e, f, g, h := v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]
		for j := 0; j < 64; j++ {
			var t, ff, gg uint32
			if j < 16 {
				t = bits.RotateLeft32(0x79cc4519, j)
				ff = a ^ b ^ c
				gg = e ^ f ^ g
			} else {
				t = bits.RotateLeft32(0x7a879d8a, j%32)
				ff = (a & b) | (a & c) | (b & c)
				gg = (e & f) | (^e & g)
			}
			ss1 := bits.RotateLeft32(bits.RotateLeft32(a, 12)+e+t, 7)
			ss2 := ss1 ^ bits.RotateLeft32(a, 12)
			tt1 := ff + d + ss2 + w1[j]
			tt2 := gg + h + ss1 + w[j]
			d = c
			c = bits.RotateLeft32(b, 9)
			b = a
			a = tt1
			h = g
			g = bits.RotateLeft32(f, 19)
			f = e
			e = tt2 ^ bits.RotateLeft32(tt2, 9) ^ bits.RotateLeft32(tt2, 17)
		}
		v[0] ^= a
		v[1] ^= b
		v[2] ^= c
		v[3] ^= d
		v[4] ^= e
		v[5] ^= f
		v[6] ^= g
		v[7] ^= h
	}

	var out [32]byte
	for i := 0; i < 8; i++ {
		binary.BigEndian.PutUint32(out[i*4:i*4+4], v[i])
	}
	return out
}
