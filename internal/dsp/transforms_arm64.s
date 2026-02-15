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

// func iTransformOneNEON(ref []byte, in []int16, dst []byte)
// NEON inverse DCT 4x4. c1=20091, c2=35468. ref/dst stride=BPS=32.
// Uses in[] as scratch after loading coefficients.
TEXT ·iTransformOneNEON(SB), NOSPLIT, $0-72
	MOVD ref_base+0(FP), R0
	MOVD in_base+24(FP), R1
	MOVD dst_base+48(FP), R2

	// Constants
	MOVW $20091, R3
	VDUP R3, V28.S4
	MOVW $35468, R3
	VDUP R3, V29.S4
	MOVW $4, R3
	VDUP R3, V30.S4

	// Load 16 int16 coefficients (32 bytes)
	VLD1 (R1), [V16.B16, V17.B16]

	// Widen int16 → int32 (4 rows)
	WORD $0x0f10a600    // SXTL  V0.4S, V16.4H  (row0)
	WORD $0x4f10a601    // SXTL2 V1.4S, V16.8H  (row1)
	WORD $0x0f10a622    // SXTL  V2.4S, V17.4H  (row2)
	WORD $0x4f10a623    // SXTL2 V3.4S, V17.8H  (row3)

	// Vertical butterfly
	VADD V2.S4, V0.S4, V4.S4     // a = row0+row2
	VSUB V2.S4, V0.S4, V5.S4     // b = row0-row2
	// Launch 4 VMULs for pipelining
	WORD $0x4ebc9c26              // V6  = V1*20091
	WORD $0x4ebd9c27              // V7  = V1*35468
	WORD $0x4ebc9c70              // V16 = V3*20091
	WORD $0x4ebd9c71              // V17 = V3*35468
	WORD $0x4f3004c6              // SSHR V6, #16
	WORD $0x4f3004e7              // SSHR V7, #16  → mul2(row1)
	WORD $0x4f300610              // SSHR V16, #16
	WORD $0x4f300631              // SSHR V17, #16 → mul2(row3)
	VADD V1.S4, V6.S4, V6.S4     // mul1(row1)
	VADD V3.S4, V16.S4, V16.S4   // mul1(row3)
	VSUB V16.S4, V7.S4, V16.S4   // cc = mul2(row1)-mul1(row3)
	VADD V17.S4, V6.S4, V17.S4   // d  = mul1(row1)+mul2(row3)
	VADD V17.S4, V4.S4, V0.S4    // tmp0 = a+d
	VADD V16.S4, V5.S4, V1.S4    // tmp1 = b+cc
	VSUB V16.S4, V5.S4, V2.S4    // tmp2 = b-cc
	VSUB V17.S4, V4.S4, V3.S4    // tmp3 = a-d

	// Transpose 1 (rows → columns)
	WORD $0x4e812804    // TRN1 V4, V0, V1
	WORD $0x4e816805    // TRN2 V5, V0, V1
	WORD $0x4e832846    // TRN1 V6, V2, V3
	WORD $0x4e836847    // TRN2 V7, V2, V3
	WORD $0x4ec63890    // ZIP1 V16←V4,V6 (col0)
	WORD $0x4ec67892    // ZIP2 V18←V4,V6 (col2)
	WORD $0x4ec738b1    // ZIP1 V17←V5,V7 (col1)
	WORD $0x4ec778b3    // ZIP2 V19←V5,V7 (col3)

	// Horizontal butterfly
	VADD V30.S4, V16.S4, V16.S4  // dc = col0+4
	VADD V18.S4, V16.S4, V4.S4   // a = dc+col2
	VSUB V18.S4, V16.S4, V5.S4   // b = dc-col2
	WORD $0x4ebc9e26              // V6  = V17*20091
	WORD $0x4ebd9e27              // V7  = V17*35468
	WORD $0x4ebc9e74              // V20 = V19*20091
	WORD $0x4ebd9e75              // V21 = V19*35468
	WORD $0x4f3004c6              // SSHR V6, #16
	WORD $0x4f3004e7              // SSHR V7, #16  → mul2(col1)
	WORD $0x4f300694              // SSHR V20, #16
	WORD $0x4f3006b5              // SSHR V21, #16 → mul2(col3)
	VADD V17.S4, V6.S4, V6.S4    // mul1(col1)
	VADD V19.S4, V20.S4, V20.S4  // mul1(col3)
	VSUB V20.S4, V7.S4, V20.S4   // cc
	VADD V21.S4, V6.S4, V21.S4   // d
	VADD V21.S4, V4.S4, V0.S4    // a+d
	VADD V20.S4, V5.S4, V1.S4    // b+cc
	VSUB V20.S4, V5.S4, V2.S4    // b-cc
	VSUB V21.S4, V4.S4, V3.S4    // a-d
	WORD $0x4f3d0400              // SSHR V0, #3
	WORD $0x4f3d0421              // SSHR V1, #3
	WORD $0x4f3d0442              // SSHR V2, #3
	WORD $0x4f3d0463              // SSHR V3, #3

	// Transpose 2 (columns → rows)
	WORD $0x4e812804    // TRN1 V4, V0, V1
	WORD $0x4e816805    // TRN2 V5, V0, V1
	WORD $0x4e832846    // TRN1 V6, V2, V3
	WORD $0x4e836847    // TRN2 V7, V2, V3
	WORD $0x4ec63890    // ZIP1 V16←V4,V6 (row0)
	WORD $0x4ec67892    // ZIP2 V18←V4,V6 (row2)
	WORD $0x4ec738b1    // ZIP1 V17←V5,V7 (row1)
	WORD $0x4ec778b3    // ZIP2 V19←V5,V7 (row3)

	// Load ref (stride 32), pack into in[] scratch, widen
	MOVWU (R0), R3
	MOVW R3, (R1)
	MOVWU 32(R0), R3
	MOVW R3, 4(R1)
	MOVWU 64(R0), R3
	MOVW R3, 8(R1)
	MOVWU 96(R0), R3
	MOVW R3, 12(R1)
	VLD1 (R1), [V24.B16]
	WORD $0x2f08a719    // UXTL  V25.8H, V24.8B
	WORD $0x6f08a71a    // UXTL2 V26.8H, V24.16B
	WORD $0x2f10a734    // UXTL  V20.4S, V25.4H  (ref row0)
	WORD $0x6f10a735    // UXTL2 V21.4S, V25.8H  (ref row1)
	WORD $0x2f10a756    // UXTL  V22.4S, V26.4H  (ref row2)
	WORD $0x6f10a757    // UXTL2 V23.4S, V26.8H  (ref row3)
	VADD V20.S4, V16.S4, V0.S4
	VADD V21.S4, V17.S4, V1.S4
	VADD V22.S4, V18.S4, V2.S4
	VADD V23.S4, V19.S4, V3.S4
	// Clip: SQXTN(int32→int16) + SQXTUN(int16→uint8)
	WORD $0x0e614804    // SQXTN  V4.4H, V0.4S
	WORD $0x4e614824    // SQXTN2 V4.8H, V1.4S
	WORD $0x0e614845    // SQXTN  V5.4H, V2.4S
	WORD $0x4e614865    // SQXTN2 V5.8H, V3.4S
	WORD $0x2e212886    // SQXTUN  V6.8B, V4.8H
	WORD $0x6e2128a6    // SQXTUN2 V6.16B, V5.8H

	// Store result (stride 32) via scratch
	VST1 [V6.B16], (R1)
	MOVWU (R1), R3
	MOVW R3, (R2)
	MOVWU 4(R1), R3
	MOVW R3, 32(R2)
	MOVWU 8(R1), R3
	MOVW R3, 64(R2)
	MOVWU 12(R1), R3
	MOVW R3, 96(R2)
	RET

// func fTransformNEON(src, ref []byte, out []int16)
// NEON forward DCT 4x4. src/ref stride=BPS=32.
// Uses MOVWU+INS for direct strided load (no scratch buffer).
// Uses MLA/MLS for fused multiply-accumulate.
TEXT ·fTransformNEON(SB), NOSPLIT, $0-72
	MOVD src_base+0(FP), R0
	MOVD ref_base+24(FP), R1
	MOVD out_base+48(FP), R2

	// Constants for horizontal pass
	MOVW $2217, R3
	VDUP R3, V28.S4
	MOVW $5352, R3
	VDUP R3, V29.S4
	MOVW $1812, R3
	VDUP R3, V26.S4
	MOVW $937, R3
	VDUP R3, V25.S4

	// Load src (stride 32) into V0 via MOVWU + INS (no scratch buffer)
	MOVWU (R0), R5
	MOVWU 32(R0), R6
	MOVWU 64(R0), R7
	MOVWU 96(R0), R8
	WORD $0x4E041CA0    // INS V0.S[0], W5
	WORD $0x4E0C1CC0    // INS V0.S[1], W6
	WORD $0x4E141CE0    // INS V0.S[2], W7
	WORD $0x4E1C1D00    // INS V0.S[3], W8

	// Load ref (stride 32) into V1 via MOVWU + INS
	MOVWU (R1), R5
	MOVWU 32(R1), R6
	MOVWU 64(R1), R7
	MOVWU 96(R1), R8
	WORD $0x4E041CA1    // INS V1.S[0], W5
	WORD $0x4E0C1CC1    // INS V1.S[1], W6
	WORD $0x4E141CE1    // INS V1.S[2], W7
	WORD $0x4E1C1D01    // INS V1.S[3], W8

	// Widen uint8→uint16
	WORD $0x2f08a402    // UXTL  V2.8H, V0.8B
	WORD $0x6f08a403    // UXTL2 V3.8H, V0.16B
	WORD $0x2f08a424    // UXTL  V4.8H, V1.8B
	WORD $0x6f08a425    // UXTL2 V5.8H, V1.16B
	// diff = src - ref (int16)
	VSUB V4.H8, V2.H8, V0.H8
	VSUB V5.H8, V3.H8, V1.H8

	// Widen diff int16→int32
	WORD $0x0f10a410    // SXTL  V16.4S, V0.4H
	WORD $0x4f10a411    // SXTL2 V17.4S, V0.8H
	WORD $0x0f10a432    // SXTL  V18.4S, V1.4H
	WORD $0x4f10a433    // SXTL2 V19.4S, V1.8H

	// Transpose 1 (rows → column vectors)
	WORD $0x4e912a04    // TRN1 V4, V16, V17
	WORD $0x4e916a05    // TRN2 V5, V16, V17
	WORD $0x4e932a46    // TRN1 V6, V18, V19
	WORD $0x4e936a47    // TRN2 V7, V18, V19
	WORD $0x4ec63880    // ZIP1 V0←V4,V6 (d0)
	WORD $0x4ec67882    // ZIP2 V2←V4,V6 (d2)
	WORD $0x4ec738a1    // ZIP1 V1←V5,V7 (d1)
	WORD $0x4ec778a3    // ZIP2 V3←V5,V7 (d3)

	// Horizontal butterfly
	VADD V3.S4, V0.S4, V4.S4     // a0 = d0+d3
	VADD V2.S4, V1.S4, V5.S4     // a1 = d1+d2
	VSUB V2.S4, V1.S4, V6.S4     // a2 = d1-d2
	VSUB V3.S4, V0.S4, V7.S4     // a3 = d0-d3
	VADD V5.S4, V4.S4, V20.S4    // a0+a1
	WORD $0x4f235694               // SHL V20, #3  → (a0+a1)*8
	VSUB V5.S4, V4.S4, V22.S4    // a0-a1
	WORD $0x4f2356d6               // SHL V22, #3  → (a0-a1)*8
	// (a2*2217+a3*5352+1812)>>9  — MUL+MLA fused
	WORD $0x4ebc9cd5               // MUL V21.4S, V6.4S, V28.4S   (a2*2217)
	WORD $0x4ebd94f5               // MLA V21.4S, V7.4S, V29.4S   (+a3*5352)
	VADD V26.S4, V21.S4, V21.S4
	WORD $0x4f3706b5               // SSHR V21, #9
	// (a3*2217-a2*5352+937)>>9  — MUL+MLS fused
	WORD $0x4ebc9cf7               // MUL V23.4S, V7.4S, V28.4S   (a3*2217)
	WORD $0x6ebd94d7               // MLS V23.4S, V6.4S, V29.4S   (-a2*5352)
	VADD V25.S4, V23.S4, V23.S4
	WORD $0x4f3706f7               // SSHR V23, #9

	// Transpose 2 (columns → rows)
	WORD $0x4e952a84    // TRN1 V4, V20, V21
	WORD $0x4e956a85    // TRN2 V5, V20, V21
	WORD $0x4e972ac6    // TRN1 V6, V22, V23
	WORD $0x4e976ac7    // TRN2 V7, V22, V23
	WORD $0x4ec63880    // ZIP1 V0←V4,V6 (row0)
	WORD $0x4ec67882    // ZIP2 V2←V4,V6 (row2)
	WORD $0x4ec738a1    // ZIP1 V1←V5,V7 (row1)
	WORD $0x4ec778a3    // ZIP2 V3←V5,V7 (row3)

	// Vertical pass constants
	MOVW $7, R3
	VDUP R3, V30.S4
	MOVW $12000, R3
	VDUP R3, V31.S4
	MOVW $51000, R3
	VDUP R3, V27.S4

	// Vertical butterfly
	VADD V3.S4, V0.S4, V4.S4     // a0
	VADD V2.S4, V1.S4, V5.S4     // a1
	VSUB V2.S4, V1.S4, V6.S4     // a2
	VSUB V3.S4, V0.S4, V7.S4     // a3
	// out_row0 = (a0+a1+7)>>4
	VADD V5.S4, V4.S4, V16.S4
	VADD V30.S4, V16.S4, V16.S4
	WORD $0x4f3c0610               // SSHR V16, #4
	// out_row2 = (a0-a1+7)>>4
	VSUB V5.S4, V4.S4, V18.S4
	VADD V30.S4, V18.S4, V18.S4
	WORD $0x4f3c0652               // SSHR V18, #4
	// out_row1 = (a2*2217+a3*5352+12000)>>16 + b2i(a3!=0) — MUL+MLA
	WORD $0x4ebc9cd1               // MUL V17.4S, V6.4S, V28.4S
	WORD $0x4ebd94f1               // MLA V17.4S, V7.4S, V29.4S
	VADD V31.S4, V17.S4, V17.S4
	WORD $0x4f300631               // SSHR V17, #16
	WORD $0x4ea098f4               // CMEQ V20, V7, #0
	WORD $0x6e205a94               // MVN  V20, V20
	VUSHR $31, V20.S4, V20.S4
	VADD V20.S4, V17.S4, V17.S4
	// out_row3 = (a3*2217-a2*5352+51000)>>16 — MUL+MLS
	WORD $0x4ebc9cf3               // MUL V19.4S, V7.4S, V28.4S
	WORD $0x6ebd94d3               // MLS V19.4S, V6.4S, V29.4S
	VADD V27.S4, V19.S4, V19.S4
	WORD $0x4f300673               // SSHR V19, #16

	// Narrow int32→int16 (truncating) and store
	WORD $0x0e612a00    // XTN  V0.4H, V16.4S
	WORD $0x4e612a20    // XTN2 V0.8H, V17.4S
	WORD $0x0e612a41    // XTN  V1.4H, V18.4S
	WORD $0x4e612a61    // XTN2 V1.8H, V19.4S
	VST1 [V0.B16, V1.B16], (R2)
	RET
