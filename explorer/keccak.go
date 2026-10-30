package main

// Keccak-256 (variante "legacy" usada pelo Ethereum), sem dependencias externas.

import "math/bits"

var keccakRC = [24]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808A, 0x8000000080008000,
	0x000000000000808B, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
	0x000000000000008A, 0x0000000000000088, 0x0000000080008009, 0x000000008000000A,
	0x000000008000808B, 0x800000000000008B, 0x8000000000008089, 0x8000000000008003,
	0x8000000000008002, 0x8000000000000080, 0x000000000000800A, 0x800000008000000A,
	0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

var keccakRotc = [24]int{1, 3, 6, 10, 15, 21, 28, 36, 45, 55, 2, 14, 27, 41, 56, 8, 25, 43, 62, 18, 39, 61, 20, 44}
var keccakPiln = [24]int{10, 7, 11, 17, 18, 3, 5, 16, 8, 21, 24, 4, 15, 23, 19, 13, 12, 2, 20, 14, 22, 9, 6, 1}

func keccakF1600(st *[25]uint64) {
	var bc [5]uint64
	for round := 0; round < 24; round++ {
		for i := 0; i < 5; i++ {
			bc[i] = st[i] ^ st[i+5] ^ st[i+10] ^ st[i+15] ^ st[i+20]
		}
		for i := 0; i < 5; i++ {
			t := bc[(i+4)%5] ^ bits.RotateLeft64(bc[(i+1)%5], 1)
			for j := 0; j < 25; j += 5 {
				st[j+i] ^= t
			}
		}
		t := st[1]
		for i := 0; i < 24; i++ {
			j := keccakPiln[i]
			bc[0] = st[j]
			st[j] = bits.RotateLeft64(t, keccakRotc[i])
			t = bc[0]
		}
		for j := 0; j < 25; j += 5 {
			for i := 0; i < 5; i++ {
				bc[i] = st[j+i]
			}
			for i := 0; i < 5; i++ {
				st[j+i] ^= (^bc[(i+1)%5]) & bc[(i+2)%5]
			}
		}
		st[0] ^= keccakRC[round]
	}
}

// Keccak256 retorna o hash Keccak-256 de data.
func Keccak256(data []byte) [32]byte {
	const rate = 136
	var st [25]uint64
	buf := make([]byte, 0, len(data)+rate)
	buf = append(buf, data...)
	// padding Keccak original: 0x01 ... 0x80
	pad := rate - len(buf)%rate
	p := make([]byte, pad)
	p[0] = 0x01
	p[pad-1] |= 0x80
	buf = append(buf, p...)
	for off := 0; off < len(buf); off += rate {
		for i := 0; i < rate/8; i++ {
			var w uint64
			for b := 0; b < 8; b++ {
				w |= uint64(buf[off+i*8+b]) << (8 * b)
			}
			st[i] ^= w
		}
		keccakF1600(&st)
	}
	var out [32]byte
	for i := 0; i < 4; i++ {
		for b := 0; b < 8; b++ {
			out[i*8+b] = byte(st[i] >> (8 * b))
		}
	}
	return out
}
