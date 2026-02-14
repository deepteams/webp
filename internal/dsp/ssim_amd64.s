#include "textflag.h"

// BPS = 32 (stride constant)
#define BPS $32

// func sse4x4SSE2(pix, ref []byte) int
// Computes sum of squared differences for a 4x4 block with BPS stride.
TEXT ·sse4x4SSE2(SB), NOSPLIT, $0-52
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
TEXT ·sse16x16SSE2(SB), NOSPLIT, $0-52
	MOVQ pix_base+0(FP), SI   // pix pointer
	MOVQ ref_base+24(FP), DI  // ref pointer
	PXOR X6, X6               // zero register
	PXOR X7, X7               // accumulator

	MOVQ $16, CX              // row counter
	XORQ DX, DX               // offset = 0

loop:
	// Load 16 bytes for this row: process as two 8-byte halves
	// Low 8 bytes
	MOVQ (SI)(DX*1), X0
	MOVQ (DI)(DX*1), X1
	MOVO X0, X2
	MOVO X1, X3
	PUNPCKLBW X6, X0          // zero-extend low 4 bytes to words
	PUNPCKLBW X6, X1
	PSUBW X1, X0
	PMADDWL X0, X0
	PADDL X0, X7

	PSRLQ $32, X2             // shift to get bytes 4-7
	PSRLQ $32, X3
	PUNPCKLBW X6, X2
	PUNPCKLBW X6, X3
	PSUBW X3, X2
	PMADDWL X2, X2
	PADDL X2, X7

	// High 8 bytes
	MOVQ 8(SI)(DX*1), X0
	MOVQ 8(DI)(DX*1), X1
	MOVO X0, X2
	MOVO X1, X3
	PUNPCKLBW X6, X0
	PUNPCKLBW X6, X1
	PSUBW X1, X0
	PMADDWL X0, X0
	PADDL X0, X7

	PSRLQ $32, X2
	PSRLQ $32, X3
	PUNPCKLBW X6, X2
	PUNPCKLBW X6, X3
	PSUBW X3, X2
	PMADDWL X2, X2
	PADDL X2, X7

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
