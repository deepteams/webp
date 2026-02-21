#include "textflag.h"

// BPS = 32 (stride constant)
#define BPS $32

// func sse16x16AVX2(pix, ref []byte) int
// Computes sum of squared differences for a 16x16 block with BPS stride.
// AVX2: processes all 16 bytes per row in a single YMM pass (vs two XMM halves).
// Uses VPMOVZXBW-equivalent (VPUNPCKLBW/VPUNPCKHBW + VINSERTI128) to extend
// 16 bytes to 16 int16 in YMM, then VPSUBW + VPMADDWD for sum-of-squares.
TEXT ·sse16x16AVX2(SB), NOSPLIT, $0-56
	MOVQ pix_base+0(FP), SI   // pix pointer
	MOVQ ref_base+24(FP), DI  // ref pointer
	VPXOR Y6, Y6, Y6          // Y6 = zero register
	VPXOR Y7, Y7, Y7          // Y7 = accumulator (8 × int32)

	MOVQ $16, CX              // row counter
	XORQ DX, DX               // offset = 0

sse16x16_avx2_loop:
	// Load 16 pix bytes, zero-extend to 16 int16 in Y0.
	VMOVDQU (SI)(DX*1), X0    // X0 = 16 pix bytes
	VPUNPCKLBW X6, X0, X2     // X2 = pix[0..7] as int16
	VPUNPCKHBW X6, X0, X3     // X3 = pix[8..15] as int16
	VINSERTI128 $1, X3, Y2, Y0 // Y0 = [pix_lo | pix_hi] = 16 int16

	// Load 16 ref bytes, zero-extend to 16 int16 in Y1.
	VMOVDQU (DI)(DX*1), X1    // X1 = 16 ref bytes
	VPUNPCKLBW X6, X1, X4     // X4 = ref[0..7] as int16
	VPUNPCKHBW X6, X1, X5     // X5 = ref[8..15] as int16
	VINSERTI128 $1, X5, Y4, Y1 // Y1 = [ref_lo | ref_hi] = 16 int16

	// Diff, square, accumulate.
	VPSUBW Y1, Y0, Y0         // 16 signed int16 differences
	// VPMADDWD Y0, Y0, Y0 — raw VEX: VEX.256.66.0F F5 /r
	LONG $0xF57DE1C4; BYTE $0xC0  // 8 int32: sum of squared pairs
	VPADDD Y0, Y7, Y7         // accumulate

	ADDQ BPS, DX               // offset += BPS
	DECQ CX
	JNZ sse16x16_avx2_loop

	// Horizontal sum of Y7 (8 × int32 → 1 int).
	VEXTRACTI128 $1, Y7, X0   // X0 = high 128 bits of Y7
	PADDD X0, X7               // X7 = 4 × int32
	PSHUFD $0x4E, X7, X0      // swap high/low 64-bit halves
	PADDD X0, X7
	PSHUFD $0xB1, X7, X0      // swap adjacent 32-bit words
	PADDD X0, X7
	MOVL X7, AX
	MOVQ AX, ret+48(FP)

	VZEROUPPER
	RET

// ============================================================================
// func tDisto4x4AVX2(a, b []byte) int
// Computes abs(tTransform(b, kWeightY) - tTransform(a, kWeightY)) >> 5
// AVX2: processes both blocks simultaneously in YMM lanes (a=low, b=high).
// Each lane independently computes a 4x4 Hadamard transform with weighting.
// ============================================================================

// Weight constants (16 bytes for VBROADCASTI128).
DATA kWeightY01_AVX2<>+0x00(SB)/8, $0x0009001400200026
DATA kWeightY01_AVX2<>+0x08(SB)/8, $0x00070011001C0020
GLOBL kWeightY01_AVX2<>(SB), (NOPTR+RODATA), $16
DATA kWeightY23_AVX2<>+0x00(SB)/8, $0x0004000A00110014
DATA kWeightY23_AVX2<>+0x08(SB)/8, $0x0002000400070009
GLOBL kWeightY23_AVX2<>(SB), (NOPTR+RODATA), $16

TEXT ·tDisto4x4AVX2(SB), NOSPLIT, $0-56
	MOVQ a_base+0(FP), SI             // SI = &a[0]
	MOVQ b_base+24(FP), DI            // DI = &b[0]

	VPXOR Y14, Y14, Y14               // zero register

	// === Load 4 rows: a in low lane, b in high lane ===
	// Row 0
	MOVD (SI), X0                     // a row0 (4 bytes)
	MOVD (DI), X1                     // b row0
	VINSERTI128 $1, X1, Y0, Y0        // Y0 = [a_row0 | b_row0]
	// Row 1
	MOVD 32(SI), X2                   // a row1
	MOVD 32(DI), X3                   // b row1
	VINSERTI128 $1, X3, Y2, Y2        // Y2 = [a_row1 | b_row1]
	// Row 2
	MOVD 64(SI), X4                   // a row2
	MOVD 64(DI), X5                   // b row2
	VINSERTI128 $1, X5, Y4, Y4        // Y4 = [a_row2 | b_row2]
	// Row 3
	MOVD 96(SI), X8                   // a row3
	MOVD 96(DI), X9                   // b row3
	VINSERTI128 $1, X9, Y8, Y8        // Y8 = [a_row3 | b_row3]

	// Zero-extend uint8 → int16 (per-lane).
	VPUNPCKLBW Y14, Y0, Y0
	VPUNPCKLBW Y14, Y2, Y2
	VPUNPCKLBW Y14, Y4, Y4
	VPUNPCKLBW Y14, Y8, Y8

	// Rename for clarity: Y0=row0, Y1=row1, Y2=row2, Y3=row3
	// But we loaded into Y0,Y2,Y4,Y8. Move to Y0-Y3.
	VMOVDQA Y2, Y1                    // Y1 = row1
	VMOVDQA Y4, Y2                    // Y2 = row2
	VMOVDQA Y8, Y3                    // Y3 = row3
	// Y0 = row0 already

	// === Transpose 4x4 int16 per-lane ===
	VPUNPCKLWD Y1, Y0, Y4             // interleave(row0, row1) per-lane
	VPUNPCKLWD Y3, Y2, Y5             // interleave(row2, row3) per-lane
	VPUNPCKLQDQ Y5, Y4, Y6            // [Y4_low64, Y5_low64] per-lane
	VPUNPCKHQDQ Y5, Y4, Y7            // [Y4_high64, Y5_high64] per-lane
	VPSHUFD $0xD8, Y6, Y0             // [col0 | col1] per-lane
	VPSHUFD $0xD8, Y7, Y2             // [col2 | col3] per-lane

	// === Butterfly pass 1 (horizontal Hadamard on transposed columns) ===
	VPADDW Y2, Y0, Y4                 // Y4 = [a0 | a1]
	VPSUBW Y2, Y0, Y5                 // Y5 = [a3 | a2]
	VPSHUFD $0x4E, Y4, Y6             // Y6 = [a1 | a0]
	VPSHUFD $0x4E, Y5, Y7             // Y7 = [a2 | a3]
	VPADDW Y6, Y4, Y0                 // Y0_low = row0 = a0+a1
	VPSUBW Y6, Y4, Y2                 // Y2_low = row3 = a0-a1
	VPADDW Y7, Y5, Y1                 // Y1_low = row1 = a3+a2
	VPSUBW Y7, Y5, Y3                 // Y3_low = row2 = a3-a2

	// === Transpose back per-lane ===
	VPUNPCKLWD Y1, Y0, Y5             // interleave(row0, row1)
	VPUNPCKLWD Y2, Y3, Y6             // interleave(row2, row3)
	VPUNPCKLQDQ Y6, Y5, Y7
	VPUNPCKHQDQ Y6, Y5, Y8
	VPSHUFD $0xD8, Y7, Y0             // [col0 | col1]
	VPSHUFD $0xD8, Y8, Y2             // [col2 | col3]

	// === Butterfly pass 2 (vertical Hadamard) ===
	VPADDW Y2, Y0, Y4                 // [a0 | a1]
	VPSUBW Y2, Y0, Y5                 // [a3 | a2]
	VPSHUFD $0x4E, Y4, Y6             // [a1 | a0]
	VPSHUFD $0x4E, Y5, Y7             // [a2 | a3]
	VPADDW Y6, Y4, Y0                 // h_row0
	VPSUBW Y6, Y4, Y2                 // h_row3
	VPADDW Y7, Y5, Y1                 // h_row1
	VPSUBW Y7, Y5, Y3                 // h_row2

	// Pack: Y0 = [h_row0 | h_row1], Y3 = [h_row2 | h_row3].
	// VMOVLHPS equivalent per-lane using VPUNPCKLQDQ.
	VPUNPCKLQDQ Y1, Y0, Y0            // Y0 = [row0 | row1] per lane
	VPUNPCKLQDQ Y2, Y3, Y3            // Y3 = [row2 | row3] per lane

	// Absolute values: abs(x) = (x ^ (x>>15)) - (x>>15).
	VPSRAW $15, Y0, Y5
	VPXOR Y5, Y0, Y0
	VPSUBW Y5, Y0, Y0
	VPSRAW $15, Y3, Y5
	VPXOR Y5, Y3, Y3
	VPSUBW Y5, Y3, Y3

	// Weighted sum via VPMADDWD (YMM).
	VBROADCASTI128 kWeightY01_AVX2<>(SB), Y6
	// VPMADDWD Y0, Y6, Y6 — VEX.NDS.256.66.0F F5 /r
	// dest=Y6(reg=6), src1=Y6(vvvv=6), src2=Y0(rm=0)
	// 2-byte VEX: R̃=1, v̄=~6&0xF=9=1001, L=1, pp=01
	// Byte2: 1_1001_1_01 = 0xCD, ModRM: 11_110_000 = 0xF0
	LONG $0xF0F5CDC5                   // VPMADDWD Y0, Y6, Y6

	VBROADCASTI128 kWeightY23_AVX2<>(SB), Y7
	// VPMADDWD Y3, Y7, Y7 — dest=Y7, src1=Y7, src2=Y3
	// v̄=~7&0xF=8=1000, Byte2: 1_1000_1_01 = 0xC5, ModRM: 11_111_011 = 0xFB
	LONG $0xFBF5C5C5                   // VPMADDWD Y3, Y7, Y7

	VPADDD Y7, Y6, Y6                 // Y6 = 8 int32 [a_4sums | b_4sums]

	// Horizontal sum per-lane.
	VEXTRACTI128 $1, Y6, X1           // X1 = lane_b (4 int32)
	// X6 = lane_a (low 128 bits of Y6)

	// Sum lane_a → R8
	VPSHUFD $0x4E, X6, X0
	VPADDD X0, X6, X6
	VPSHUFD $0xB1, X6, X0
	VPADDD X0, X6, X6
	MOVL X6, R8                        // R8 = sum_a

	// Sum lane_b → AX
	VPSHUFD $0x4E, X1, X0
	VPADDD X0, X1, X1
	VPSHUFD $0xB1, X1, X0
	VPADDD X0, X1, X1
	MOVL X1, AX                        // AX = sum_b

	// result = abs(sum_b - sum_a) >> 5
	SUBQ R8, AX
	MOVQ AX, BX
	SARQ $63, BX
	XORQ BX, AX
	SUBQ BX, AX
	SHRQ $5, AX

	MOVQ AX, ret+48(FP)

	VZEROUPPER
	RET
