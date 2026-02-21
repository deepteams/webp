#include "textflag.h"

// AVX2 (VEX-encoded) versions of forward/inverse DCT 4x4 transforms.
// VEX 3-operand form eliminates ~12-15 MOVO copies per function vs SSE2.
// Also avoids SSE/AVX transition penalties when called between other AVX2 functions.

// Forward DCT constants (16-byte aligned for VMOVDQU).
// [2217, 5352] packed as two int16 for VPMADDWD.
DATA kMul2217_5352<>+0x00(SB)/4, $0x14E808A9
DATA kMul2217_5352<>+0x04(SB)/4, $0x14E808A9
DATA kMul2217_5352<>+0x08(SB)/4, $0x14E808A9
DATA kMul2217_5352<>+0x0C(SB)/4, $0x14E808A9
GLOBL kMul2217_5352<>(SB), (NOPTR+RODATA), $16

// [2217, -5352] packed as two int16 for VPMADDWD.
DATA kMul2217_n5352<>+0x00(SB)/4, $0xEB1808A9
DATA kMul2217_n5352<>+0x04(SB)/4, $0xEB1808A9
DATA kMul2217_n5352<>+0x08(SB)/4, $0xEB1808A9
DATA kMul2217_n5352<>+0x0C(SB)/4, $0xEB1808A9
GLOBL kMul2217_n5352<>(SB), (NOPTR+RODATA), $16

DATA kBias1812<>+0x00(SB)/4, $1812
DATA kBias1812<>+0x04(SB)/4, $1812
DATA kBias1812<>+0x08(SB)/4, $1812
DATA kBias1812<>+0x0C(SB)/4, $1812
GLOBL kBias1812<>(SB), (NOPTR+RODATA), $16

DATA kBias937<>+0x00(SB)/4, $937
DATA kBias937<>+0x04(SB)/4, $937
DATA kBias937<>+0x08(SB)/4, $937
DATA kBias937<>+0x0C(SB)/4, $937
GLOBL kBias937<>(SB), (NOPTR+RODATA), $16

DATA kBias7<>+0x00(SB)/4, $7
DATA kBias7<>+0x04(SB)/4, $7
DATA kBias7<>+0x08(SB)/4, $7
DATA kBias7<>+0x0C(SB)/4, $7
GLOBL kBias7<>(SB), (NOPTR+RODATA), $16

DATA kBias12000<>+0x00(SB)/4, $12000
DATA kBias12000<>+0x04(SB)/4, $12000
DATA kBias12000<>+0x08(SB)/4, $12000
DATA kBias12000<>+0x0C(SB)/4, $12000
GLOBL kBias12000<>(SB), (NOPTR+RODATA), $16

DATA kBias51000<>+0x00(SB)/4, $51000
DATA kBias51000<>+0x04(SB)/4, $51000
DATA kBias51000<>+0x08(SB)/4, $51000
DATA kBias51000<>+0x0C(SB)/4, $51000
GLOBL kBias51000<>(SB), (NOPTR+RODATA), $16

DATA kConst1<>+0x00(SB)/4, $1
DATA kConst1<>+0x04(SB)/4, $1
DATA kConst1<>+0x08(SB)/4, $1
DATA kConst1<>+0x0C(SB)/4, $1
GLOBL kConst1<>(SB), (NOPTR+RODATA), $16

// ============================================================================
// func fTransformAVX2(src, ref []byte, out []int16)
// Forward DCT 4x4. VEX re-encoding of fTransformSSE2.
// Same algorithm: load diffs → widen to int32 → transpose → horizontal
// butterfly → transpose back → vertical butterfly → pack int32→int16 → store.
// ============================================================================
TEXT ·fTransformAVX2(SB), NOSPLIT, $0-72
	MOVQ src_base+0(FP), SI
	MOVQ ref_base+24(FP), DI
	MOVQ out_base+48(FP), DX

	VPXOR X14, X14, X14               // zero register

	// === Load 4 rows of diffs (src - ref), widen to int32 ===
	// Row 0
	MOVL (SI), X0
	MOVL (DI), X1
	VPUNPCKLBW X14, X0, X0
	VPUNPCKLBW X14, X1, X1
	VPSUBW X1, X0, X0                 // diff int16
	VPSRAW $15, X0, X4                // sign extend (eliminated MOVO)
	VPUNPCKLWD X4, X0, X0             // X0 = row0 (4×int32)

	// Row 1
	MOVL 32(SI), X1
	MOVL 32(DI), X2
	VPUNPCKLBW X14, X1, X1
	VPUNPCKLBW X14, X2, X2
	VPSUBW X2, X1, X1
	VPSRAW $15, X1, X4
	VPUNPCKLWD X4, X1, X1             // X1 = row1

	// Row 2
	MOVL 64(SI), X2
	MOVL 64(DI), X3
	VPUNPCKLBW X14, X2, X2
	VPUNPCKLBW X14, X3, X3
	VPSUBW X3, X2, X2
	VPSRAW $15, X2, X4
	VPUNPCKLWD X4, X2, X2             // X2 = row2

	// Row 3
	MOVL 96(SI), X3
	MOVL 96(DI), X5
	VPUNPCKLBW X14, X3, X3
	VPUNPCKLBW X14, X5, X5
	VPSUBW X5, X3, X3
	VPSRAW $15, X3, X4
	VPUNPCKLWD X4, X3, X3             // X3 = row3

	// === Transpose 4×4 int32 (rows → columns) ===
	VPUNPCKLDQ X1, X0, X4             // [r0c0,r1c0, r0c1,r1c1]
	VPUNPCKHDQ X1, X0, X5             // [r0c2,r1c2, r0c3,r1c3]
	VPUNPCKLDQ X3, X2, X6             // [r2c0,r3c0, r2c1,r3c1]
	VPUNPCKHDQ X3, X2, X7             // [r2c2,r3c2, r2c3,r3c3]

	VPUNPCKLQDQ X6, X4, X0            // col0
	VPUNPCKHQDQ X6, X4, X1            // col1
	VPUNPCKLQDQ X7, X5, X2            // col2
	VPUNPCKHQDQ X7, X5, X3            // col3

	// === Horizontal butterfly ===
	// a0=d0+d3, a1=d1+d2, a2=d1-d2, a3=d0-d3
	VPADDD X3, X0, X4                 // X4 = a0
	VPADDD X2, X1, X5                 // X5 = a1
	VPSUBD X2, X1, X6                 // X6 = a2
	VPSUBD X3, X0, X7                 // X7 = a3

	// tmp0 = (a0 + a1) << 3
	VPADDD X5, X4, X0                 // a0 + a1 (eliminated MOVO)
	VPSLLD $3, X0, X0                 // X0 = tmp0

	// tmp2 = (a0 - a1) << 3
	VPSUBD X5, X4, X8                 // a0 - a1
	VPSLLD $3, X8, X8                 // X8 = tmp2

	// Pack a2(X6), a3(X7) to int16 for VPMADDWD.
	// VPACKSSDW X7, X6, X1 — VEX.NDS.128.66.0F 6B /r
	// dest=X1(reg=1), src1=X6(vvvv=6), src2=X7(rm=7)
	// 2-byte VEX: C5 C9 6B CF
	LONG $0xCF6BC9C5
	VPSHUFD $0x4E, X1, X2             // X2 = [a3_0..3, a2_0..3]

	// tmp1 = (a2*2217 + a3*5352 + 1812) >> 9
	VPUNPCKLWD X2, X1, X3             // [a2_0,a3_0, a2_1,a3_1, ...]
	VMOVDQU kMul2217_5352<>(SB), X4   // [2217, 5352] broadcast
	// VPMADDWD X4, X3, X3 — VEX.NDS.128.66.0F F5 /r
	// dest=X3(reg=3), src1=X3(vvvv=3), src2=X4(rm=4)
	// 2-byte VEX: C5 E1 F5 DC
	LONG $0xDCF5E1C5
	VMOVDQU kBias1812<>(SB), X5
	VPADDD X5, X3, X3
	VPSRAD $9, X3, X3                 // X3 = tmp1

	// tmp3 = (a3*2217 - a2*5352 + 937) >> 9
	VPUNPCKLWD X1, X2, X5             // [a3_0,a2_0, ...]
	VMOVDQU kMul2217_n5352<>(SB), X4  // [2217, -5352] broadcast
	// VPMADDWD X4, X5, X5 — 2-byte VEX: C5 D1 F5 EC
	LONG $0xECF5D1C5
	VMOVDQU kBias937<>(SB), X4
	VPADDD X4, X5, X5
	VPSRAD $9, X5, X5                 // X5 = tmp3

	// === Transpose back (columns → rows for vertical pass) ===
	// X0=tmp0, X3=tmp1, X8=tmp2, X5=tmp3
	VPUNPCKLDQ X3, X0, X4
	VPUNPCKHDQ X3, X0, X6
	VPUNPCKLDQ X5, X8, X7
	VPUNPCKHDQ X5, X8, X9

	VPUNPCKLQDQ X7, X4, X0            // row0
	VPUNPCKHQDQ X7, X4, X1            // row1
	VPUNPCKLQDQ X9, X6, X2            // row2
	VPUNPCKHQDQ X9, X6, X3            // row3

	// === Vertical butterfly ===
	VPADDD X3, X0, X4                 // a0
	VPADDD X2, X1, X5                 // a1
	VPSUBD X2, X1, X6                 // a2
	VPSUBD X3, X0, X7                 // a3

	// out_row0 = (a0 + a1 + 7) >> 4
	VPADDD X5, X4, X0
	VMOVDQU kBias7<>(SB), X1
	VPADDD X1, X0, X0
	VPSRAD $4, X0, X0                 // X0 = out_row0

	// out_row2 = (a0 - a1 + 7) >> 4
	VPSUBD X5, X4, X2
	VPADDD X1, X2, X2
	VPSRAD $4, X2, X2                 // X2 = out_row2

	// Pack a2(X6), a3(X7) to int16 for VPMADDWD.
	// VPACKSSDW X7, X6, X1 — same encoding: C5 C9 6B CF
	LONG $0xCF6BC9C5
	VPSHUFD $0x4E, X1, X9             // X9 = swapped

	// out_row1 = (a2*2217 + a3*5352 + 12000) >> 16 + (a3 != 0 ? 1 : 0)
	VPUNPCKLWD X9, X1, X3             // [a2,a3, ...]
	VMOVDQU kMul2217_5352<>(SB), X4
	// VPMADDWD X4, X3, X3 — C5 E1 F5 DC
	LONG $0xDCF5E1C5
	VMOVDQU kBias12000<>(SB), X5
	VPADDD X5, X3, X3
	VPSRAD $16, X3, X3

	// Correction: + (a3 != 0 ? 1 : 0)
	VPCMPEQD X14, X7, X4              // X4 = -1 where a3==0
	VMOVDQU kConst1<>(SB), X8
	VPANDN X8, X4, X4                 // X4 = 1 where a3!=0
	VPADDD X4, X3, X3                 // X3 = out_row1

	// out_row3 = (a3*2217 - a2*5352 + 51000) >> 16
	VPUNPCKLWD X1, X9, X5             // [a3,a2, ...]
	VMOVDQU kMul2217_n5352<>(SB), X4
	// VPMADDWD X4, X5, X5 — C5 D1 F5 EC
	LONG $0xECF5D1C5
	VMOVDQU kBias51000<>(SB), X4
	VPADDD X4, X5, X5
	VPSRAD $16, X5, X5                // X5 = out_row3

	// === Pack int32→int16 and store ===
	// VPACKSSDW X3, X0, X0 — dest=X0, src1=X0, src2=X3
	// 2-byte VEX: C5 F9 6B C3
	LONG $0xC36BF9C5                   // X0 = [row0, row1]
	// VPACKSSDW X5, X2, X2 — dest=X2, src1=X2, src2=X5
	// 2-byte VEX: C5 E9 6B D5
	LONG $0xD56BE9C5                   // X2 = [row2, row3]
	VMOVDQU X0, 0(DX)
	VMOVDQU X2, 16(DX)

	VZEROUPPER
	RET

// ============================================================================
// func iTransformOneAVX2(ref []byte, in []int16, dst []byte)
// Inverse DCT 4x4. VEX re-encoding of iTransformOneSSE2.
// Same algorithm: load int16 coeffs → vertical pass via mul1/mul2 (VPMULHW) →
// transpose → horizontal pass with bias → transpose back → add ref + clip.
// ============================================================================
TEXT ·iTransformOneAVX2(SB), NOSPLIT, $0-72
	MOVQ ref_base+0(FP), SI
	MOVQ in_base+24(FP), DI
	MOVQ dst_base+48(FP), DX

	// Load 16 int16 coefficients as 4 rows (64 bits each).
	MOVQ 0(DI), X0                    // row0
	MOVQ 8(DI), X1                    // row1
	MOVQ 16(DI), X2                   // row2
	MOVQ 24(DI), X3                   // row3

	// Load mul constants into X6/X7 (reused across both passes).
	// c1=20091, c2_i16=int16(35468)=-30068.
	MOVQ $0x4E7B4E7B4E7B4E7B, AX
	MOVQ AX, X6                       // c1
	MOVQ $0x8A8C8A8C8A8C8A8C, AX
	MOVQ AX, X7                       // c2_i16

	// === Vertical pass (int16) ===
	// a = row0 + row2, b = row0 - row2
	VPADDW X2, X0, X4                 // X4 = a (eliminated MOVO)
	VPSUBW X2, X0, X5                 // X5 = b (eliminated MOVO)

	// mul1(row1) = PMULHW(row1, c1) + row1
	// VPMULHW X6, X1, X8 — VEX.NDS.128.66.0F E5 /r
	// dest=X8(reg=0,R=1), src1=X1(vvvv=1), src2=X6(rm=6,B=0)
	// 3-byte VEX: C4 61 71 E5 C6
	LONG $0xE57161C4; BYTE $0xC6      // X8 = (row1 * 20091) >> 16
	VPADDW X1, X8, X8                 // X8 = mul1(row1)

	// mul2(row1) = PMULHW(row1, c2_i16) + row1
	// VPMULHW X7, X1, X9 — 3-byte VEX: C4 61 71 E5 CF
	LONG $0xE57161C4; BYTE $0xCF      // X9 = (row1 * -30068) >> 16
	VPADDW X1, X9, X9                 // X9 = mul2(row1)

	// mul1(row3) = PMULHW(row3, c1) + row3
	// VPMULHW X6, X3, X10 — 3-byte VEX: C4 61 61 E5 D6
	LONG $0xE56161C4; BYTE $0xD6
	VPADDW X3, X10, X10               // X10 = mul1(row3)

	// mul2(row3) = PMULHW(row3, c2_i16) + row3
	// VPMULHW X7, X3, X11 — 3-byte VEX: C4 61 61 E5 DF
	LONG $0xE56161C4; BYTE $0xDF
	VPADDW X3, X11, X11               // X11 = mul2(row3)

	// cc = mul2(row1) - mul1(row3)
	VPSUBW X10, X9, X2                // X2 = cc (eliminated MOVO)

	// d = mul1(row1) + mul2(row3)
	VPADDW X11, X8, X3                // X3 = d (eliminated MOVO)

	// tmp0 = a + d, tmp1 = b + cc, tmp2 = b - cc, tmp3 = a - d
	VPADDW X3, X4, X0                 // X0 = tmp0
	VPADDW X2, X5, X1                 // X1 = tmp1
	VPSUBW X2, X5, X8                 // X8 = tmp2
	VPSUBW X3, X4, X9                 // X9 = tmp3

	// === Transpose 4x4 int16 (rows → columns) ===
	VPUNPCKLWD X1, X0, X4             // interleave(tmp0, tmp1)
	VPUNPCKLWD X9, X8, X5             // interleave(tmp2, tmp3)
	VPUNPCKLQDQ X5, X4, X2            // [X4_low64, X5_low64]
	VPUNPCKHQDQ X5, X4, X3            // [X4_high64, X5_high64]
	VPSHUFD $0xD8, X2, X0             // col0|col1
	VPSHUFD $0xD8, X3, X2             // col2|col3

	// Unpack to individual column registers.
	VPSHUFD $0x4E, X0, X1             // X1 = col1|col0
	VPSHUFD $0x4E, X2, X3             // X3 = col3|col2

	// === Horizontal pass (int16) ===
	// Add bias +4 to col0 (dc).
	MOVQ $0x0004000400040004, AX
	MOVQ AX, X14
	VPADDW X14, X0, X0                // col0 += 4

	// a = dc + col2, b = dc - col2
	VPADDW X2, X0, X4                 // X4 = a
	VPSUBW X2, X0, X5                 // X5 = b

	// mul1(col1) = PMULHW(col1, c1) + col1
	// VPMULHW X6, X1, X8 — same encoding: C4 61 71 E5 C6
	LONG $0xE57161C4; BYTE $0xC6
	VPADDW X1, X8, X8                 // X8 = mul1(col1)

	// mul2(col1)
	// VPMULHW X7, X1, X9 — C4 61 71 E5 CF
	LONG $0xE57161C4; BYTE $0xCF
	VPADDW X1, X9, X9                 // X9 = mul2(col1)

	// mul1(col3)
	// VPMULHW X6, X3, X10 — C4 61 61 E5 D6
	LONG $0xE56161C4; BYTE $0xD6
	VPADDW X3, X10, X10               // X10 = mul1(col3)

	// mul2(col3)
	// VPMULHW X7, X3, X11 — C4 61 61 E5 DF
	LONG $0xE56161C4; BYTE $0xDF
	VPADDW X3, X11, X11               // X11 = mul2(col3)

	// cc = mul2(col1) - mul1(col3)
	VPSUBW X10, X9, X2

	// d = mul1(col1) + mul2(col3)
	VPADDW X11, X8, X3

	// out0 = (a + d) >> 3, out1 = (b + cc) >> 3
	// out2 = (b - cc) >> 3, out3 = (a - d) >> 3
	VPADDW X3, X4, X0
	VPSRAW $3, X0, X0                 // X0 = out_col0

	VPADDW X2, X5, X1
	VPSRAW $3, X1, X1                 // X1 = out_col1

	VPSUBW X2, X5, X8
	VPSRAW $3, X8, X8                 // X8 = out_col2

	VPSUBW X3, X4, X9
	VPSRAW $3, X9, X9                 // X9 = out_col3

	// === Transpose back 4x4 int16 (columns → rows) ===
	VPUNPCKLWD X1, X0, X4
	VPUNPCKLWD X9, X8, X5
	VPUNPCKLQDQ X5, X4, X2
	VPUNPCKHQDQ X5, X4, X3
	VPSHUFD $0xD8, X2, X0             // row0|row1
	VPSHUFD $0xD8, X3, X2             // row2|row3

	// === Add ref and clip to [0,255] ===
	VPXOR X14, X14, X14               // zero

	// Row 0
	MOVL (SI), X4
	VPUNPCKLBW X14, X4, X4
	VPADDW X4, X0, X5
	// VPACKUSWB X5, X5, X5 — VEX.NDS.128.66.0F 67 /r
	// dest=X5(reg=5), src1=X5(vvvv=5), src2=X5(rm=5)
	// 2-byte VEX: C5 D1 67 ED
	LONG $0xED67D1C5
	MOVL X5, (DX)

	// Row 1
	MOVL 32(SI), X4
	VPUNPCKLBW X14, X4, X4
	VPSHUFD $0x4E, X0, X5             // bring row1 to low 64 bits
	VPADDW X4, X5, X5
	LONG $0xED67D1C5                   // VPACKUSWB X5, X5, X5
	MOVL X5, 32(DX)

	// Row 2
	MOVL 64(SI), X4
	VPUNPCKLBW X14, X4, X4
	VPADDW X4, X2, X5
	LONG $0xED67D1C5                   // VPACKUSWB X5, X5, X5
	MOVL X5, 64(DX)

	// Row 3
	MOVL 96(SI), X4
	VPUNPCKLBW X14, X4, X4
	VPSHUFD $0x4E, X2, X5             // bring row3 to low 64 bits
	VPADDW X4, X5, X5
	LONG $0xED67D1C5                   // VPACKUSWB X5, X5, X5
	MOVL X5, 96(DX)

	VZEROUPPER
	RET
