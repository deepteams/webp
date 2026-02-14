#include "textflag.h"

// func fTransformWHTNEON(in []int16, out []int16)
// Forward WHT on flat 4x4 DC coefficients (stride 4).
// Uses scalar ARM64 instructions (butterfly has sequential dependencies).
TEXT ·fTransformWHTNEON(SB), NOSPLIT, $0-48
	MOVD in_base+0(FP), R0
	MOVD out_base+24(FP), R1

	// First pass: row-wise butterfly
	// Row 0
	MOVH (R0), R2              // in[0]
	MOVH 2(R0), R3             // in[1]
	MOVH 4(R0), R4             // in[2]
	MOVH 6(R0), R5             // in[3]
	// Sign-extend from 16-bit
	SBFX $0, R2, $16, R2
	SBFX $0, R3, $16, R3
	SBFX $0, R4, $16, R4
	SBFX $0, R5, $16, R5

	ADD R2, R4, R6             // a0 = in[0]+in[2]
	SUB R4, R2, R7             // a3 = in[0]-in[2]
	ADD R3, R5, R8             // a1 = in[1]+in[3]
	SUB R5, R3, R9             // a2 = in[1]-in[3]

	ADD R6, R8, R10            // tmp[0] = a0+a1
	ADD R7, R9, R11            // tmp[1] = a3+a2
	SUB R9, R7, R12            // tmp[2] = a3-a2
	SUB R8, R6, R13            // tmp[3] = a0-a1
	MOVH R10, (R1)
	MOVH R11, 2(R1)
	MOVH R12, 4(R1)
	MOVH R13, 6(R1)

	// Row 1
	MOVH 8(R0), R2
	MOVH 10(R0), R3
	MOVH 12(R0), R4
	MOVH 14(R0), R5
	SBFX $0, R2, $16, R2
	SBFX $0, R3, $16, R3
	SBFX $0, R4, $16, R4
	SBFX $0, R5, $16, R5
	ADD R2, R4, R6
	SUB R4, R2, R7
	ADD R3, R5, R8
	SUB R5, R3, R9
	ADD R6, R8, R10
	ADD R7, R9, R11
	SUB R9, R7, R12
	SUB R8, R6, R13
	MOVH R10, 8(R1)
	MOVH R11, 10(R1)
	MOVH R12, 12(R1)
	MOVH R13, 14(R1)

	// Row 2
	MOVH 16(R0), R2
	MOVH 18(R0), R3
	MOVH 20(R0), R4
	MOVH 22(R0), R5
	SBFX $0, R2, $16, R2
	SBFX $0, R3, $16, R3
	SBFX $0, R4, $16, R4
	SBFX $0, R5, $16, R5
	ADD R2, R4, R6
	SUB R4, R2, R7
	ADD R3, R5, R8
	SUB R5, R3, R9
	ADD R6, R8, R10
	ADD R7, R9, R11
	SUB R9, R7, R12
	SUB R8, R6, R13
	MOVH R10, 16(R1)
	MOVH R11, 18(R1)
	MOVH R12, 20(R1)
	MOVH R13, 22(R1)

	// Row 3
	MOVH 24(R0), R2
	MOVH 26(R0), R3
	MOVH 28(R0), R4
	MOVH 30(R0), R5
	SBFX $0, R2, $16, R2
	SBFX $0, R3, $16, R3
	SBFX $0, R4, $16, R4
	SBFX $0, R5, $16, R5
	ADD R2, R4, R6
	SUB R4, R2, R7
	ADD R3, R5, R8
	SUB R5, R3, R9
	ADD R6, R8, R10
	ADD R7, R9, R11
	SUB R9, R7, R12
	SUB R8, R6, R13
	MOVH R10, 24(R1)
	MOVH R11, 26(R1)
	MOVH R12, 28(R1)
	MOVH R13, 30(R1)

	// Second pass: column-wise butterfly, reading/writing DI
	// Column 0
	MOVH (R1), R2              // t[0]
	MOVH 16(R1), R3            // t[8]
	MOVH 8(R1), R4             // t[4]
	MOVH 24(R1), R5            // t[12]
	SBFX $0, R2, $16, R2
	SBFX $0, R3, $16, R3
	SBFX $0, R4, $16, R4
	SBFX $0, R5, $16, R5
	ADD R2, R3, R6             // a0
	SUB R3, R2, R7             // a3
	ADD R4, R5, R8             // a1
	SUB R5, R4, R9             // a2
	ADD R6, R8, R10
	ASR $1, R10                // >>1
	MOVH R10, (R1)
	ADD R7, R9, R10
	ASR $1, R10
	MOVH R10, 8(R1)
	SUB R9, R7, R10
	ASR $1, R10
	MOVH R10, 16(R1)
	SUB R8, R6, R10
	ASR $1, R10
	MOVH R10, 24(R1)

	// Column 1
	MOVH 2(R1), R2
	MOVH 18(R1), R3
	MOVH 10(R1), R4
	MOVH 26(R1), R5
	SBFX $0, R2, $16, R2
	SBFX $0, R3, $16, R3
	SBFX $0, R4, $16, R4
	SBFX $0, R5, $16, R5
	ADD R2, R3, R6
	SUB R3, R2, R7
	ADD R4, R5, R8
	SUB R5, R4, R9
	ADD R6, R8, R10
	ASR $1, R10
	MOVH R10, 2(R1)
	ADD R7, R9, R10
	ASR $1, R10
	MOVH R10, 10(R1)
	SUB R9, R7, R10
	ASR $1, R10
	MOVH R10, 18(R1)
	SUB R8, R6, R10
	ASR $1, R10
	MOVH R10, 26(R1)

	// Column 2
	MOVH 4(R1), R2
	MOVH 20(R1), R3
	MOVH 12(R1), R4
	MOVH 28(R1), R5
	SBFX $0, R2, $16, R2
	SBFX $0, R3, $16, R3
	SBFX $0, R4, $16, R4
	SBFX $0, R5, $16, R5
	ADD R2, R3, R6
	SUB R3, R2, R7
	ADD R4, R5, R8
	SUB R5, R4, R9
	ADD R6, R8, R10
	ASR $1, R10
	MOVH R10, 4(R1)
	ADD R7, R9, R10
	ASR $1, R10
	MOVH R10, 12(R1)
	SUB R9, R7, R10
	ASR $1, R10
	MOVH R10, 20(R1)
	SUB R8, R6, R10
	ASR $1, R10
	MOVH R10, 28(R1)

	// Column 3
	MOVH 6(R1), R2
	MOVH 22(R1), R3
	MOVH 14(R1), R4
	MOVH 30(R1), R5
	SBFX $0, R2, $16, R2
	SBFX $0, R3, $16, R3
	SBFX $0, R4, $16, R4
	SBFX $0, R5, $16, R5
	ADD R2, R3, R6
	SUB R3, R2, R7
	ADD R4, R5, R8
	SUB R5, R4, R9
	ADD R6, R8, R10
	ASR $1, R10
	MOVH R10, 6(R1)
	ADD R7, R9, R10
	ASR $1, R10
	MOVH R10, 14(R1)
	SUB R9, R7, R10
	ASR $1, R10
	MOVH R10, 22(R1)
	SUB R8, R6, R10
	ASR $1, R10
	MOVH R10, 30(R1)

	RET

// func transformWHTNEON(in []int16, out []int16)
// Inverse WHT. in: 16 coeffs, out: 16 DCs at stride-16 positions.
// Uses scalar ARM64 instructions (Go's ARM64 asm has limited NEON support).
TEXT ·transformWHTNEON(SB), NOSPLIT, $0-48
	MOVD in_base+0(FP), R0
	MOVD out_base+24(FP), R1

	// Vertical pass: for each column i=0..3
	// a0=in[i]+in[12+i], a1=in[4+i]+in[8+i], a2=in[4+i]-in[8+i], a3=in[i]-in[12+i]
	// tmp[i]=a0+a1, tmp[8+i]=a0-a1, tmp[4+i]=a3+a2, tmp[12+i]=a3-a2

	// Column 0: in[0], in[4], in[8], in[12]
	MOVH (R0), R2              // in[0]
	SBFX $0, R2, $16, R2
	MOVH 8(R0), R3             // in[4]
	SBFX $0, R3, $16, R3
	MOVH 16(R0), R4            // in[8]
	SBFX $0, R4, $16, R4
	MOVH 24(R0), R5            // in[12]
	SBFX $0, R5, $16, R5

	ADD R2, R5, R6             // a0 = in[0]+in[12]
	SUB R5, R2, R7             // a3 = in[0]-in[12]
	ADD R3, R4, R8             // a1 = in[4]+in[8]
	SUB R4, R3, R9             // a2 = in[4]-in[8]

	ADD R6, R8, R10            // tmp[0] = a0+a1
	SUB R8, R6, R11            // tmp[8] = a0-a1
	ADD R7, R9, R12            // tmp[4] = a3+a2
	SUB R9, R7, R13            // tmp[12] = a3-a2

	// Column 1: in[1], in[5], in[9], in[13]
	MOVH 2(R0), R2             // in[1]
	SBFX $0, R2, $16, R2
	MOVH 10(R0), R3            // in[5]
	SBFX $0, R3, $16, R3
	MOVH 18(R0), R4            // in[9]
	SBFX $0, R4, $16, R4
	MOVH 26(R0), R5            // in[13]
	SBFX $0, R5, $16, R5

	ADD R2, R5, R6
	SUB R5, R2, R7
	ADD R3, R4, R8
	SUB R4, R3, R9

	ADD R6, R8, R14            // tmp[1]
	SUB R8, R6, R15            // tmp[9]
	ADD R7, R9, R16            // tmp[5]
	SUB R9, R7, R17            // tmp[13]

	// Column 2: in[2], in[6], in[10], in[14]
	MOVH 4(R0), R2             // in[2]
	SBFX $0, R2, $16, R2
	MOVH 12(R0), R3            // in[6]
	SBFX $0, R3, $16, R3
	MOVH 20(R0), R4            // in[10]
	SBFX $0, R4, $16, R4
	MOVH 28(R0), R5            // in[14]
	SBFX $0, R5, $16, R5

	ADD R2, R5, R6
	SUB R5, R2, R7
	ADD R3, R4, R8
	SUB R4, R3, R9

	ADD R6, R8, R19            // tmp[2]
	SUB R8, R6, R20            // tmp[10]
	ADD R7, R9, R21            // tmp[6]
	SUB R9, R7, R22            // tmp[14]

	// Column 3: in[3], in[7], in[11], in[15]
	MOVH 6(R0), R2             // in[3]
	SBFX $0, R2, $16, R2
	MOVH 14(R0), R3            // in[7]
	SBFX $0, R3, $16, R3
	MOVH 22(R0), R4            // in[11]
	SBFX $0, R4, $16, R4
	MOVH 30(R0), R5            // in[15]
	SBFX $0, R5, $16, R5

	ADD R2, R5, R6
	SUB R5, R2, R7
	ADD R3, R4, R8
	SUB R4, R3, R9

	// tmp[3]=R6+R8, tmp[11]=R6-R8, tmp[7]=R7+R9, tmp[15]=R7-R9
	ADD R6, R8, R23            // tmp[3]
	SUB R8, R6, R24            // tmp[11]
	ADD R7, R9, R25            // tmp[7]
	SUB R9, R7, R26            // tmp[15]

	// Horizontal pass: Row 0 (tmp[0,1,2,3])
	// dc=tmp[0]+3, a0=dc+tmp[3], a1=tmp[1]+tmp[2], a2=tmp[1]-tmp[2], a3=dc-tmp[3]
	ADD $3, R10, R2            // dc = tmp[0]+3
	ADD R2, R23, R3            // a0 = dc + tmp[3]
	SUB R23, R2, R4            // a3 = dc - tmp[3]
	ADD R14, R19, R5           // a1 = tmp[1] + tmp[2]
	SUB R19, R14, R6           // a2 = tmp[1] - tmp[2]

	ADD R3, R5, R7
	ASR $3, R7
	MOVH R7, (R1)              // out[0*16]
	ADD R4, R6, R7
	ASR $3, R7
	MOVH R7, 32(R1)            // out[1*16]
	SUB R5, R3, R7
	ASR $3, R7
	MOVH R7, 64(R1)            // out[2*16]
	SUB R6, R4, R7
	ASR $3, R7
	MOVH R7, 96(R1)            // out[3*16]

	// Row 1 (tmp[4,5,6,7])
	ADD $3, R12, R2
	ADD R2, R25, R3
	SUB R25, R2, R4
	ADD R16, R21, R5
	SUB R21, R16, R6
	ADD R3, R5, R7
	ASR $3, R7
	MOVH R7, 128(R1)
	ADD R4, R6, R7
	ASR $3, R7
	MOVH R7, 160(R1)
	SUB R5, R3, R7
	ASR $3, R7
	MOVH R7, 192(R1)
	SUB R6, R4, R7
	ASR $3, R7
	MOVH R7, 224(R1)

	// Row 2 (tmp[8,9,10,11])
	ADD $3, R11, R2
	ADD R2, R24, R3
	SUB R24, R2, R4
	ADD R15, R20, R5
	SUB R20, R15, R6
	ADD R3, R5, R7
	ASR $3, R7
	MOVH R7, 256(R1)
	ADD R4, R6, R7
	ASR $3, R7
	MOVH R7, 288(R1)
	SUB R5, R3, R7
	ASR $3, R7
	MOVH R7, 320(R1)
	SUB R6, R4, R7
	ASR $3, R7
	MOVH R7, 352(R1)

	// Row 3 (tmp[12,13,14,15])
	ADD $3, R13, R2
	ADD R2, R26, R3
	SUB R26, R2, R4
	ADD R17, R22, R5
	SUB R22, R17, R6
	ADD R3, R5, R7
	ASR $3, R7
	MOVH R7, 384(R1)
	ADD R4, R6, R7
	ASR $3, R7
	MOVH R7, 416(R1)
	SUB R5, R3, R7
	ASR $3, R7
	MOVH R7, 448(R1)
	SUB R6, R4, R7
	ASR $3, R7
	MOVH R7, 480(R1)

	RET
