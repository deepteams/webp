#include "textflag.h"

// func fTransformWHTSSE2(in []int16, out []int16)
// Forward Walsh-Hadamard Transform on flat 4x4 DC coefficients (stride 4).
// in[i*4+c] -> butterfly -> out[i] with stride 4.
TEXT ·fTransformWHTSSE2(SB), NOSPLIT, $0-48
	MOVQ in_base+0(FP), SI
	MOVQ out_base+24(FP), DI

	// Load all 16 int16 values (32 bytes) into X0 and X1.
	MOVOU 0(SI), X0       // in[0..7]
	MOVOU 16(SI), X1      // in[8..15]

	// First pass: row-wise butterfly.
	// For each row i=0..3, values at in[i*4+0..3]:
	// a0=in[0]+in[2], a1=in[1]+in[3], a2=in[1]-in[3], a3=in[0]-in[2]
	// out: a0+a1, a3+a2, a3-a2, a0-a1

	// Process as scalar using GP registers (16 values, simple butterfly).
	// Row 0: in[0..3]
	MOVWQSX 0(SI), AX      // in[0]
	MOVWQSX 2(SI), BX      // in[1]
	MOVWQSX 4(SI), CX      // in[2]
	MOVWQSX 6(SI), DX      // in[3]

	MOVQ AX, R8
	ADDQ CX, R8            // a0 = in[0]+in[2]
	MOVQ AX, R9
	SUBQ CX, R9            // a3 = in[0]-in[2]
	MOVQ BX, R10
	ADDQ DX, R10           // a1 = in[1]+in[3]
	MOVQ BX, R11
	SUBQ DX, R11           // a2 = in[1]-in[3]

	// tmp[0..3] = a0+a1, a3+a2, a3-a2, a0-a1
	MOVQ R8, AX
	ADDQ R10, AX           // tmp[0] = a0+a1
	MOVW AX, 0(DI)
	MOVQ R9, AX
	ADDQ R11, AX           // tmp[1] = a3+a2
	MOVW AX, 2(DI)
	MOVQ R9, AX
	SUBQ R11, AX           // tmp[2] = a3-a2
	MOVW AX, 4(DI)
	MOVQ R8, AX
	SUBQ R10, AX           // tmp[3] = a0-a1
	MOVW AX, 6(DI)

	// Row 1: in[4..7]
	MOVWQSX 8(SI), AX
	MOVWQSX 10(SI), BX
	MOVWQSX 12(SI), CX
	MOVWQSX 14(SI), DX
	MOVQ AX, R8
	ADDQ CX, R8
	MOVQ AX, R9
	SUBQ CX, R9
	MOVQ BX, R10
	ADDQ DX, R10
	MOVQ BX, R11
	SUBQ DX, R11
	MOVQ R8, AX
	ADDQ R10, AX
	MOVW AX, 8(DI)
	MOVQ R9, AX
	ADDQ R11, AX
	MOVW AX, 10(DI)
	MOVQ R9, AX
	SUBQ R11, AX
	MOVW AX, 12(DI)
	MOVQ R8, AX
	SUBQ R10, AX
	MOVW AX, 14(DI)

	// Row 2: in[8..11]
	MOVWQSX 16(SI), AX
	MOVWQSX 18(SI), BX
	MOVWQSX 20(SI), CX
	MOVWQSX 22(SI), DX
	MOVQ AX, R8
	ADDQ CX, R8
	MOVQ AX, R9
	SUBQ CX, R9
	MOVQ BX, R10
	ADDQ DX, R10
	MOVQ BX, R11
	SUBQ DX, R11
	MOVQ R8, AX
	ADDQ R10, AX
	MOVW AX, 16(DI)
	MOVQ R9, AX
	ADDQ R11, AX
	MOVW AX, 18(DI)
	MOVQ R9, AX
	SUBQ R11, AX
	MOVW AX, 20(DI)
	MOVQ R8, AX
	SUBQ R10, AX
	MOVW AX, 22(DI)

	// Row 3: in[12..15]
	MOVWQSX 24(SI), AX
	MOVWQSX 26(SI), BX
	MOVWQSX 28(SI), CX
	MOVWQSX 30(SI), DX
	MOVQ AX, R8
	ADDQ CX, R8
	MOVQ AX, R9
	SUBQ CX, R9
	MOVQ BX, R10
	ADDQ DX, R10
	MOVQ BX, R11
	SUBQ DX, R11
	MOVQ R8, AX
	ADDQ R10, AX
	MOVW AX, 24(DI)
	MOVQ R9, AX
	ADDQ R11, AX
	MOVW AX, 26(DI)
	MOVQ R9, AX
	SUBQ R11, AX
	MOVW AX, 28(DI)
	MOVQ R8, AX
	SUBQ R10, AX
	MOVW AX, 30(DI)

	// Second pass: column-wise butterfly, reading/writing from DI (tmp/out).
	// For column i: a0=t[i]+t[8+i], a1=t[4+i]+t[12+i], a2=t[4+i]-t[12+i], a3=t[i]-t[8+i]
	// b0=a0+a1, b1=a3+a2, b2=a3-a2, b3=a0-a1
	// out[i]=b0>>1, out[4+i]=b1>>1, out[8+i]=b2>>1, out[12+i]=b3>>1

	// Column 0
	MOVWQSX 0(DI), AX      // t[0]
	MOVWQSX 16(DI), BX     // t[8]
	MOVQ AX, R8
	ADDQ BX, R8            // a0
	MOVQ AX, R9
	SUBQ BX, R9            // a3
	MOVWQSX 8(DI), AX      // t[4]
	MOVWQSX 24(DI), BX     // t[12]
	MOVQ AX, R10
	ADDQ BX, R10           // a1
	MOVQ AX, R11
	SUBQ BX, R11           // a2
	MOVQ R8, AX
	ADDQ R10, AX
	SARQ $1, AX
	MOVW AX, 0(DI)         // out[0]
	MOVQ R9, AX
	ADDQ R11, AX
	SARQ $1, AX
	MOVW AX, 8(DI)         // out[4]
	MOVQ R9, AX
	SUBQ R11, AX
	SARQ $1, AX
	MOVW AX, 16(DI)        // out[8]
	MOVQ R8, AX
	SUBQ R10, AX
	SARQ $1, AX
	MOVW AX, 24(DI)        // out[12]

	// Column 1
	MOVWQSX 2(DI), AX
	MOVWQSX 18(DI), BX
	MOVQ AX, R8
	ADDQ BX, R8
	MOVQ AX, R9
	SUBQ BX, R9
	MOVWQSX 10(DI), AX
	MOVWQSX 26(DI), BX
	MOVQ AX, R10
	ADDQ BX, R10
	MOVQ AX, R11
	SUBQ BX, R11
	MOVQ R8, AX
	ADDQ R10, AX
	SARQ $1, AX
	MOVW AX, 2(DI)
	MOVQ R9, AX
	ADDQ R11, AX
	SARQ $1, AX
	MOVW AX, 10(DI)
	MOVQ R9, AX
	SUBQ R11, AX
	SARQ $1, AX
	MOVW AX, 18(DI)
	MOVQ R8, AX
	SUBQ R10, AX
	SARQ $1, AX
	MOVW AX, 26(DI)

	// Column 2
	MOVWQSX 4(DI), AX
	MOVWQSX 20(DI), BX
	MOVQ AX, R8
	ADDQ BX, R8
	MOVQ AX, R9
	SUBQ BX, R9
	MOVWQSX 12(DI), AX
	MOVWQSX 28(DI), BX
	MOVQ AX, R10
	ADDQ BX, R10
	MOVQ AX, R11
	SUBQ BX, R11
	MOVQ R8, AX
	ADDQ R10, AX
	SARQ $1, AX
	MOVW AX, 4(DI)
	MOVQ R9, AX
	ADDQ R11, AX
	SARQ $1, AX
	MOVW AX, 12(DI)
	MOVQ R9, AX
	SUBQ R11, AX
	SARQ $1, AX
	MOVW AX, 20(DI)
	MOVQ R8, AX
	SUBQ R10, AX
	SARQ $1, AX
	MOVW AX, 28(DI)

	// Column 3
	MOVWQSX 6(DI), AX
	MOVWQSX 22(DI), BX
	MOVQ AX, R8
	ADDQ BX, R8
	MOVQ AX, R9
	SUBQ BX, R9
	MOVWQSX 14(DI), AX
	MOVWQSX 30(DI), BX
	MOVQ AX, R10
	ADDQ BX, R10
	MOVQ AX, R11
	SUBQ BX, R11
	MOVQ R8, AX
	ADDQ R10, AX
	SARQ $1, AX
	MOVW AX, 6(DI)
	MOVQ R9, AX
	ADDQ R11, AX
	SARQ $1, AX
	MOVW AX, 14(DI)
	MOVQ R9, AX
	SUBQ R11, AX
	SARQ $1, AX
	MOVW AX, 22(DI)
	MOVQ R8, AX
	SUBQ R10, AX
	SARQ $1, AX
	MOVW AX, 30(DI)

	RET

// func transformWHTSSE2(in []int16, out []int16)
// Inverse WHT. in: 16 int16 coeffs. out: 16 DC values at stride-16 positions.
// out[base+0*16], out[base+1*16], etc. where base = row*4*16.
TEXT ·transformWHTSSE2(SB), NOSPLIT, $64-48
	MOVQ in_base+0(FP), SI
	MOVQ out_base+24(FP), DI

	// Vertical pass: for each column i=0..3
	// a0=in[i]+in[12+i], a1=in[4+i]+in[8+i], a2=in[4+i]-in[8+i], a3=in[i]-in[12+i]
	// tmp[i]=a0+a1, tmp[8+i]=a0-a1, tmp[4+i]=a3+a2, tmp[12+i]=a3-a2
	// Store tmp as 16 int32 values on stack (4 per column, 4 columns).

	// Column 0
	MOVWQSX 0(SI), AX      // in[0]
	MOVWQSX 24(SI), BX     // in[12]
	MOVQ AX, R8
	ADDQ BX, R8            // a0
	MOVQ AX, R9
	SUBQ BX, R9            // a3
	MOVWQSX 8(SI), AX      // in[4]
	MOVWQSX 16(SI), BX     // in[8]
	MOVQ AX, R10
	ADDQ BX, R10           // a1
	MOVQ AX, R11
	SUBQ BX, R11           // a2
	// tmp[0]=a0+a1
	MOVQ R8, AX
	ADDQ R10, AX
	MOVQ AX, 0(SP)
	// tmp[8]=a0-a1
	MOVQ R8, AX
	SUBQ R10, AX
	MOVQ AX, 16(SP)
	// tmp[4]=a3+a2
	MOVQ R9, AX
	ADDQ R11, AX
	MOVQ AX, 8(SP)
	// tmp[12]=a3-a2
	MOVQ R9, AX
	SUBQ R11, AX
	MOVQ AX, 24(SP)

	// Column 1
	MOVWQSX 2(SI), AX
	MOVWQSX 26(SI), BX
	MOVQ AX, R8
	ADDQ BX, R8
	MOVQ AX, R9
	SUBQ BX, R9
	MOVWQSX 10(SI), AX
	MOVWQSX 18(SI), BX
	MOVQ AX, R10
	ADDQ BX, R10
	MOVQ AX, R11
	SUBQ BX, R11
	MOVQ R8, AX
	ADDQ R10, AX
	MOVQ AX, 32(SP)        // tmp[1]
	MOVQ R8, AX
	SUBQ R10, AX
	MOVQ AX, 48(SP)        // tmp[9]
	MOVQ R9, AX
	ADDQ R11, AX
	MOVQ AX, 40(SP)        // tmp[5]
	MOVQ R9, AX
	SUBQ R11, AX
	MOVQ AX, 56(SP)        // tmp[13]

	// Column 2 - store in R12-R15 for horizontal pass
	MOVWQSX 4(SI), AX
	MOVWQSX 28(SI), BX
	MOVQ AX, R8
	ADDQ BX, R8
	MOVQ AX, R9
	SUBQ BX, R9
	MOVWQSX 12(SI), AX
	MOVWQSX 20(SI), BX
	MOVQ AX, R10
	ADDQ BX, R10
	MOVQ AX, R11
	SUBQ BX, R11
	MOVQ R8, R12
	ADDQ R10, R12           // tmp[2]
	MOVQ R8, R13
	SUBQ R10, R13           // tmp[10]
	MOVQ R9, R14
	ADDQ R11, R14           // tmp[6]
	MOVQ R9, R15
	SUBQ R11, R15           // tmp[14]

	// Column 3 - keep in R8-R11
	MOVWQSX 6(SI), AX
	MOVWQSX 30(SI), BX
	MOVQ AX, R8
	ADDQ BX, R8
	MOVQ AX, R9
	SUBQ BX, R9
	MOVWQSX 14(SI), AX
	MOVWQSX 22(SI), BX
	MOVQ AX, R10
	ADDQ BX, R10
	MOVQ AX, R11
	SUBQ BX, R11
	// tmp[3]=R8+R10, tmp[11]=R8-R10, tmp[7]=R9+R11, tmp[15]=R9-R11

	// Horizontal pass: 4 rows. Each row writes 4 DCs at stride 16 (32 bytes).
	// Row i: dc=tmp[i*4+0]+3, a0=dc+tmp[i*4+3], a1=tmp[i*4+1]+tmp[i*4+2],
	//        a2=tmp[i*4+1]-tmp[i*4+2], a3=dc-tmp[i*4+3]
	// out[base+k*16] = (val)>>3, base=i*4*16=i*64 words=i*128 bytes

	// Row 0: tmp[0,1,2,3]
	MOVQ 0(SP), AX          // tmp[0]
	ADDQ $3, AX             // dc
	MOVQ R8, CX
	ADDQ R10, CX            // tmp[3]
	MOVQ AX, DX
	ADDQ CX, DX             // a0 = dc + tmp[3]
	MOVQ AX, SI             // a3 = dc - tmp[3] (SI done)
	SUBQ CX, SI
	MOVQ 32(SP), BX         // tmp[1]
	MOVQ BX, CX
	ADDQ R12, CX            // a1 = tmp[1] + tmp[2]
	SUBQ R12, BX            // a2 = tmp[1] - tmp[2]
	// out[0]
	MOVQ DX, AX
	ADDQ CX, AX
	SARQ $3, AX
	MOVW AX, 0(DI)
	// out[16] = out + 32 bytes
	MOVQ SI, AX
	ADDQ BX, AX
	SARQ $3, AX
	MOVW AX, 32(DI)
	// out[32] = out + 64 bytes
	MOVQ DX, AX
	SUBQ CX, AX
	SARQ $3, AX
	MOVW AX, 64(DI)
	// out[48] = out + 96 bytes
	MOVQ SI, AX
	SUBQ BX, AX
	SARQ $3, AX
	MOVW AX, 96(DI)

	// Row 1: tmp[4,5,6,7]
	MOVQ 8(SP), AX          // tmp[4]
	ADDQ $3, AX
	MOVQ R9, CX
	ADDQ R11, CX            // tmp[7]
	MOVQ AX, DX
	ADDQ CX, DX             // a0
	MOVQ AX, SI
	SUBQ CX, SI             // a3
	MOVQ 40(SP), BX         // tmp[5]
	MOVQ BX, CX
	ADDQ R14, CX            // a1 = tmp[5] + tmp[6]
	SUBQ R14, BX            // a2
	MOVQ DX, AX
	ADDQ CX, AX
	SARQ $3, AX
	MOVW AX, 128(DI)        // out[4*16] = row1, col0
	MOVQ SI, AX
	ADDQ BX, AX
	SARQ $3, AX
	MOVW AX, 160(DI)        // out[5*16]
	MOVQ DX, AX
	SUBQ CX, AX
	SARQ $3, AX
	MOVW AX, 192(DI)        // out[6*16]
	MOVQ SI, AX
	SUBQ BX, AX
	SARQ $3, AX
	MOVW AX, 224(DI)        // out[7*16]

	// Row 2: tmp[8,9,10,11]
	MOVQ 16(SP), AX         // tmp[8]
	ADDQ $3, AX
	MOVQ R8, CX
	SUBQ R10, CX            // tmp[11] = col3_a0-a1
	MOVQ AX, DX
	ADDQ CX, DX
	MOVQ AX, SI
	SUBQ CX, SI
	MOVQ 48(SP), BX         // tmp[9]
	MOVQ BX, CX
	ADDQ R13, CX            // a1 = tmp[9] + tmp[10]
	SUBQ R13, BX            // a2
	MOVQ DX, AX
	ADDQ CX, AX
	SARQ $3, AX
	MOVW AX, 256(DI)        // out[8*16]
	MOVQ SI, AX
	ADDQ BX, AX
	SARQ $3, AX
	MOVW AX, 288(DI)
	MOVQ DX, AX
	SUBQ CX, AX
	SARQ $3, AX
	MOVW AX, 320(DI)
	MOVQ SI, AX
	SUBQ BX, AX
	SARQ $3, AX
	MOVW AX, 352(DI)

	// Row 3: tmp[12,13,14,15]
	MOVQ 24(SP), AX         // tmp[12]
	ADDQ $3, AX
	MOVQ R9, CX
	SUBQ R11, CX            // tmp[15] = col3_a3-a2
	MOVQ AX, DX
	ADDQ CX, DX
	MOVQ AX, SI
	SUBQ CX, SI
	MOVQ 56(SP), BX         // tmp[13]
	MOVQ BX, CX
	ADDQ R15, CX            // a1 = tmp[13] + tmp[14]
	SUBQ R15, BX            // a2
	MOVQ DX, AX
	ADDQ CX, AX
	SARQ $3, AX
	MOVW AX, 384(DI)        // out[12*16]
	MOVQ SI, AX
	ADDQ BX, AX
	SARQ $3, AX
	MOVW AX, 416(DI)
	MOVQ DX, AX
	SUBQ CX, AX
	SARQ $3, AX
	MOVW AX, 448(DI)
	MOVQ SI, AX
	SUBQ BX, AX
	SARQ $3, AX
	MOVW AX, 480(DI)

	RET
