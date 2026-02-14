#include "textflag.h"

#define BPS 32

// func ve16asmNEON(dst []byte, off int)
// Vertical 16x16: copy top row to all 16 rows.
TEXT ·ve16asmNEON(SB), NOSPLIT, $0-32
	MOVD dst_base+0(FP), R0
	MOVD off+24(FP), R1
	ADD R1, R0                  // R0 = &dst[off]
	SUB $BPS, R0, R2            // R2 = &dst[off-BPS] (top row)
	VLD1 (R2), [V0.B16]        // load 16 top bytes

	VST1 [V0.B16], (R0)
	ADD $BPS, R0, R2
	VST1 [V0.B16], (R2)
	ADD $(2*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(3*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(4*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(5*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(6*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(7*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(8*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(9*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(10*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(11*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(12*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(13*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(14*BPS), R0, R2
	VST1 [V0.B16], (R2)
	ADD $(15*BPS), R0, R2
	VST1 [V0.B16], (R2)
	RET

// func he16asmNEON(dst []byte, off int)
// Horizontal 16x16: broadcast left pixel per row.
TEXT ·he16asmNEON(SB), NOSPLIT, $0-32
	MOVD dst_base+0(FP), R0
	MOVD off+24(FP), R1
	ADD R1, R0
	MOVD $16, R3

he16_loop:
	SUB $1, R0, R2
	MOVBU (R2), R4
	VDUP R4, V0.B16
	VST1 [V0.B16], (R0)
	ADD $BPS, R0
	SUBS $1, R3
	BNE he16_loop
	RET

// func dc16asmNEON(dst []byte, off int)
// DC 16x16: average top+left, fill block. Scalar sum + NEON fill.
TEXT ·dc16asmNEON(SB), NOSPLIT, $0-32
	MOVD dst_base+0(FP), R0
	MOVD off+24(FP), R1
	ADD R1, R0                  // R0 = &dst[off]

	// Sum 16 top pixels (scalar)
	SUB $BPS, R0, R2
	MOVD $0, R3
	MOVBU (R2), R4
	ADD R4, R3
	MOVBU 1(R2), R4
	ADD R4, R3
	MOVBU 2(R2), R4
	ADD R4, R3
	MOVBU 3(R2), R4
	ADD R4, R3
	MOVBU 4(R2), R4
	ADD R4, R3
	MOVBU 5(R2), R4
	ADD R4, R3
	MOVBU 6(R2), R4
	ADD R4, R3
	MOVBU 7(R2), R4
	ADD R4, R3
	MOVBU 8(R2), R4
	ADD R4, R3
	MOVBU 9(R2), R4
	ADD R4, R3
	MOVBU 10(R2), R4
	ADD R4, R3
	MOVBU 11(R2), R4
	ADD R4, R3
	MOVBU 12(R2), R4
	ADD R4, R3
	MOVBU 13(R2), R4
	ADD R4, R3
	MOVBU 14(R2), R4
	ADD R4, R3
	MOVBU 15(R2), R4
	ADD R4, R3

	// Sum 16 left pixels (scalar)
	SUB $1, R0, R2
	MOVBU (R2), R4
	ADD R4, R3
	MOVBU BPS(R2), R4
	ADD R4, R3
	MOVBU (2*BPS)(R2), R4
	ADD R4, R3
	MOVBU (3*BPS)(R2), R4
	ADD R4, R3
	MOVBU (4*BPS)(R2), R4
	ADD R4, R3
	MOVBU (5*BPS)(R2), R4
	ADD R4, R3
	MOVBU (6*BPS)(R2), R4
	ADD R4, R3
	MOVBU (7*BPS)(R2), R4
	ADD R4, R3
	MOVBU (8*BPS)(R2), R4
	ADD R4, R3
	MOVBU (9*BPS)(R2), R4
	ADD R4, R3
	MOVBU (10*BPS)(R2), R4
	ADD R4, R3
	MOVBU (11*BPS)(R2), R4
	ADD R4, R3
	MOVBU (12*BPS)(R2), R4
	ADD R4, R3
	MOVBU (13*BPS)(R2), R4
	ADD R4, R3
	MOVBU (14*BPS)(R2), R4
	ADD R4, R3
	MOVBU (15*BPS)(R2), R4
	ADD R4, R3

	// DC = (sum + 16) >> 5
	ADD $16, R3
	LSR $5, R3
	VDUP R3, V0.B16             // broadcast to 16 bytes

	// Fill 16 rows
	MOVD $16, R3
dc16_store:
	VST1 [V0.B16], (R0)
	ADD $BPS, R0
	SUBS $1, R3
	BNE dc16_store
	RET

// func tm16asmNEON(dst []byte, off int)
// TrueMotion 16x16: dst[i,j] = clip(left[j] + top[i] - tl).
// NEON vectorized: widen to uint16, add, saturating narrow.
// ~10 instructions per row vs ~68 scalar.
TEXT ·tm16asmNEON(SB), NOSPLIT, $0-32
	MOVD dst_base+0(FP), R0
	MOVD off+24(FP), R1
	ADD R1, R0                  // R0 = &dst[off]

	// Load top-left pixel
	SUB $(BPS+1), R0, R2
	MOVBU (R2), R3              // tl

	// Load 16 top pixels
	SUB $BPS, R0, R2            // &top
	VLD1 (R2), [V0.B16]        // V0 = 16 top pixels (uint8)

	// Widen top pixels: uint8 → uint16 (two 8-element vectors)
	WORD $0x2F08A401            // UXTL  V1.8H, V0.8B   (low 8 pixels)
	WORD $0x6F08A402            // UXTL2 V2.8H, V0.16B  (high 8 pixels)

	// Broadcast tl as uint16, compute diff = top - tl
	VDUP R3, V3.H8              // V3 = [tl, tl, ...] × 8 as uint16
	VSUB V3.H8, V1.H8, V4.H8   // V4 = top_lo - tl (int16)
	VSUB V3.H8, V2.H8, V5.H8   // V5 = top_hi - tl (int16)

	MOVD $16, R5                // row counter
tm16_neon_row:
	SUB $1, R0, R6
	MOVBU (R6), R7              // left pixel for this row

	// Broadcast left as uint16, compute result = left + diff
	VDUP R7, V6.H8              // V6 = [left, ...] × 8 as uint16
	VADD V4.H8, V6.H8, V1.H8   // V1 = left + (top_lo - tl)
	VADD V5.H8, V6.H8, V2.H8   // V2 = left + (top_hi - tl)

	// Saturating narrow int16 → uint8 (clips to [0,255])
	WORD $0x2E212820            // SQXTUN  V0.8B,  V1.8H  (low 8 bytes)
	WORD $0x6E212840            // SQXTUN2 V0.16B, V2.8H  (high 8 bytes)

	// Store 16 result bytes
	VST1 [V0.B16], (R0)

	ADD $BPS, R0
	SUBS $1, R5
	BNE tm16_neon_row
	RET

// func ve8uvasmNEON(dst []byte, off int)
// Vertical 8x8.
TEXT ·ve8uvasmNEON(SB), NOSPLIT, $0-32
	MOVD dst_base+0(FP), R0
	MOVD off+24(FP), R1
	ADD R1, R0
	SUB $BPS, R0, R2
	VLD1 (R2), [V0.B8]

	VST1 [V0.B8], (R0)
	ADD $BPS, R0, R2
	VST1 [V0.B8], (R2)
	ADD $(2*BPS), R0, R2
	VST1 [V0.B8], (R2)
	ADD $(3*BPS), R0, R2
	VST1 [V0.B8], (R2)
	ADD $(4*BPS), R0, R2
	VST1 [V0.B8], (R2)
	ADD $(5*BPS), R0, R2
	VST1 [V0.B8], (R2)
	ADD $(6*BPS), R0, R2
	VST1 [V0.B8], (R2)
	ADD $(7*BPS), R0, R2
	VST1 [V0.B8], (R2)
	RET

// func he8uvasmNEON(dst []byte, off int)
// Horizontal 8x8.
TEXT ·he8uvasmNEON(SB), NOSPLIT, $0-32
	MOVD dst_base+0(FP), R0
	MOVD off+24(FP), R1
	ADD R1, R0
	MOVD $8, R3

he8_loop:
	SUB $1, R0, R2
	MOVBU (R2), R4
	VDUP R4, V0.B8
	VST1 [V0.B8], (R0)
	ADD $BPS, R0
	SUBS $1, R3
	BNE he8_loop
	RET

// func dc8uvasmNEON(dst []byte, off int)
// DC 8x8.
TEXT ·dc8uvasmNEON(SB), NOSPLIT, $0-32
	MOVD dst_base+0(FP), R0
	MOVD off+24(FP), R1
	ADD R1, R0

	// Sum 8 top pixels (scalar)
	SUB $BPS, R0, R2
	MOVD $0, R3
	MOVBU (R2), R4
	ADD R4, R3
	MOVBU 1(R2), R4
	ADD R4, R3
	MOVBU 2(R2), R4
	ADD R4, R3
	MOVBU 3(R2), R4
	ADD R4, R3
	MOVBU 4(R2), R4
	ADD R4, R3
	MOVBU 5(R2), R4
	ADD R4, R3
	MOVBU 6(R2), R4
	ADD R4, R3
	MOVBU 7(R2), R4
	ADD R4, R3

	// Sum 8 left pixels (scalar)
	SUB $1, R0, R2
	MOVBU (R2), R4
	ADD R4, R3
	MOVBU BPS(R2), R4
	ADD R4, R3
	MOVBU (2*BPS)(R2), R4
	ADD R4, R3
	MOVBU (3*BPS)(R2), R4
	ADD R4, R3
	MOVBU (4*BPS)(R2), R4
	ADD R4, R3
	MOVBU (5*BPS)(R2), R4
	ADD R4, R3
	MOVBU (6*BPS)(R2), R4
	ADD R4, R3
	MOVBU (7*BPS)(R2), R4
	ADD R4, R3

	// DC = (sum + 8) >> 4
	ADD $8, R3
	LSR $4, R3
	VDUP R3, V0.B8

	MOVD $8, R3
dc8_store:
	VST1 [V0.B8], (R0)
	ADD $BPS, R0
	SUBS $1, R3
	BNE dc8_store
	RET

// func tm8uvasmNEON(dst []byte, off int)
// TrueMotion 8x8. NEON vectorized.
TEXT ·tm8uvasmNEON(SB), NOSPLIT, $0-32
	MOVD dst_base+0(FP), R0
	MOVD off+24(FP), R1
	ADD R1, R0

	SUB $(BPS+1), R0, R2
	MOVBU (R2), R3              // tl

	SUB $BPS, R0, R2            // &top
	VLD1 (R2), [V0.B8]         // V0 = 8 top pixels (uint8)

	// Widen top pixels: uint8 → uint16
	WORD $0x2F08A401            // UXTL V1.8H, V0.8B

	// Broadcast tl as uint16, compute diff = top - tl
	VDUP R3, V3.H8
	VSUB V3.H8, V1.H8, V4.H8   // V4 = top - tl (int16)

	MOVD $8, R5
tm8_neon_row:
	SUB $1, R0, R6
	MOVBU (R6), R7              // left

	VDUP R7, V6.H8
	VADD V4.H8, V6.H8, V1.H8   // V1 = left + (top - tl)

	// Saturating narrow int16 → uint8
	WORD $0x2E212820            // SQXTUN V0.8B, V1.8H

	VST1 [V0.B8], (R0)

	ADD $BPS, R0
	SUBS $1, R5
	BNE tm8_neon_row
	RET
