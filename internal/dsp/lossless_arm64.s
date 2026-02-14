#include "textflag.h"

// func addGreenToBlueAndRedNEON(argb []uint32, numPixels int)
// Adds green channel to red and blue for each ARGB pixel.
// Pixel layout (little-endian uint32): byte0=B, byte1=G, byte2=R, byte3=A
TEXT ·addGreenToBlueAndRedNEON(SB), NOSPLIT, $0-32
	MOVD argb_base+0(FP), R0    // pixel data pointer
	MOVD numPixels+24(FP), R1   // count

	CMP $0, R1
	BLE addgreen_done

	// Build mask: 0x000000FF repeated
	MOVD $0xFF, R2
	VDUP R2, V5.S4              // V5 = [0x000000FF x4]

	// Process 4 pixels at a time
	LSR $2, R1, R2              // R2 = count / 4
	CBZ R2, addgreen_tail

addgreen_loop4:
	VLD1.P 16(R0), [V0.B16]    // load 4 pixels

	// Extract green: shift right 8, mask to isolate green byte
	VUSHR $8, V0.S4, V1.S4     // V1 = pixels >> 8
	VAND V5.B16, V1.B16, V1.B16 // V1 = green in byte 0 of each lane

	// Replicate green to byte 2 (red position)
	VSHL $16, V1.S4, V2.S4     // V2 = green << 16
	VORR V1.B16, V2.B16, V3.B16 // V3 = [G, 0, G, 0] per pixel

	// Add green to original (only affects B and R bytes)
	VADD V3.B16, V0.B16, V0.B16

	// Store back (we advanced R0 during load, store at R0-16)
	SUB $16, R0, R3
	VST1 [V0.B16], (R3)

	SUBS $1, R2
	BNE addgreen_loop4

addgreen_tail:
	// Handle remaining 0-3 pixels
	AND $3, R1, R2
	CBZ R2, addgreen_done

addgreen_scalar:
	MOVW (R0), R3               // load pixel
	UBFX $8, R3, $8, R4        // green = (p >> 8) & 0xFF
	LSL $16, R4, R5            // green << 16
	ORR R4, R5                 // green in B and R positions
	// Add green to B byte and R byte
	AND $0x00FF00FF, R3, R6    // isolate R and B
	ADD R5, R6                 // add green
	AND $0x00FF00FF, R6, R6    // mask overflow
	AND $0xFF00FF00, R3, R7    // isolate A and G
	ORR R6, R7, R3             // combine
	MOVW R3, (R0)
	ADD $4, R0
	SUBS $1, R2
	BNE addgreen_scalar

addgreen_done:
	RET

// func subtractGreenNEON(argb []uint32, numPixels int)
// Subtracts green channel from red and blue for each ARGB pixel.
TEXT ·subtractGreenNEON(SB), NOSPLIT, $0-32
	MOVD argb_base+0(FP), R0
	MOVD numPixels+24(FP), R1

	CMP $0, R1
	BLE subgreen_done

	MOVD $0xFF, R2
	VDUP R2, V5.S4

	LSR $2, R1, R2
	CBZ R2, subgreen_tail

subgreen_loop4:
	VLD1.P 16(R0), [V0.B16]

	VUSHR $8, V0.S4, V1.S4
	VAND V5.B16, V1.B16, V1.B16
	VSHL $16, V1.S4, V2.S4
	VORR V1.B16, V2.B16, V3.B16

	// Subtract green from original
	VSUB V3.B16, V0.B16, V0.B16

	SUB $16, R0, R3
	VST1 [V0.B16], (R3)

	SUBS $1, R2
	BNE subgreen_loop4

subgreen_tail:
	AND $3, R1, R2
	CBZ R2, subgreen_done

subgreen_scalar:
	MOVW (R0), R3
	UBFX $8, R3, $8, R4        // green
	// Subtract green from R and B
	UBFX $16, R3, $8, R5       // red
	SUB R4, R5                  // red - green
	AND $0xFF, R5               // mask
	UBFX $0, R3, $8, R6        // blue
	SUB R4, R6                  // blue - green
	AND $0xFF, R6               // mask
	AND $0xFF00FF00, R3, R7     // keep A and G
	ORR R6, R7                  // add blue
	LSL $16, R5, R5
	ORR R5, R7, R3              // add red
	MOVW R3, (R0)
	ADD $4, R0
	SUBS $1, R2
	BNE subgreen_scalar

subgreen_done:
	RET
