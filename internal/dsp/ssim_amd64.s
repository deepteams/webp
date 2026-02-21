#include "textflag.h"

// BPS = 32 (stride constant)
#define BPS $32

// func sse4x4SSE2(pix, ref []byte) int
// Computes sum of squared differences for a 4x4 block with BPS stride.
TEXT ·sse4x4SSE2(SB), NOSPLIT, $0-56
	MOVQ pix_base+0(FP), SI   // pix pointer
	MOVQ ref_base+24(FP), DI  // ref pointer
	PXOR X6, X6               // zero register for unpacking
	PXOR X7, X7               // accumulator

	// Process 4 rows, each row: load 4 bytes, diff, square, accumulate
	// Row 0
	MOVL (SI), X0
	MOVL (DI), X1
	PUNPCKLBW X6, X0          // zero-extend bytes to words
	PUNPCKLBW X6, X1
	PSUBW X1, X0              // diff = pix - ref (signed 16-bit)
	PMADDWL X0, X0            // sum of squares: d0*d0+d1*d1, d2*d2+d3*d3
	PADDL X0, X7

	// Row 1
	MOVL 32(SI), X0           // 32 = 1*BPS
	MOVL 32(DI), X1
	PUNPCKLBW X6, X0
	PUNPCKLBW X6, X1
	PSUBW X1, X0
	PMADDWL X0, X0
	PADDL X0, X7

	// Row 2
	MOVL 64(SI), X0           // 64 = 2*BPS
	MOVL 64(DI), X1
	PUNPCKLBW X6, X0
	PUNPCKLBW X6, X1
	PSUBW X1, X0
	PMADDWL X0, X0
	PADDL X0, X7

	// Row 3
	MOVL 96(SI), X0           // 96 = 3*BPS
	MOVL 96(DI), X1
	PUNPCKLBW X6, X0
	PUNPCKLBW X6, X1
	PSUBW X1, X0
	PMADDWL X0, X0
	PADDL X0, X7

	// Horizontal sum of X7 (4 x int32 -> 1 int)
	PSHUFD $0x4e, X7, X0      // swap high/low 64-bit halves
	PADDL X0, X7
	PSHUFD $0xb1, X7, X0      // swap adjacent 32-bit words
	PADDL X0, X7
	MOVL X7, AX
	MOVQ AX, ret+48(FP)
	RET

// func sse16x16SSE2(pix, ref []byte) int
// Computes sum of squared differences for a 16x16 block with BPS stride.
TEXT ·sse16x16SSE2(SB), NOSPLIT, $0-56
	MOVQ pix_base+0(FP), SI   // pix pointer
	MOVQ ref_base+24(FP), DI  // ref pointer
	PXOR X6, X6               // zero register
	PXOR X7, X7               // accumulator

	MOVQ $16, CX              // row counter
	XORQ DX, DX               // offset = 0

loop:
	// Load 16 bytes for this row: process as two 8-byte halves
	// Low 8 bytes (0-7): MOVQ loads 8 bytes, PUNPCKLBW zero-extends all 8 to words
	MOVQ (SI)(DX*1), X0
	MOVQ (DI)(DX*1), X1
	PUNPCKLBW X6, X0
	PUNPCKLBW X6, X1
	PSUBW X1, X0
	PMADDWL X0, X0
	PADDL X0, X7

	// High 8 bytes (8-15)
	MOVQ 8(SI)(DX*1), X0
	MOVQ 8(DI)(DX*1), X1
	PUNPCKLBW X6, X0
	PUNPCKLBW X6, X1
	PSUBW X1, X0
	PMADDWL X0, X0
	PADDL X0, X7

	ADDQ $32, DX              // offset += BPS
	DECQ CX
	JNZ loop

	// Horizontal sum
	PSHUFD $0x4e, X7, X0
	PADDL X0, X7
	PSHUFD $0xb1, X7, X0
	PADDL X0, X7
	MOVL X7, AX
	MOVQ AX, ret+48(FP)
	RET

// --- Perceptual Hadamard distortion (kWeightY) ---

// Weight constants for PMADDWD: kWeightY rows packed as int16.
// Row 0+1: [38,32,20,9, 32,28,17,7]
DATA kWeightY01<>+0x00(SB)/8, $0x0009001400200026
DATA kWeightY01<>+0x08(SB)/8, $0x00070011001C0020
GLOBL kWeightY01<>(SB), (NOPTR+RODATA), $16
// Row 2+3: [20,17,10,4, 9,7,4,2]
DATA kWeightY23<>+0x00(SB)/8, $0x0004000A00110014
DATA kWeightY23<>+0x08(SB)/8, $0x0002000400070009
GLOBL kWeightY23<>(SB), (NOPTR+RODATA), $16

// func tDisto4x4SSE2(a, b []byte) int
// Computes abs(tTransform(b, kWeightY) - tTransform(a, kWeightY)) >> 5
// where tTransform is a 4x4 Hadamard transform with perceptual weighting.
// a and b are BPS=32 strided buffers.
//
// Strategy: for each block, transpose-butterfly-transpose-butterfly (matching
// the fTransformWHTSSE2 pattern), then abs + PMADDWD weight + horizontal sum.
//
// Arguments:
//   a_base+0(FP)  = pointer to a
//   a_len+8(FP)   = len (unused)
//   a_cap+16(FP)  = cap (unused)
//   b_base+24(FP) = pointer to b
//   b_len+32(FP)  = len (unused)
//   b_cap+40(FP)  = cap (unused)
//   ret+48(FP)    = return value (int)
TEXT ·tDisto4x4SSE2(SB), NOSPLIT, $0-56
	MOVQ a_base+0(FP), SI     // SI = &a[0]
	MOVQ b_base+24(FP), DI    // DI = &b[0]

	// Load weight constants.
	MOVOU kWeightY01<>(SB), X10  // [38,32,20,9, 32,28,17,7]
	MOVOU kWeightY23<>(SB), X11  // [20,17,10,4, 9,7,4,2]

	PXOR X14, X14                // zero register

	// ========== tTransform(a) ==========

	// Load 4 rows of 4 bytes at stride BPS=32, zero-extend to int16.
	MOVD (SI), X0                // row0 (4 bytes)
	PUNPCKLBW X14, X0            // row0 int16
	MOVD 32(SI), X1              // row1
	PUNPCKLBW X14, X1
	MOVD 64(SI), X2              // row2
	PUNPCKLBW X14, X2
	MOVD 96(SI), X3              // row3
	PUNPCKLBW X14, X3

	// Transpose 4x4 int16 (rows -> columns).
	MOVO X0, X4
	PUNPCKLWL X1, X4             // interleave(row0, row1)
	MOVO X2, X5
	PUNPCKLWL X3, X5             // interleave(row2, row3)
	MOVO X4, X6
	MOVLHPS X5, X4               // [low(X4), low(X5)]
	MOVHLPS X6, X5               // [high(X6), high(X5)]
	PSHUFD $0xD8, X4, X0         // [col0 | col1]
	PSHUFD $0xD8, X5, X2         // [col2 | col3]

	// Butterfly pass 1 (horizontal Hadamard on transposed columns).
	MOVO X0, X4
	PADDW X2, X0                 // X0 = [a0 | a1]
	PSUBW X2, X4                 // X4 = [a3 | a2]
	PSHUFD $0x4E, X0, X1         // X1 = [a1 | a0]
	PSHUFD $0x4E, X4, X5         // X5 = [a2 | a3]
	MOVO X0, X2
	MOVO X4, X3
	PADDW X1, X0                 // X0_low = row0 = a0+a1
	PSUBW X1, X2                 // X2_low = row3 = a0-a1
	PADDW X5, X4                 // X4_low = row1 = a3+a2
	PSUBW X5, X3                 // X3_low = row2 = a3-a2

	// Transpose back (columns -> rows).
	MOVO X0, X5
	PUNPCKLWL X4, X5             // interleave(row0, row1)
	MOVO X3, X6
	PUNPCKLWL X2, X6             // interleave(row2, row3)
	MOVO X5, X7
	MOVLHPS X6, X5
	MOVHLPS X7, X6
	PSHUFD $0xD8, X5, X0         // [col0 | col1]
	PSHUFD $0xD8, X6, X2         // [col2 | col3]

	// Butterfly pass 2 (vertical Hadamard).
	MOVO X0, X4
	PADDW X2, X0                 // X0 = [a0 | a1]
	PSUBW X2, X4                 // X4 = [a3 | a2]
	PSHUFD $0x4E, X0, X1         // X1 = [a1 | a0]
	PSHUFD $0x4E, X4, X5         // X5 = [a2 | a3]
	MOVO X0, X2
	MOVO X4, X3
	PADDW X1, X0                 // X0_low = h_row0 = a0+a1
	PSUBW X1, X2                 // X2_low = h_row3 = a0-a1
	PADDW X5, X4                 // X4_low = h_row1 = a3+a2
	PSUBW X5, X3                 // X3_low = h_row2 = a3-a2

	// Pack: X0 = [h_row0 | h_row1], X3 = [h_row2 | h_row3].
	MOVLHPS X4, X0               // X0 = [row0 | row1]
	MOVLHPS X2, X3               // X3 = [row2 | row3]

	// Absolute values: abs(x) = (x ^ (x>>15)) - (x>>15).
	MOVO X0, X5
	PSRAW $15, X5
	PXOR X5, X0
	PSUBW X5, X0
	MOVO X3, X5
	PSRAW $15, X5
	PXOR X5, X3
	PSUBW X5, X3

	// Weighted sum via PMADDWD: multiply abs values by weights, produce int32 sums.
	MOVO X10, X6                 // copy weights (preserve for block b)
	PMADDWL X0, X6               // X6 = 4 int32 from rows 0,1
	MOVO X11, X7                 // copy weights
	PMADDWL X3, X7               // X7 = 4 int32 from rows 2,3
	PADDL X7, X6

	// Horizontal sum of 4 int32.
	PSHUFD $0x4E, X6, X1         // swap high/low 64-bit halves
	PADDL X1, X6
	PSHUFD $0xB1, X6, X1         // swap adjacent 32-bit words
	PADDL X1, X6
	MOVL X6, R8                  // R8 = sum_a

	// ========== tTransform(b) ==========

	// Load 4 rows.
	MOVD (DI), X0
	PUNPCKLBW X14, X0
	MOVD 32(DI), X1
	PUNPCKLBW X14, X1
	MOVD 64(DI), X2
	PUNPCKLBW X14, X2
	MOVD 96(DI), X3
	PUNPCKLBW X14, X3

	// Transpose.
	MOVO X0, X4
	PUNPCKLWL X1, X4
	MOVO X2, X5
	PUNPCKLWL X3, X5
	MOVO X4, X6
	MOVLHPS X5, X4
	MOVHLPS X6, X5
	PSHUFD $0xD8, X4, X0
	PSHUFD $0xD8, X5, X2

	// Butterfly pass 1.
	MOVO X0, X4
	PADDW X2, X0
	PSUBW X2, X4
	PSHUFD $0x4E, X0, X1
	PSHUFD $0x4E, X4, X5
	MOVO X0, X2
	MOVO X4, X3
	PADDW X1, X0
	PSUBW X1, X2
	PADDW X5, X4
	PSUBW X5, X3

	// Transpose back.
	MOVO X0, X5
	PUNPCKLWL X4, X5
	MOVO X3, X6
	PUNPCKLWL X2, X6
	MOVO X5, X7
	MOVLHPS X6, X5
	MOVHLPS X7, X6
	PSHUFD $0xD8, X5, X0
	PSHUFD $0xD8, X6, X2

	// Butterfly pass 2.
	MOVO X0, X4
	PADDW X2, X0
	PSUBW X2, X4
	PSHUFD $0x4E, X0, X1
	PSHUFD $0x4E, X4, X5
	MOVO X0, X2
	MOVO X4, X3
	PADDW X1, X0
	PSUBW X1, X2
	PADDW X5, X4
	PSUBW X5, X3

	// Pack.
	MOVLHPS X4, X0
	MOVLHPS X2, X3

	// Abs.
	MOVO X0, X5
	PSRAW $15, X5
	PXOR X5, X0
	PSUBW X5, X0
	MOVO X3, X5
	PSRAW $15, X5
	PXOR X5, X3
	PSUBW X5, X3

	// Weighted sum (destroy weight regs, no longer needed).
	PMADDWL X0, X10              // X10 = 4 int32
	PMADDWL X3, X11              // X11 = 4 int32
	PADDL X11, X10

	// Horizontal sum.
	PSHUFD $0x4E, X10, X1
	PADDL X1, X10
	PSHUFD $0xB1, X10, X1
	PADDL X1, X10
	MOVL X10, AX                 // AX = sum_b

	// result = abs(sum_b - sum_a) >> 5
	SUBQ R8, AX                  // AX = sum_b - sum_a
	MOVQ AX, BX
	SARQ $63, BX                 // BX = sign mask (all 1s if negative)
	XORQ BX, AX
	SUBQ BX, AX                  // AX = abs(sum_b - sum_a)
	SHRQ $5, AX                  // AX >>= 5

	MOVQ AX, ret+48(FP)
	RET
