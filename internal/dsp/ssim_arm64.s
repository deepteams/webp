#include "textflag.h"

#define BPS 32

// func sse4x4NEON(pix, ref []byte) int
// SSE for 4x4 block with BPS=32 stride. Scalar ARM64 implementation.
TEXT ·sse4x4NEON(SB), NOSPLIT, $0-56
	MOVD pix_base+0(FP), R0    // pix
	MOVD ref_base+24(FP), R1   // ref
	MOVD $0, R5                // sum = 0
	MOVD $4, R8                // row counter

sse4x4_row:
	// Unrolled 4 pixels per row
	MOVBU (R0), R2
	MOVBU (R1), R3
	SUB R3, R2, R4
	MUL R4, R4, R6
	ADD R6, R5

	MOVBU 1(R0), R2
	MOVBU 1(R1), R3
	SUB R3, R2, R4
	MUL R4, R4, R6
	ADD R6, R5

	MOVBU 2(R0), R2
	MOVBU 2(R1), R3
	SUB R3, R2, R4
	MUL R4, R4, R6
	ADD R6, R5

	MOVBU 3(R0), R2
	MOVBU 3(R1), R3
	SUB R3, R2, R4
	MUL R4, R4, R6
	ADD R6, R5

	ADD $BPS, R0               // next row
	ADD $BPS, R1
	SUBS $1, R8
	BNE sse4x4_row

	MOVD R5, ret+48(FP)
	RET

// func sse16x16NEON(pix, ref []byte) int
// SSE for 16x16 block with BPS=32 stride. Scalar ARM64 implementation.
TEXT ·sse16x16NEON(SB), NOSPLIT, $0-56
	MOVD pix_base+0(FP), R0
	MOVD ref_base+24(FP), R1
	MOVD $0, R5                // sum = 0
	MOVD $16, R8               // row counter

sse16x16_row:
	MOVD $0, R9                // col counter

sse16x16_col:
	MOVBU (R0)(R9), R2
	MOVBU (R1)(R9), R3
	SUB R3, R2, R4
	MUL R4, R4, R6
	ADD R6, R5
	ADD $1, R9
	CMP $16, R9
	BLT sse16x16_col

	ADD $BPS, R0               // next row
	ADD $BPS, R1
	SUBS $1, R8
	BNE sse16x16_row

	MOVD R5, ret+48(FP)
	RET
