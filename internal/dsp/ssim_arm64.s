#include "textflag.h"

#define BPS 32

// func sse4x4NEON(pix, ref []byte) int
// SSE for 4x4 block with BPS=32 stride. NEON SIMD implementation.
// Uses INS to transfer strided bytes from GPR to NEON, then
// widening subtract, multiply, and pairwise-add reduction.
TEXT ·sse4x4NEON(SB), NOSPLIT, $0-56
	MOVD pix_base+0(FP), R0    // pix
	MOVD ref_base+24(FP), R1   // ref

	// Pack src 4x4 bytes into V0.B16 via INS Vd.S[idx], Wn
	MOVWU (R0), R2
	WORD $0x4e041c40            // INS V0.S[0], W2
	MOVWU 32(R0), R2
	WORD $0x4e0c1c40            // INS V0.S[1], W2
	MOVWU 64(R0), R2
	WORD $0x4e141c40            // INS V0.S[2], W2
	MOVWU 96(R0), R2
	WORD $0x4e1c1c40            // INS V0.S[3], W2

	// Pack ref 4x4 bytes into V1.B16 via INS
	MOVWU (R1), R2
	WORD $0x4e041c41            // INS V1.S[0], W2
	MOVWU 32(R1), R2
	WORD $0x4e0c1c41            // INS V1.S[1], W2
	MOVWU 64(R1), R2
	WORD $0x4e141c41            // INS V1.S[2], W2
	MOVWU 96(R1), R2
	WORD $0x4e1c1c41            // INS V1.S[3], W2

	// Widen src uint8→uint16: V2 = low 8, V3 = high 8
	WORD $0x2f08a402            // UXTL  V2.8H, V0.8B
	WORD $0x6f08a403            // UXTL2 V3.8H, V0.16B

	// Widen ref uint8→uint16: V4 = low 8, V5 = high 8
	WORD $0x2f08a424            // UXTL  V4.8H, V1.8B
	WORD $0x6f08a425            // UXTL2 V5.8H, V1.16B

	// Signed diff: src - ref (int16)
	VSUB V4.H8, V2.H8, V2.H8
	VSUB V5.H8, V3.H8, V3.H8

	// Square diffs: SMULL/SMULL2 (int16→int32)
	WORD $0x0e62c044            // SMULL  V4.4S, V2.4H, V2.4H
	WORD $0x4e62c045            // SMULL2 V5.4S, V2.8H, V2.8H
	WORD $0x0e63c066            // SMULL  V6.4S, V3.4H, V3.4H
	WORD $0x4e63c067            // SMULL2 V7.4S, V3.8H, V3.8H

	// Sum all 16 squared diffs: tree reduction
	VADD V5.S4, V4.S4, V4.S4
	VADD V7.S4, V6.S4, V6.S4
	VADD V6.S4, V4.S4, V4.S4

	// Horizontal sum of 4×int32 → scalar
	WORD $0x4eb4bc84            // ADDP V4.4S, V4.4S, V4.4S
	WORD $0x4eb4bc84            // ADDP V4.4S, V4.4S, V4.4S

	// Extract scalar result
	WORD $0x1e260080            // FMOV W0, S4
	MOVD R0, ret+48(FP)
	RET

// func sse16x16NEON(pix, ref []byte) int
// SSE for 16x16 block with BPS=32 stride. NEON SIMD implementation.
// Each row is 16 contiguous bytes, loaded directly with VLD1.
// Uses UABDL for unsigned absolute diff, UMULL for squaring.
TEXT ·sse16x16NEON(SB), NOSPLIT, $0-56
	MOVD pix_base+0(FP), R0
	MOVD ref_base+24(FP), R1

	// V20 = accumulator (4×uint32), zeroed
	VEOR V20.B16, V20.B16, V20.B16

	MOVD $16, R8               // row counter

sse16x16_neon_row:
	// Load 16 src bytes and 16 ref bytes
	VLD1 (R0), [V0.B16]
	VLD1 (R1), [V1.B16]

	// Unsigned absolute difference: uint8→uint16
	WORD $0x2e217004            // UABDL  V4.8H, V0.8B, V1.8B
	WORD $0x6e217005            // UABDL2 V5.8H, V0.16B, V1.16B

	// Square the absolute diffs: uint16→uint32
	WORD $0x2e64c086            // UMULL  V6.4S, V4.4H, V4.4H
	WORD $0x6e64c087            // UMULL2 V7.4S, V4.8H, V4.8H
	WORD $0x2e65c0a8            // UMULL  V8.4S, V5.4H, V5.4H
	WORD $0x6e65c0a9            // UMULL2 V9.4S, V5.8H, V5.8H

	// Reduce 4 registers → 1 and accumulate
	VADD V7.S4, V6.S4, V6.S4
	VADD V9.S4, V8.S4, V8.S4
	VADD V8.S4, V6.S4, V6.S4
	VADD V6.S4, V20.S4, V20.S4

	ADD $BPS, R0               // next row
	ADD $BPS, R1
	SUBS $1, R8
	BNE sse16x16_neon_row

	// Horizontal sum of V20 (4×uint32) → scalar
	WORD $0x4eb4be94            // ADDP V20.4S, V20.4S, V20.4S
	WORD $0x4eb4be94            // ADDP V20.4S, V20.4S, V20.4S

	// Extract scalar result
	WORD $0x1e260280            // FMOV W0, S20
	MOVD R0, ret+48(FP)
	RET
