#include "textflag.h"

// func fTransformWHTSSE2(in []int16, out []int16)
// Forward Walsh-Hadamard Transform on flat 4x4 DC coefficients (stride 4).
// SSE2 vectorized: transpose-butterfly-transpose approach.
// ~52 SIMD instructions vs ~250 scalar instructions.
TEXT ·fTransformWHTSSE2(SB), NOSPLIT, $0-48
	MOVQ in_base+0(FP), SI
	MOVQ out_base+24(FP), DI

	// Load 4 rows of 4 int16 each (64 bits per row).
	MOVQ 0(SI), X0        // row0 = [r0c0, r0c1, r0c2, r0c3]
	MOVQ 8(SI), X1        // row1 = [r1c0, r1c1, r1c2, r1c3]
	MOVQ 16(SI), X2       // row2 = [r2c0, r2c1, r2c2, r2c3]
	MOVQ 24(SI), X3       // row3 = [r3c0, r3c1, r3c2, r3c3]

	// === 4x4 int16 transpose (rows → columns) ===
	// Step 1: interleave words from row pairs
	MOVO X0, X4
	PUNPCKLWL X1, X4       // X4 = [r0c0,r1c0, r0c1,r1c1, r0c2,r1c2, r0c3,r1c3]
	MOVO X2, X5
	PUNPCKLWL X3, X5       // X5 = [r2c0,r3c0, r2c1,r3c1, r2c2,r3c2, r2c3,r3c3]
	// Step 2: combine 64-bit halves (MOVLHPS/MOVHLPS for qword granularity)
	MOVO X4, X6
	MOVLHPS X5, X4         // X4 = [X4_low64, X5_low64]
	MOVHLPS X6, X5         // X5 = [X6_high64, X5_high64]
	// Step 3: group columns via dword shuffle
	PSHUFD $0xD8, X4, X0   // X0 = [col0 | col1]
	PSHUFD $0xD8, X5, X2   // X2 = [col2 | col3]

	// === Pass 1: row-wise butterfly (on transposed columns) ===
	// a0=col0+col2, a1=col1+col3, a3=col0-col2, a2=col1-col3
	// out: tcol0=a0+a1, tcol1=a3+a2, tcol2=a3-a2, tcol3=a0-a1
	MOVO X0, X4
	PADDW X2, X0           // X0 = [a0 | a1]
	PSUBW X2, X4           // X4 = [a3 | a2]
	PSHUFD $0x4E, X0, X1   // X1 = [a1 | a0] (swap 64-bit halves)
	PSHUFD $0x4E, X4, X5   // X5 = [a2 | a3]
	MOVO X0, X2
	MOVO X4, X3
	PADDW X1, X0           // X0_low = tcol0 = a0+a1
	PSUBW X1, X2           // X2_low = tcol3 = a0-a1
	PADDW X5, X4           // X4_low = tcol1 = a3+a2
	PSUBW X5, X3           // X3_low = tcol2 = a3-a2

	// === 4x4 int16 transpose back (columns → rows) ===
	MOVO X0, X5
	PUNPCKLWL X4, X5       // X5 = interleave(tcol0, tcol1)
	MOVO X3, X6
	PUNPCKLWL X2, X6       // X6 = interleave(tcol2, tcol3)
	MOVO X5, X7
	MOVLHPS X6, X5         // X5 = [X5_low64, X6_low64]
	MOVHLPS X7, X6         // X6 = [X7_high64, X6_high64]
	PSHUFD $0xD8, X5, X0   // X0 = [row0 | row1]
	PSHUFD $0xD8, X6, X2   // X2 = [row2 | row3]

	// === Pass 2: column-wise butterfly ===
	// a0=row0+row2, a1=row1+row3, a3=row0-row2, a2=row1-row3
	// out: frow0=a0+a1, frow1=a3+a2, frow2=a3-a2, frow3=a0-a1
	MOVO X0, X4
	PADDW X2, X0           // X0 = [a0 | a1]
	PSUBW X2, X4           // X4 = [a3 | a2]
	PSHUFD $0x4E, X0, X1   // X1 = [a1 | a0]
	PSHUFD $0x4E, X4, X5   // X5 = [a2 | a3]
	MOVO X0, X2
	MOVO X4, X3
	PADDW X1, X0           // X0_low = frow0
	PSUBW X1, X2           // X2_low = frow3
	PADDW X5, X4           // X4_low = frow1
	PSUBW X5, X3           // X3_low = frow2

	// Arithmetic shift right by 1
	PSRAW $1, X0
	PSRAW $1, X4
	PSRAW $1, X3
	PSRAW $1, X2

	// Store 4 rows (64 bits each)
	MOVQ X0, 0(DI)         // out[0..3]
	MOVQ X4, 8(DI)         // out[4..7]
	MOVQ X3, 16(DI)        // out[8..11]
	MOVQ X2, 24(DI)        // out[12..15]

	RET

// func transformWHTSSE2(in []int16, out []int16)
// Inverse WHT. in: 16 int16 coeffs. out: 16 DC values at stride-16 positions.
// SSE2 vectorized: column butterfly → transpose → row butterfly → transpose → scatter.
// ~80 instructions vs ~230 scalar.
TEXT ·transformWHTSSE2(SB), NOSPLIT, $0-48
	MOVQ in_base+0(FP), SI
	MOVQ out_base+24(FP), DI

	// Load 16 int16 values as 2 packed registers (2 rows per register).
	MOVOU 0(SI), X0        // X0 = [row0 | row1]
	MOVOU 16(SI), X1       // X1 = [row2 | row3]

	// === Pass 1: column-wise butterfly ===
	// Pairs: (row0,row3) and (row1,row2). Swap halves of X1 to pair correctly.
	PSHUFD $0x4E, X1, X3   // X3 = [row3 | row2]
	MOVO X0, X4
	PADDW X3, X0           // X0 = [row0+row3 | row1+row2] = [a0 | a1]
	PSUBW X3, X4           // X4 = [row0-row3 | row1-row2] = [a3 | a2]
	// Second stage: cross-half add/sub
	PSHUFD $0x4E, X0, X1   // X1 = [a1 | a0]
	PSHUFD $0x4E, X4, X5   // X5 = [a2 | a3]
	MOVO X0, X2
	MOVO X4, X3
	PADDW X1, X0           // X0_low = trow0 = a0+a1
	PSUBW X1, X2           // X2_low = trow2 = a0-a1
	PADDW X5, X4           // X4_low = trow1 = a3+a2
	PSUBW X5, X3           // X3_low = trow3 = a3-a2

	// === Transpose 4x4 int16 (rows → columns) ===
	MOVO X0, X5
	PUNPCKLWL X4, X5       // interleave(trow0, trow1)
	MOVO X2, X6
	PUNPCKLWL X3, X6       // interleave(trow2, trow3)
	MOVO X5, X7
	MOVLHPS X6, X5         // X5 = [X5_low64, X6_low64]
	MOVHLPS X7, X6         // X6 = [X7_high64, X6_high64]
	PSHUFD $0xD8, X5, X0   // X0 = [tcol0 | tcol1]
	PSHUFD $0xD8, X6, X2   // X2 = [tcol2 | tcol3]

	// === Add bias +3 to tcol0 (low 64 bits of X0) ===
	MOVQ $0x0003000300030003, AX
	MOVQ AX, X6            // X6 = [3,3,3,3, 0,0,0,0]
	PADDW X6, X0           // tcol0 += 3, tcol1 unchanged

	// === Pass 2: row-wise butterfly (on transposed columns) ===
	// Pairs: (tcol0_biased,tcol3) and (tcol1,tcol2).
	PSHUFD $0x4E, X2, X3   // X3 = [tcol3 | tcol2]
	MOVO X0, X4
	PADDW X3, X0           // X0 = [a0 | a1]
	PSUBW X3, X4           // X4 = [a3 | a2]
	PSHUFD $0x4E, X0, X1   // X1 = [a1 | a0]
	PSHUFD $0x4E, X4, X5   // X5 = [a2 | a3]
	MOVO X0, X2
	MOVO X4, X3
	PADDW X1, X0           // X0_low = fcol0 = a0+a1
	PSUBW X1, X2           // X2_low = fcol2 = a0-a1
	PADDW X5, X4           // X4_low = fcol1 = a3+a2
	PSUBW X5, X3           // X3_low = fcol3 = a3-a2

	// Arithmetic shift right by 3
	PSRAW $3, X0
	PSRAW $3, X4
	PSRAW $3, X2
	PSRAW $3, X3

	// === Transpose back (columns → rows) ===
	MOVO X0, X5
	PUNPCKLWL X4, X5       // interleave(fcol0, fcol1)
	MOVO X2, X6
	PUNPCKLWL X3, X6       // interleave(fcol2, fcol3)
	MOVO X5, X7
	MOVLHPS X6, X5         // X5 = [X5_low64, X6_low64]
	MOVHLPS X7, X6         // X6 = [X7_high64, X6_high64]
	PSHUFD $0xD8, X5, X0   // X0 = [frow0 | frow1]
	PSHUFD $0xD8, X6, X2   // X2 = [frow2 | frow3]

	// === Scatter store: each row's 4 values at stride 16 (32 bytes) ===
	// Using PEXTRW to extract words directly from XMM registers,
	// eliminating serial SHRQ dependency chains for better ILP.

	// Row 0 (words 0-3 of X0): byte offsets 0, 32, 64, 96
	PEXTRW $0, X0, AX
	MOVW AX, 0(DI)
	PEXTRW $1, X0, AX
	MOVW AX, 32(DI)
	PEXTRW $2, X0, AX
	MOVW AX, 64(DI)
	PEXTRW $3, X0, AX
	MOVW AX, 96(DI)

	// Row 1 (words 4-7 of X0): byte offsets 128, 160, 192, 224
	PEXTRW $4, X0, AX
	MOVW AX, 128(DI)
	PEXTRW $5, X0, AX
	MOVW AX, 160(DI)
	PEXTRW $6, X0, AX
	MOVW AX, 192(DI)
	PEXTRW $7, X0, AX
	MOVW AX, 224(DI)

	// Row 2 (words 0-3 of X2): byte offsets 256, 288, 320, 352
	PEXTRW $0, X2, AX
	MOVW AX, 256(DI)
	PEXTRW $1, X2, AX
	MOVW AX, 288(DI)
	PEXTRW $2, X2, AX
	MOVW AX, 320(DI)
	PEXTRW $3, X2, AX
	MOVW AX, 352(DI)

	// Row 3 (words 4-7 of X2): byte offsets 384, 416, 448, 480
	PEXTRW $4, X2, AX
	MOVW AX, 384(DI)
	PEXTRW $5, X2, AX
	MOVW AX, 416(DI)
	PEXTRW $6, X2, AX
	MOVW AX, 448(DI)
	PEXTRW $7, X2, AX
	MOVW AX, 480(DI)

	RET

// func fTransformSSE2(src, ref []byte, out []int16)
// Forward DCT 4x4. src/ref stride=BPS=32. SSE2 vectorized.
// Strategy: load diffs → widen to int32 → transpose → horizontal butterfly →
// transpose back → vertical butterfly → pack int32→int16 → store.
TEXT ·fTransformSSE2(SB), NOSPLIT, $0-72
	MOVQ src_base+0(FP), SI
	MOVQ ref_base+24(FP), DI
	MOVQ out_base+48(FP), DX

	PXOR X14, X14               // zero register

	// === Load 4 rows of diffs (src - ref), widen to int32 ===
	// Row 0
	MOVL (SI), X0
	MOVL (DI), X1
	PUNPCKLBW X14, X0
	PUNPCKLBW X14, X1
	PSUBW X1, X0                // X0 = row0 diff (int16)
	MOVO X0, X4
	PSRAW $15, X4
	PUNPCKLWL X4, X0            // X0 = row0 (4×int32)

	// Row 1
	MOVL 32(SI), X1
	MOVL 32(DI), X2
	PUNPCKLBW X14, X1
	PUNPCKLBW X14, X2
	PSUBW X2, X1
	MOVO X1, X4
	PSRAW $15, X4
	PUNPCKLWL X4, X1            // X1 = row1

	// Row 2
	MOVL 64(SI), X2
	MOVL 64(DI), X3
	PUNPCKLBW X14, X2
	PUNPCKLBW X14, X3
	PSUBW X3, X2
	MOVO X2, X4
	PSRAW $15, X4
	PUNPCKLWL X4, X2            // X2 = row2

	// Row 3
	MOVL 96(SI), X3
	MOVL 96(DI), X4
	PUNPCKLBW X14, X3
	PUNPCKLBW X14, X4
	PSUBW X4, X3
	MOVO X3, X4
	PSRAW $15, X4
	PUNPCKLWL X4, X3            // X3 = row3

	// === Transpose 4×4 int32 (rows → columns) ===
	MOVO X0, X4
	PUNPCKLLQ X1, X4            // [r0c0,r1c0, r0c1,r1c1]
	MOVO X0, X5
	PUNPCKHLQ X1, X5            // [r0c2,r1c2, r0c3,r1c3]
	MOVO X2, X6
	PUNPCKLLQ X3, X6            // [r2c0,r3c0, r2c1,r3c1]
	MOVO X2, X7
	PUNPCKHLQ X3, X7            // [r2c2,r3c2, r2c3,r3c3]

	MOVO X4, X0
	MOVLHPS X6, X0              // X0 = col0
	MOVO X6, X1
	MOVHLPS X4, X1              // X1 = col1
	MOVO X5, X2
	MOVLHPS X7, X2              // X2 = col2
	MOVO X7, X3
	MOVHLPS X5, X3              // X3 = col3

	// === Horizontal butterfly ===
	// a0=d0+d3, a1=d1+d2, a2=d1-d2, a3=d0-d3
	MOVO X0, X4                 // save d0
	MOVO X1, X5                 // save d1
	PADDL X3, X0                // a0
	PADDL X2, X1                // a1
	PSUBL X2, X5                // a2
	PSUBL X3, X4                // a3

	// tmp0 = (a0 + a1) << 3
	MOVO X0, X6
	PADDL X1, X6
	PSLLL $3, X6                // X6 = tmp0

	// tmp2 = (a0 - a1) << 3
	PSUBL X1, X0
	PSLLL $3, X0                // X0 = tmp2

	// Pack a2(X5), a3(X4) to int16 for PMADDWD
	MOVO X5, X7
	PACKSSLW X4, X7             // X7 = [a2_0..3, a3_0..3]
	PSHUFD $0x4E, X7, X8        // X8 = [a3_0..3, a2_0..3]

	// tmp1 = (a2*2217 + a3*5352 + 1812) >> 9
	MOVO X7, X9
	PUNPCKLWL X8, X9            // [a2_0,a3_0, a2_1,a3_1, ...]
	MOVQ $0x14E808A9, AX        // [2217, 5352] packed
	MOVQ AX, X10
	PSHUFD $0x00, X10, X10
	PMADDWL X10, X9             // a2*2217 + a3*5352
	MOVL $1812, AX
	MOVQ AX, X12
	PSHUFD $0x00, X12, X12
	PADDL X12, X9
	PSRAL $9, X9                // X9 = tmp1

	// tmp3 = (a3*2217 - a2*5352 + 937) >> 9
	MOVO X8, X12
	PUNPCKLWL X7, X12           // [a3_0,a2_0, a3_1,a2_1, ...]
	MOVL $-350746455, AX        // [2217, -5352] = 0xEB1808A9
	MOVQ AX, X11
	PSHUFD $0x00, X11, X11
	PMADDWL X11, X12            // a3*2217 - a2*5352
	MOVL $937, AX
	MOVQ AX, X13
	PSHUFD $0x00, X13, X13
	PADDL X13, X12
	PSRAL $9, X12               // X12 = tmp3

	// === Transpose back (columns → rows for vertical pass) ===
	// X6=tmp0, X9=tmp1, X0=tmp2, X12=tmp3
	MOVO X6, X4
	PUNPCKLLQ X9, X4
	MOVO X6, X5
	PUNPCKHLQ X9, X5
	MOVO X0, X7
	PUNPCKLLQ X12, X7
	MOVO X0, X8
	PUNPCKHLQ X12, X8

	MOVO X4, X0
	MOVLHPS X7, X0              // row0
	MOVO X7, X1
	MOVHLPS X4, X1              // row1
	MOVO X5, X2
	MOVLHPS X8, X2              // row2
	MOVO X8, X3
	MOVHLPS X5, X3              // row3

	// === Vertical butterfly ===
	MOVO X0, X4                 // save d0
	MOVO X1, X5                 // save d1
	PADDL X3, X0                // a0
	PADDL X2, X1                // a1
	PSUBL X2, X5                // a2
	PSUBL X3, X4                // a3

	// out_row0 = (a0 + a1 + 7) >> 4
	MOVO X0, X6
	PADDL X1, X6
	MOVL $7, AX
	MOVQ AX, X12
	PSHUFD $0x00, X12, X12
	PADDL X12, X6
	PSRAL $4, X6                // X6 = out_row0

	// out_row2 = (a0 - a1 + 7) >> 4
	PSUBL X1, X0
	PADDL X12, X0
	PSRAL $4, X0                // X0 = out_row2

	// Pack a2(X5), a3(X4) to int16 for PMADDWD
	MOVO X5, X7
	PACKSSLW X4, X7
	PSHUFD $0x4E, X7, X8

	// out_row1 = (a2*2217 + a3*5352 + 12000) >> 16 + (a3 != 0 ? 1 : 0)
	MOVO X7, X9
	PUNPCKLWL X8, X9
	MOVQ $0x14E808A9, AX
	MOVQ AX, X10
	PSHUFD $0x00, X10, X10
	PMADDWL X10, X9
	MOVL $12000, AX
	MOVQ AX, X12
	PSHUFD $0x00, X12, X12
	PADDL X12, X9
	PSRAL $16, X9

	// Correction: + (a3 != 0 ? 1 : 0)
	MOVO X4, X13
	PCMPEQL X14, X13            // X13 = -1 where a3==0
	MOVL $1, AX
	MOVQ AX, X15
	PSHUFD $0x00, X15, X15
	PANDN X15, X13              // X13 = 1 where a3!=0
	PADDL X13, X9               // X9 = out_row1

	// out_row3 = (a3*2217 - a2*5352 + 51000) >> 16
	MOVO X8, X12
	PUNPCKLWL X7, X12
	MOVL $-350746455, AX        // [2217, -5352] = 0xEB1808A9
	MOVQ AX, X11
	PSHUFD $0x00, X11, X11
	PMADDWL X11, X12
	MOVL $51000, AX
	MOVQ AX, X13
	PSHUFD $0x00, X13, X13
	PADDL X13, X12
	PSRAL $16, X12              // X12 = out_row3

	// === Pack int32→int16 and store ===
	PACKSSLW X9, X6             // X6 = [row0, row1]
	PACKSSLW X12, X0            // X0 = [row2, row3]
	MOVOU X6, 0(DX)
	MOVOU X0, 16(DX)

	RET

// func iTransformOneSSE2(ref []byte, in []int16, dst []byte)
// Inverse DCT 4x4 for encoder path. ref/dst stride=BPS=32.
// SSE2 vectorized: widen int16→int32, vertical butterfly with mul1/mul2 via
// PMULHW, transpose, horizontal butterfly, add ref, clip via PACKUSWB.
// Constants: c1=20091, c2=35468 (as int16: 20091, -30068).
// mul1(x) = PMULHW(x,c1) + x;  mul2(x) = PMULHW(x,c2_i16) + x.
TEXT ·iTransformOneSSE2(SB), NOSPLIT, $0-72
	MOVQ ref_base+0(FP), SI
	MOVQ in_base+24(FP), DI
	MOVQ dst_base+48(FP), DX

	// Load 16 int16 coefficients as 4 rows (64 bits each).
	MOVQ 0(DI), X0              // row0 = in[0..3]
	MOVQ 8(DI), X1              // row1 = in[4..7]
	MOVQ 16(DI), X2             // row2 = in[8..11]
	MOVQ 24(DI), X3             // row3 = in[12..15]

	// Broadcast mul constants: c1=20091, c2_i16=int16(35468)=-30068.
	MOVQ $0x4E7B4E7B4E7B4E7B, AX  // 20091 = 0x4E7B, repeated 4 times
	MOVQ AX, X12
	MOVQ $0x8A8C8A8C8A8C8A8C, AX  // -30068 = 0x8A8C (int16), repeated
	MOVQ AX, X13

	// === Vertical pass (int16) ===
	// a = row0 + row2, b = row0 - row2
	MOVO X0, X4
	PADDW X2, X4                // X4 = a = row0 + row2
	MOVO X0, X5
	PSUBW X2, X5                // X5 = b = row0 - row2

	// mul1(row1) = PMULHW(row1, c1) + row1
	MOVO X1, X6
	PMULHW X12, X6              // (row1 * 20091) >> 16
	PADDW X1, X6                // X6 = mul1(row1)

	// mul2(row1) = PMULHW(row1, c2_i16) + row1
	MOVO X1, X7
	PMULHW X13, X7              // (row1 * -30068) >> 16
	PADDW X1, X7                // X7 = mul2(row1)

	// mul1(row3) = PMULHW(row3, c1) + row3
	MOVO X3, X8
	PMULHW X12, X8
	PADDW X3, X8                // X8 = mul1(row3)

	// mul2(row3) = PMULHW(row3, c2_i16) + row3
	MOVO X3, X9
	PMULHW X13, X9
	PADDW X3, X9                // X9 = mul2(row3)

	// cc = mul2(row1) - mul1(row3)
	MOVO X7, X10
	PSUBW X8, X10               // X10 = cc

	// d = mul1(row1) + mul2(row3)
	MOVO X6, X11
	PADDW X9, X11               // X11 = d

	// tmp0 = a + d, tmp1 = b + cc, tmp2 = b - cc, tmp3 = a - d
	MOVO X4, X0
	PADDW X11, X0               // X0 = tmp0 (int16)
	MOVO X5, X1
	PADDW X10, X1               // X1 = tmp1
	MOVO X5, X2
	PSUBW X10, X2               // X2 = tmp2
	MOVO X4, X3
	PSUBW X11, X3               // X3 = tmp3

	// === Transpose 4x4 int16 (rows → columns) ===
	// Input: X0=tmp0, X1=tmp1, X2=tmp2, X3=tmp3 (low 64 bits used)
	MOVO X0, X4
	PUNPCKLWL X1, X4            // [t0c0,t1c0, t0c1,t1c1, t0c2,t1c2, t0c3,t1c3]
	MOVO X2, X5
	PUNPCKLWL X3, X5            // [t2c0,t3c0, t2c1,t3c1, t2c2,t3c2, t2c3,t3c3]
	MOVO X4, X6
	MOVLHPS X5, X4              // [X4_low64, X5_low64]
	MOVHLPS X6, X5              // [X6_high64, X5_high64]
	PSHUFD $0xD8, X4, X0        // col0|col1
	PSHUFD $0xD8, X5, X2        // col2|col3

	// Unpack to individual column registers.
	MOVO X0, X1
	PSHUFD $0x4E, X0, X1        // X1 = col1|col0
	// X0 low64 = col0, X1 low64 = col1 (from high64 of X0)
	// X2 low64 = col2
	MOVO X2, X3
	PSHUFD $0x4E, X2, X3        // X3 = col3|col2

	// === Horizontal pass (int16) ===
	// Add bias +4 to col0 (dc).
	MOVQ $0x0004000400040004, AX
	MOVQ AX, X14
	PADDW X14, X0               // col0 += 4

	// a = dc + col2, b = dc - col2
	MOVO X0, X4
	PADDW X2, X4                // X4 = a
	MOVO X0, X5
	PSUBW X2, X5                // X5 = b

	// mul1(col1) = PMULHW(col1, c1) + col1
	MOVO X1, X6
	PMULHW X12, X6
	PADDW X1, X6                // X6 = mul1(col1)

	// mul2(col1) = PMULHW(col1, c2_i16) + col1
	MOVO X1, X7
	PMULHW X13, X7
	PADDW X1, X7                // X7 = mul2(col1)

	// mul1(col3) = PMULHW(col3, c1) + col3
	MOVO X3, X8
	PMULHW X12, X8
	PADDW X3, X8                // X8 = mul1(col3)

	// mul2(col3) = PMULHW(col3, c2_i16) + col3
	MOVO X3, X9
	PMULHW X13, X9
	PADDW X3, X9                // X9 = mul2(col3)

	// cc = mul2(col1) - mul1(col3)
	MOVO X7, X10
	PSUBW X8, X10               // X10 = cc

	// d = mul1(col1) + mul2(col3)
	MOVO X6, X11
	PADDW X9, X11               // X11 = d

	// out0 = (a + d) >> 3, out1 = (b + cc) >> 3
	// out2 = (b - cc) >> 3, out3 = (a - d) >> 3
	MOVO X4, X0
	PADDW X11, X0
	PSRAW $3, X0                // X0 = out_col0

	MOVO X5, X1
	PADDW X10, X1
	PSRAW $3, X1                // X1 = out_col1

	MOVO X5, X2
	PSUBW X10, X2
	PSRAW $3, X2                // X2 = out_col2

	MOVO X4, X3
	PSUBW X11, X3
	PSRAW $3, X3                // X3 = out_col3

	// === Transpose back 4x4 int16 (columns → rows) ===
	MOVO X0, X4
	PUNPCKLWL X1, X4
	MOVO X2, X5
	PUNPCKLWL X3, X5
	MOVO X4, X6
	MOVLHPS X5, X4
	MOVHLPS X6, X5
	PSHUFD $0xD8, X4, X0        // row0|row1
	PSHUFD $0xD8, X5, X2        // row2|row3

	// === Add ref and clip to [0,255] ===
	// Process each row individually: load ref, widen, add IDCT, pack, store.
	// X0 = [row0 | row1], X2 = [row2 | row3]
	PXOR X14, X14               // zero

	// Row 0
	MOVL (SI), X4               // ref row0 (4 bytes)
	PUNPCKLBW X14, X4           // widen uint8→uint16
	MOVO X0, X5
	PADDW X4, X5                // row0 + ref_row0 (low 64 bits valid)
	PACKUSWB X5, X5             // clip to [0,255]
	MOVL X5, (DX)               // store dst row0

	// Row 1
	MOVL 32(SI), X4             // ref row1
	PUNPCKLBW X14, X4
	PSHUFD $0x4E, X0, X5        // bring row1 to low 64 bits
	PADDW X4, X5
	PACKUSWB X5, X5
	MOVL X5, 32(DX)             // store dst row1

	// Row 2
	MOVL 64(SI), X4             // ref row2
	PUNPCKLBW X14, X4
	MOVO X2, X5
	PADDW X4, X5
	PACKUSWB X5, X5
	MOVL X5, 64(DX)             // store dst row2

	// Row 3
	MOVL 96(SI), X4             // ref row3
	PUNPCKLBW X14, X4
	PSHUFD $0x4E, X2, X5        // bring row3 to low 64 bits
	PADDW X4, X5
	PACKUSWB X5, X5
	MOVL X5, 96(DX)             // store dst row3

	RET
