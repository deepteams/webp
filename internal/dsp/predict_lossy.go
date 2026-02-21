package dsp

// VP8 intra prediction modes for lossy decoding/encoding.
//
// Convention: each PredFunc receives the full reconstruction buffer (buf) and
// an offset (off) such that buf[off] is the top-left pixel of the block.
// Reference pixels live before off:
//   - buf[off - BPS + i] : top row
//   - buf[off - 1 + j*BPS] : left column
//   - buf[off - BPS - 1] : top-left corner
//
// Using an explicit offset keeps all slice indices non-negative, which is
// required by Go's runtime bounds checking.

// avg3 returns (a + 2*b + c + 2) >> 2.
func avg3(a, b, c uint8) uint8 {
	return uint8((int(a) + 2*int(b) + int(c) + 2) >> 2)
}

// avg2 returns (a + b + 1) >> 1.
func avg2(a, b uint8) uint8 {
	return uint8((int(a) + int(b) + 1) >> 1)
}

// ---------- 16x16 prediction modes ----------

func dc16(dst []byte, off int) {
	dc := 0
	for i := 0; i < 16; i++ {
		dc += int(dst[off+i-BPS])
		dc += int(dst[off-1+i*BPS])
	}
	v := uint8((dc + 16) >> 5)
	for j := 0; j < 16; j++ {
		for i := 0; i < 16; i++ {
			dst[off+i+j*BPS] = v
		}
	}
}

func tm16(dst []byte, off int) {
	tl := int(dst[off-1-BPS]) // top-left pixel, constant for all iterations
	for j := 0; j < 16; j++ {
		left := int(dst[off-1+j*BPS])
		base := left - tl
		rowOff := off + j*BPS
		for i := 0; i < 16; i++ {
			dst[rowOff+i] = Clip8b(base + int(dst[off+i-BPS]))
		}
	}
}

func ve16(dst []byte, off int) {
	for j := 0; j < 16; j++ {
		for i := 0; i < 16; i++ {
			dst[off+i+j*BPS] = dst[off+i-BPS]
		}
	}
}

func he16(dst []byte, off int) {
	for j := 0; j < 16; j++ {
		v := dst[off-1+j*BPS]
		for i := 0; i < 16; i++ {
			dst[off+i+j*BPS] = v
		}
	}
}

func dc16NoTop(dst []byte, off int) {
	dc := 0
	for i := 0; i < 16; i++ {
		dc += int(dst[off-1+i*BPS])
	}
	v := uint8((dc + 8) >> 4)
	for j := 0; j < 16; j++ {
		for i := 0; i < 16; i++ {
			dst[off+i+j*BPS] = v
		}
	}
}

func dc16NoLeft(dst []byte, off int) {
	dc := 0
	for i := 0; i < 16; i++ {
		dc += int(dst[off+i-BPS])
	}
	v := uint8((dc + 8) >> 4)
	for j := 0; j < 16; j++ {
		for i := 0; i < 16; i++ {
			dst[off+i+j*BPS] = v
		}
	}
}

func dc16NoTopLeft(dst []byte, off int) {
	for j := 0; j < 16; j++ {
		for i := 0; i < 16; i++ {
			dst[off+i+j*BPS] = 128
		}
	}
}

// ---------- 8x8 chroma prediction modes ----------

func dc8uv(dst []byte, off int) {
	dc := 0
	for i := 0; i < 8; i++ {
		dc += int(dst[off+i-BPS])
		dc += int(dst[off-1+i*BPS])
	}
	v := uint8((dc + 8) >> 4)
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			dst[off+i+j*BPS] = v
		}
	}
}

func tm8uv(dst []byte, off int) {
	tl := int(dst[off-1-BPS])
	for j := 0; j < 8; j++ {
		left := int(dst[off-1+j*BPS])
		base := left - tl
		rowOff := off + j*BPS
		for i := 0; i < 8; i++ {
			dst[rowOff+i] = Clip8b(base + int(dst[off+i-BPS]))
		}
	}
}

func ve8uv(dst []byte, off int) {
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			dst[off+i+j*BPS] = dst[off+i-BPS]
		}
	}
}

func he8uv(dst []byte, off int) {
	for j := 0; j < 8; j++ {
		v := dst[off-1+j*BPS]
		for i := 0; i < 8; i++ {
			dst[off+i+j*BPS] = v
		}
	}
}

func dc8uvNoTop(dst []byte, off int) {
	dc := 0
	for i := 0; i < 8; i++ {
		dc += int(dst[off-1+i*BPS])
	}
	v := uint8((dc + 4) >> 3)
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			dst[off+i+j*BPS] = v
		}
	}
}

func dc8uvNoLeft(dst []byte, off int) {
	dc := 0
	for i := 0; i < 8; i++ {
		dc += int(dst[off+i-BPS])
	}
	v := uint8((dc + 4) >> 3)
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			dst[off+i+j*BPS] = v
		}
	}
}

func dc8uvNoTopLeft(dst []byte, off int) {
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			dst[off+i+j*BPS] = 128
		}
	}
}

// ---------- 4x4 prediction modes ----------

func dc4(dst []byte, off int) {
	dc := 0
	for i := 0; i < 4; i++ {
		dc += int(dst[off+i-BPS])
		dc += int(dst[off-1+i*BPS])
	}
	v := uint8((dc + 4) >> 3)
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			dst[off+i+j*BPS] = v
		}
	}
}

func tm4(dst []byte, off int) {
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			v := int(dst[off-1+j*BPS]) + int(dst[off+i-BPS]) - int(dst[off-1-BPS])
			dst[off+i+j*BPS] = Clip8b(v)
		}
	}
}

func ve4(dst []byte, off int) {
	topM1 := dst[off-1-BPS]
	top0 := dst[off+0-BPS]
	top1 := dst[off+1-BPS]
	top2 := dst[off+2-BPS]
	top3 := dst[off+3-BPS]
	top4 := dst[off+4-BPS]
	vals := [4]uint8{
		avg3(topM1, top0, top1),
		avg3(top0, top1, top2),
		avg3(top1, top2, top3),
		avg3(top2, top3, top4),
	}
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			dst[off+i+j*BPS] = vals[i]
		}
	}
}

func he4(dst []byte, off int) {
	tl := dst[off-1-BPS]
	l0 := dst[off-1+0*BPS]
	l1 := dst[off-1+1*BPS]
	l2 := dst[off-1+2*BPS]
	l3 := dst[off-1+3*BPS]
	vals := [4]uint8{
		avg3(tl, l0, l1),
		avg3(l0, l1, l2),
		avg3(l1, l2, l3),
		avg3(l2, l3, l3),
	}
	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			dst[off+i+j*BPS] = vals[j]
		}
	}
}

func rd4(dst []byte, off int) {
	tl := dst[off-1-BPS]
	t0 := dst[off+0-BPS]
	t1 := dst[off+1-BPS]
	t2 := dst[off+2-BPS]
	t3 := dst[off+3-BPS]
	l0 := dst[off-1+0*BPS]
	l1 := dst[off-1+1*BPS]
	l2 := dst[off-1+2*BPS]
	l3 := dst[off-1+3*BPS]

	dst[off+0+3*BPS] = avg3(l3, l2, l1)
	dst[off+0+2*BPS] = avg3(l2, l1, l0)
	dst[off+1+3*BPS] = avg3(l2, l1, l0)
	dst[off+0+1*BPS] = avg3(l1, l0, tl)
	dst[off+1+2*BPS] = avg3(l1, l0, tl)
	dst[off+2+3*BPS] = avg3(l1, l0, tl)
	dst[off+0+0*BPS] = avg3(l0, tl, t0)
	dst[off+1+1*BPS] = avg3(l0, tl, t0)
	dst[off+2+2*BPS] = avg3(l0, tl, t0)
	dst[off+3+3*BPS] = avg3(l0, tl, t0)
	dst[off+1+0*BPS] = avg3(tl, t0, t1)
	dst[off+2+1*BPS] = avg3(tl, t0, t1)
	dst[off+3+2*BPS] = avg3(tl, t0, t1)
	dst[off+2+0*BPS] = avg3(t0, t1, t2)
	dst[off+3+1*BPS] = avg3(t0, t1, t2)
	dst[off+3+0*BPS] = avg3(t1, t2, t3)
}

func vr4(dst []byte, off int) {
	tl := dst[off-1-BPS]
	t0 := dst[off+0-BPS]
	t1 := dst[off+1-BPS]
	t2 := dst[off+2-BPS]
	t3 := dst[off+3-BPS]
	l0 := dst[off-1+0*BPS]
	l1 := dst[off-1+1*BPS]
	l2 := dst[off-1+2*BPS]

	dst[off+0+0*BPS] = avg2(tl, t0)
	dst[off+1+0*BPS] = avg2(t0, t1)
	dst[off+2+0*BPS] = avg2(t1, t2)
	dst[off+3+0*BPS] = avg2(t2, t3)

	dst[off+0+1*BPS] = avg3(l0, tl, t0)
	dst[off+1+1*BPS] = avg3(tl, t0, t1)
	dst[off+2+1*BPS] = avg3(t0, t1, t2)
	dst[off+3+1*BPS] = avg3(t1, t2, t3)

	dst[off+0+2*BPS] = avg3(l1, l0, tl)
	dst[off+1+2*BPS] = dst[off+0+0*BPS]
	dst[off+2+2*BPS] = dst[off+1+0*BPS]
	dst[off+3+2*BPS] = dst[off+2+0*BPS]

	dst[off+0+3*BPS] = avg3(l2, l1, l0)
	dst[off+1+3*BPS] = dst[off+0+1*BPS]
	dst[off+2+3*BPS] = dst[off+1+1*BPS]
	dst[off+3+3*BPS] = dst[off+2+1*BPS]
}

func ld4(dst []byte, off int) {
	// Down-Left: anti-diagonal values come from top row with stride 1.
	// C reference: LD4_C in libwebp/src/dsp/dec.c
	A := dst[off+0-BPS]
	B := dst[off+1-BPS]
	C := dst[off+2-BPS]
	D := dst[off+3-BPS]
	E := dst[off+4-BPS]
	F := dst[off+5-BPS]
	G := dst[off+6-BPS]
	H := dst[off+7-BPS]

	dst[off+0+0*BPS] = avg3(A, B, C)
	dst[off+1+0*BPS] = avg3(B, C, D)
	dst[off+0+1*BPS] = avg3(B, C, D)
	dst[off+2+0*BPS] = avg3(C, D, E)
	dst[off+1+1*BPS] = avg3(C, D, E)
	dst[off+0+2*BPS] = avg3(C, D, E)
	dst[off+3+0*BPS] = avg3(D, E, F)
	dst[off+2+1*BPS] = avg3(D, E, F)
	dst[off+1+2*BPS] = avg3(D, E, F)
	dst[off+0+3*BPS] = avg3(D, E, F)
	dst[off+3+1*BPS] = avg3(E, F, G)
	dst[off+2+2*BPS] = avg3(E, F, G)
	dst[off+1+3*BPS] = avg3(E, F, G)
	dst[off+3+2*BPS] = avg3(F, G, H)
	dst[off+2+3*BPS] = avg3(F, G, H)
	dst[off+3+3*BPS] = avg3(G, H, H)
}

func vl4(dst []byte, off int) {
	// Vertical-Left: C reference VL4_C in libwebp/src/dsp/dec.c
	// Reads 8 top pixels (A-H), not 7.
	A := dst[off+0-BPS]
	B := dst[off+1-BPS]
	C := dst[off+2-BPS]
	D := dst[off+3-BPS]
	E := dst[off+4-BPS]
	F := dst[off+5-BPS]
	G := dst[off+6-BPS]
	H := dst[off+7-BPS]

	dst[off+0+0*BPS] = avg2(A, B)
	dst[off+1+0*BPS] = avg2(B, C)
	dst[off+0+2*BPS] = avg2(B, C)
	dst[off+2+0*BPS] = avg2(C, D)
	dst[off+1+2*BPS] = avg2(C, D)
	dst[off+3+0*BPS] = avg2(D, E)
	dst[off+2+2*BPS] = avg2(D, E)

	dst[off+0+1*BPS] = avg3(A, B, C)
	dst[off+1+1*BPS] = avg3(B, C, D)
	dst[off+0+3*BPS] = avg3(B, C, D)
	dst[off+2+1*BPS] = avg3(C, D, E)
	dst[off+1+3*BPS] = avg3(C, D, E)
	dst[off+3+1*BPS] = avg3(D, E, F)
	dst[off+2+3*BPS] = avg3(D, E, F)
	dst[off+3+2*BPS] = avg3(E, F, G)
	dst[off+3+3*BPS] = avg3(F, G, H)
}

func hd4(dst []byte, off int) {
	tl := dst[off-1-BPS]
	t0 := dst[off+0-BPS]
	t1 := dst[off+1-BPS]
	t2 := dst[off+2-BPS]
	l0 := dst[off-1+0*BPS]
	l1 := dst[off-1+1*BPS]
	l2 := dst[off-1+2*BPS]
	l3 := dst[off-1+3*BPS]

	dst[off+0+0*BPS] = avg2(tl, l0)
	dst[off+1+0*BPS] = avg3(l0, tl, t0)
	dst[off+2+0*BPS] = avg3(tl, t0, t1)
	dst[off+3+0*BPS] = avg3(t0, t1, t2)

	dst[off+0+1*BPS] = avg2(l0, l1)
	dst[off+1+1*BPS] = avg3(tl, l0, l1)
	dst[off+2+1*BPS] = dst[off+0+0*BPS]
	dst[off+3+1*BPS] = dst[off+1+0*BPS]

	dst[off+0+2*BPS] = avg2(l1, l2)
	dst[off+1+2*BPS] = avg3(l0, l1, l2)
	dst[off+2+2*BPS] = dst[off+0+1*BPS]
	dst[off+3+2*BPS] = dst[off+1+1*BPS]

	dst[off+0+3*BPS] = avg2(l2, l3)
	dst[off+1+3*BPS] = avg3(l1, l2, l3)
	dst[off+2+3*BPS] = dst[off+0+2*BPS]
	dst[off+3+3*BPS] = dst[off+1+2*BPS]
}

func hu4(dst []byte, off int) {
	l0 := dst[off-1+0*BPS]
	l1 := dst[off-1+1*BPS]
	l2 := dst[off-1+2*BPS]
	l3 := dst[off-1+3*BPS]

	dst[off+0+0*BPS] = avg2(l0, l1)
	dst[off+1+0*BPS] = avg3(l0, l1, l2)
	dst[off+2+0*BPS] = avg2(l1, l2)
	dst[off+3+0*BPS] = avg3(l1, l2, l3)

	dst[off+0+1*BPS] = dst[off+2+0*BPS]
	dst[off+1+1*BPS] = dst[off+3+0*BPS]
	dst[off+2+1*BPS] = avg2(l2, l3)
	dst[off+3+1*BPS] = avg3(l2, l3, l3)

	dst[off+0+2*BPS] = dst[off+2+1*BPS]
	dst[off+1+2*BPS] = dst[off+3+1*BPS]
	dst[off+2+2*BPS] = l3
	dst[off+3+2*BPS] = l3

	dst[off+0+3*BPS] = l3
	dst[off+1+3*BPS] = l3
	dst[off+2+3*BPS] = l3
	dst[off+3+3*BPS] = l3
}

// PredLuma4Direct calls the 4x4 prediction function for the given mode directly
// via a switch statement, avoiding indirect function call overhead.
func PredLuma4Direct(mode int, dst []byte, off int) {
	switch mode {
	case 0:
		dc4(dst, off)
	case 1:
		tm4(dst, off)
	case 2:
		ve4(dst, off)
	case 3:
		he4(dst, off)
	case 4:
		rd4(dst, off)
	case 5:
		vr4(dst, off)
	case 6:
		ld4(dst, off)
	case 7:
		vl4(dst, off)
	case 8:
		hd4(dst, off)
	case 9:
		hu4(dst, off)
	}
}

// PredLuma16Direct and PredChroma8Direct are defined in platform-specific files:
// - predict_lossy_direct_arm64.go (NEON assembly for modes 0-3)
// - predict_lossy_direct_amd64.go (pure Go)
// - predict_lossy_direct_noasm.go (pure Go fallback)

// initPredictors assigns all prediction functions to the global arrays.
func initPredictors() {
	PredLuma16[0] = dc16
	PredLuma16[1] = tm16
	PredLuma16[2] = ve16
	PredLuma16[3] = he16
	PredLuma16[4] = dc16NoTop
	PredLuma16[5] = dc16NoLeft
	PredLuma16[6] = dc16NoTopLeft

	PredChroma8[0] = dc8uv
	PredChroma8[1] = tm8uv
	PredChroma8[2] = ve8uv
	PredChroma8[3] = he8uv
	PredChroma8[4] = dc8uvNoTop
	PredChroma8[5] = dc8uvNoLeft
	PredChroma8[6] = dc8uvNoTopLeft

	PredLuma4[0] = dc4
	PredLuma4[1] = tm4
	PredLuma4[2] = ve4
	PredLuma4[3] = he4
	PredLuma4[4] = rd4
	PredLuma4[5] = vr4
	PredLuma4[6] = ld4
	PredLuma4[7] = vl4
	PredLuma4[8] = hd4
	PredLuma4[9] = hu4
}
