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
	// Row 0: byte offsets 0, 32, 64, 96
	MOVQ X0, AX
	MOVW AX, 0(DI)
	SHRQ $16, AX
	MOVW AX, 32(DI)
	SHRQ $16, AX
	MOVW AX, 64(DI)
	SHRQ $16, AX
	MOVW AX, 96(DI)

	// Row 1: byte offsets 128, 160, 192, 224
	PSHUFD $0x4E, X0, X0   // swap halves: frow1 now in low 64
	MOVQ X0, AX
	MOVW AX, 128(DI)
	SHRQ $16, AX
	MOVW AX, 160(DI)
	SHRQ $16, AX
	MOVW AX, 192(DI)
	SHRQ $16, AX
	MOVW AX, 224(DI)

	// Row 2: byte offsets 256, 288, 320, 352
	MOVQ X2, AX
	MOVW AX, 256(DI)
	SHRQ $16, AX
	MOVW AX, 288(DI)
	SHRQ $16, AX
	MOVW AX, 320(DI)
	SHRQ $16, AX
	MOVW AX, 352(DI)

	// Row 3: byte offsets 384, 416, 448, 480
	PSHUFD $0x4E, X2, X2
	MOVQ X2, AX
	MOVW AX, 384(DI)
	SHRQ $16, AX
	MOVW AX, 416(DI)
	SHRQ $16, AX
	MOVW AX, 448(DI)
	SHRQ $16, AX
	MOVW AX, 480(DI)

	RET
