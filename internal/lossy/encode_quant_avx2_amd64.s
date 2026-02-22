#include "textflag.h"

// func quantizeACAVX2(in, out, sharpen []int16, iQuant, bias int)
// Quantizes 16 int16 coefficients using AVX2.
// Same algorithm as SSE2 but processes all 16 positions in a single pass:
//   out[n] = sign(in[n]) * min((abs(in[n]) + sharpen[n]) * iQuant + bias) >> 17, 2047)
// Caller must fix DC (position 0) separately.
//
// Uses VPMULUDQ for 32×32→64 widening multiply (raw VEX encoding since
// Go's assembler may not have VPMULUDQ).
//
// Register allocation:
//   Y15 = zero
//   Y14 = [2047 × 8] int32
//   Y13 = [iQuant × 8] uint32 (for VPMULUDQ, only even lanes used)
//   Y12 = [bias × 4] uint64
//   Y8  = signs (all 16 int16)
//   Y0-Y7, Y9-Y11 = temporaries
TEXT ·quantizeACAVX2(SB), NOSPLIT, $0-88
	MOVQ in_base+0(FP), SI
	MOVQ out_base+24(FP), DI
	MOVQ sharpen_base+48(FP), R8
	MOVQ iQuant+72(FP), AX
	MOVQ bias+80(FP), BX

	// Setup constants.
	VPXOR Y15, Y15, Y15            // Y15 = zero

	MOVQ AX, X13
	VPBROADCASTD X13, Y13          // Y13 = [iQuant × 8]

	MOVQ BX, X12
	VPBROADCASTQ X12, Y12          // Y12 = [bias × 4] as qwords

	MOVL $2047, AX
	MOVQ AX, X14
	VPBROADCASTD X14, Y14          // Y14 = [2047 × 8]

	// Load all 16 coefficients and sharpen values.
	VMOVDQU (SI), Y0               // in[0..15] as 16 int16
	VMOVDQU (R8), Y1               // sharpen[0..15]

	// Extract signs.
	VMOVDQA Y0, Y8
	VPSRAW  $15, Y8, Y8            // Y8 = sign mask (preserved)

	// Absolute value: |x| = (x ^ sign) - sign.
	VPXOR  Y8, Y0, Y0
	VPSUBW Y8, Y0, Y0

	// Add sharpen, clamp negative to 0.
	VPADDW  Y1, Y0, Y0
	VPMAXSW Y15, Y0, Y0            // Y0 = max(|in|+sharpen, 0)

	// Save for high-group processing.
	VMOVDQA Y0, Y9                  // Y9 = saved v[0..15]

	// === Process low group: widen to int32 ===
	// VPUNPCKLWL operates within each 128-bit lane:
	//   Lane 0: [v0d, v1d, v2d, v3d]  (from positions 0-3)
	//   Lane 1: [v8d, v9d, v10d, v11d] (from positions 8-11)
	// VPUNPCKLWD Y0, Y0, Y15 — VEX.256.66.0F 61 /r
	LONG $0x617DC1C4; BYTE $0xC7  // Y0 = [v0,v1,v2,v3 | v8,v9,v10,v11] as int32

	// VPMULUDQ: multiply even-indexed dwords (0,2,4,6) of Y0 with Y13.
	// Raw VEX encoding: VPMULUDQ Y13, Y0, Y1
	// VEX.256.66.0F.WIG F4 /r ; dst=Y1(reg=001), src1=Y0(vvvv=~0=1111), src2=Y13(rm=101, REX.B for ≥8)
	// Y13 index=13 → needs REX.B (B̃=0). VEX byte 1: R̃=1, X̃=1, B̃=0, mmmmm=00001 = 0b11000001 = 0xC1
	// VEX byte 2: W=0, vvvv=1111, L=1, pp=01 = 0b01111101 = 0x7D
	// ModRM: mod=11, reg=001(Y1), rm=101(Y13 low 3 bits) = 0b11001101 = 0xCD
	VMOVDQA Y0, Y1                  // save for odd-element multiply
	LONG $0xF47DC1C4; BYTE $0xCD   // VPMULUDQ Y13, Y0, Y1 → actually we need Y0 as src1
	// Let me redo: VPMULUDQ Y13, Y0, Y0 (dst=Y0, src1=Y0, src2=Y13)
	// VEX byte 1: R̃=1(Y0 reg≤7), X̃=1, B̃=0(Y13≥8), mmmmm=00001 = 0xC1
	// VEX byte 2: W=0, vvvv=~0=1111, L=1, pp=01 = 0x7D
	// ModRM: mod=11, reg=000(Y0), rm=101(Y13&7=5) = 0b11000101 = 0xC5
	// So: C4 C1 7D F4 C5

	// Actually, let me reconsider register allocation to avoid high registers in VPMULUDQ.
	// Move iQuant to Y6 (index 6, no REX needed).
	VMOVDQA Y13, Y6                // Y6 = [iQuant × 8]
	// bias stays in Y12, move to Y7.
	VMOVDQA Y12, Y7                // Y7 = [bias × 4] as qwords

	// Now Y0 = low group values as int32
	VMOVDQA Y0, Y1                 // Y1 = copy for odd elements

	// VPMULUDQ Y6, Y0, Y0: dst=Y0(000), src1=Y0(vvvv=~0=1111), src2=Y6(rm=110)
	// VEX: C4 E1 7D F4 C6
	LONG $0xF47DE1C4; BYTE $0xC6   // Y0 = VPMULUDQ(Y0, Y6) → even elements

	// Shuffle odd elements to even positions.
	VPSHUFD $0xF5, Y1, Y1          // [v1,v1,v3,v3 | v9,v9,v11,v11]

	// VPMULUDQ Y6, Y1, Y1: dst=Y1(001), src1=Y1(vvvv=~1=1110), src2=Y6(rm=110)
	// VEX byte 2: vvvv=1110 → 0b01110101 = 0x75
	// ModRM: reg=001, rm=110 = 0b11001110 = 0xCE
	// VEX: C4 E1 75 F4 CE
	LONG $0xF475E1C4; BYTE $0xCE   // Y1 = VPMULUDQ(Y1, Y6) → odd elements

	VPADDQ Y7, Y0, Y0              // + bias
	VPADDQ Y7, Y1, Y1

	VPSRLQ $17, Y0, Y0             // >> 17
	VPSRLQ $17, Y1, Y1

	// Merge: Y0=[c0,_,c2,_ | c8,_,c10,_], Y1=[c1,_,c3,_ | c9,_,c11,_]
	VPSHUFD $0xB1, Y1, Y1          // [_,c1,_,c3 | _,c9,_,c11]
	VPOR    Y1, Y0, Y0             // Y0 = [c0,c1,c2,c3 | c8,c9,c10,c11]

	// Clamp to 2047.
	VMOVDQA Y0, Y1
	VPCMPGTD Y14, Y1, Y1           // mask where > 2047
	VPAND   Y1, Y14, Y2            // 2047 where > 2047
	VPANDN  Y0, Y1, Y1             // coeff where ≤ 2047
	VPOR    Y2, Y1, Y1             // Y1 = clamped low group

	// === Process high group ===
	// VPUNPCKHWL: Lane 0 → [v4,v5,v6,v7], Lane 1 → [v12,v13,v14,v15]
	// VPUNPCKHWD Y0, Y9, Y15 — VEX.256.66.0F 69 /r
	LONG $0x6935C1C4; BYTE $0xC7  // Y0 = high group as int32

	VMOVDQA Y0, Y2                 // copy for odd elements

	// VPMULUDQ Y6, Y0, Y0
	LONG $0xF47DE1C4; BYTE $0xC6   // even elements

	VPSHUFD $0xF5, Y2, Y2
	// VPMULUDQ Y6, Y2, Y2: dst=Y2(010), src1=Y2(vvvv=~2=1101), src2=Y6(rm=110)
	// VEX byte 2: vvvv=1101 → 0b01101101 = 0x6D
	// ModRM: reg=010, rm=110 = 0b11010110 = 0xD6
	// VEX: C4 E1 6D F4 D6
	LONG $0xF46DE1C4; BYTE $0xD6   // odd elements

	VPADDQ Y7, Y0, Y0
	VPADDQ Y7, Y2, Y2

	VPSRLQ $17, Y0, Y0
	VPSRLQ $17, Y2, Y2

	VPSHUFD $0xB1, Y2, Y2
	VPOR    Y2, Y0, Y0             // Y0 = [c4,c5,c6,c7 | c12,c13,c14,c15]

	// Clamp to 2047.
	VMOVDQA Y0, Y2
	VPCMPGTD Y14, Y2, Y2
	VPAND   Y2, Y14, Y3
	VPANDN  Y0, Y2, Y2
	VPOR    Y3, Y2, Y2             // Y2 = clamped high group

	// Pack int32 → int16 with signed saturation.
	// VPACKSSDW Y2, Y1, Y1:
	//   Lane 0: pack(Y1_lo4=[c0..c3], Y2_lo4=[c4..c7]) = [c0h..c3h, c4h..c7h]
	//   Lane 1: pack(Y1_hi4=[c8..c11], Y2_hi4=[c12..c15]) = [c8h..c15h]
	VPACKSSDW Y2, Y1, Y1           // Y1 = [c0..c7 | c8..c15] as 16 int16

	// Apply signs: result = (coeff ^ sign) - sign.
	VPXOR  Y8, Y1, Y1
	VPSUBW Y8, Y1, Y1

	// Store all 16 coefficients.
	VMOVDQU Y1, (DI)

	VZEROUPPER
	RET
