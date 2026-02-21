#include "textflag.h"

// VP8L lossless color transforms - AMD64 SSE2 assembly.
//
// These functions add or subtract the green channel to/from the red and blue
// channels of ARGB uint32 pixels. Used by the VP8L SubtractGreen transform
// (encoding) and its inverse AddGreenToBlueAndRed (decoding).
//
// Pixel layout in memory (little-endian uint32 ARGB):
//   byte 0 = B, byte 1 = G, byte 2 = R, byte 3 = A
//
// Strategy:
//   1. PSRLD $8 shifts each 32-bit lane right by 8 bits:
//      [B, G, R, A] -> [G, R, A, 0]
//   2. AND with 0x000000FF isolates green in byte 0:
//      [G, R, A, 0] & [FF, 0, 0, 0] -> [G, 0, 0, 0]
//   3. PSLLD $16 copies green to byte 2 (red position):
//      [G, 0, 0, 0] << 16 -> [0, 0, G, 0]
//   4. OR combines both: [G, 0, G, 0]
//   5. PADDB/PSUBB adds/subtracts green to/from original pixel bytes.
//      Only B (byte 0) and R (byte 2) are affected; G and A have 0 added.

// func addGreenToBlueAndRedSSE2(argb []uint32, numPixels int)
// Adds the green channel value to both red and blue channels for each pixel.
// Unrolled to process 8 pixels (32 bytes) per iteration for better ILP.
// Arguments (Plan9 ABI):
//   argb_base+0(FP)   = pointer to []uint32 data
//   argb_len+8(FP)    = slice length (unused)
//   argb_cap+16(FP)   = slice capacity (unused)
//   numPixels+24(FP)  = number of pixels to process
TEXT ·addGreenToBlueAndRedSSE2(SB), NOSPLIT, $0-32
	MOVQ argb_base+0(FP), SI    // SI = pointer to pixel data
	MOVQ numPixels+24(FP), CX   // CX = number of pixels

	CMPQ CX, $0
	JLE  addgreen_done

	// Build the 0x000000FF mask in X5 using register operations (no DATA).
	// PCMPEQD sets all bits to 1, PSRLD $24 shifts each dword to 0x000000FF.
	PCMPEQL X5, X5              // X5 = 0xFFFFFFFF x4
	PSRLL   $24, X5             // X5 = 0x000000FF x4

	// Process 8 pixels (32 bytes) per iteration.
	MOVQ CX, DX
	SHRQ $3, DX                 // DX = numPixels / 8
	JZ   addgreen_tail4         // fewer than 8 pixels, try 4-pixel path

addgreen_loop8:
	// First 4 pixels.
	MOVOU (SI), X0              // X0 = pixels 0-3
	MOVO  X0, X1
	PSRLL $8, X1
	PAND  X5, X1
	MOVO  X1, X2
	PSLLL $16, X2
	POR   X2, X1
	PADDB X1, X0
	MOVOU X0, (SI)

	// Second 4 pixels.
	MOVOU 16(SI), X3            // X3 = pixels 4-7
	MOVO  X3, X1
	PSRLL $8, X1
	PAND  X5, X1
	MOVO  X1, X2
	PSLLL $16, X2
	POR   X2, X1
	PADDB X1, X3
	MOVOU X3, 16(SI)

	ADDQ  $32, SI               // advance pointer by 8 pixels
	DECQ  DX
	JNZ   addgreen_loop8

addgreen_tail4:
	// Check if 4 remaining pixels.
	TESTQ $4, CX
	JZ    addgreen_tail

	MOVOU (SI), X0
	MOVO  X0, X1
	PSRLL $8, X1
	PAND  X5, X1
	MOVO  X1, X2
	PSLLL $16, X2
	POR   X2, X1
	PADDB X1, X0
	MOVOU X0, (SI)
	ADDQ  $16, SI

addgreen_tail:
	// Handle remaining 0-3 pixels one at a time.
	ANDQ $3, CX                 // CX = numPixels % 4
	JZ   addgreen_done

addgreen_tail_loop:
	MOVL  (SI), AX              // load one pixel (uint32)

	// Extract green = (pixel >> 8) & 0xFF
	MOVL  AX, BX
	SHRL  $8, BX
	ANDL  $0xFF, BX             // BX = green

	// Compute green * 0x00010001 = (green << 16) | green
	MOVL  BX, DX
	SHLL  $16, DX
	ORL   BX, DX                // DX = green in both R and B positions

	// Add green to red and blue, mask to keep only those channels.
	MOVL  AX, R8
	ANDL  $0x00FF00FF, R8       // R8 = original red and blue
	ADDL  DX, R8                // R8 = (red+green, blue+green)
	ANDL  $0x00FF00FF, R8       // mask overflow

	// Combine with original alpha and green.
	ANDL  $0xFF00FF00, AX       // AX = alpha and green channels
	ORL   R8, AX                // AX = final pixel
	MOVL  AX, (SI)              // store

	ADDQ  $4, SI                // advance by 1 pixel (4 bytes)
	DECQ  CX
	JNZ   addgreen_tail_loop

addgreen_done:
	RET

// func subtractGreenSSE2(argb []uint32, numPixels int)
// Subtracts the green channel value from both red and blue channels for each pixel.
// Unrolled to process 8 pixels (32 bytes) per iteration for better ILP.
// Arguments (Plan9 ABI):
//   argb_base+0(FP)   = pointer to []uint32 data
//   argb_len+8(FP)    = slice length (unused)
//   argb_cap+16(FP)   = slice capacity (unused)
//   numPixels+24(FP)  = number of pixels to process
TEXT ·subtractGreenSSE2(SB), NOSPLIT, $0-32
	MOVQ argb_base+0(FP), SI    // SI = pointer to pixel data
	MOVQ numPixels+24(FP), CX   // CX = number of pixels

	CMPQ CX, $0
	JLE  subgreen_done

	// Build the 0x000000FF mask in X5.
	PCMPEQL X5, X5              // X5 = 0xFFFFFFFF x4
	PSRLL   $24, X5             // X5 = 0x000000FF x4

	// Process 8 pixels (32 bytes) per iteration.
	MOVQ CX, DX
	SHRQ $3, DX                 // DX = numPixels / 8
	JZ   subgreen_tail4

subgreen_loop8:
	// First 4 pixels.
	MOVOU (SI), X0              // X0 = pixels 0-3
	MOVO  X0, X1
	PSRLL $8, X1
	PAND  X5, X1
	MOVO  X1, X2
	PSLLL $16, X2
	POR   X2, X1
	PSUBB X1, X0
	MOVOU X0, (SI)

	// Second 4 pixels.
	MOVOU 16(SI), X3            // X3 = pixels 4-7
	MOVO  X3, X1
	PSRLL $8, X1
	PAND  X5, X1
	MOVO  X1, X2
	PSLLL $16, X2
	POR   X2, X1
	PSUBB X1, X3
	MOVOU X3, 16(SI)

	ADDQ  $32, SI               // advance pointer by 8 pixels
	DECQ  DX
	JNZ   subgreen_loop8

subgreen_tail4:
	// Check if 4 remaining pixels.
	TESTQ $4, CX
	JZ    subgreen_tail

	MOVOU (SI), X0
	MOVO  X0, X1
	PSRLL $8, X1
	PAND  X5, X1
	MOVO  X1, X2
	PSLLL $16, X2
	POR   X2, X1
	PSUBB X1, X0
	MOVOU X0, (SI)
	ADDQ  $16, SI

subgreen_tail:
	// Handle remaining 0-3 pixels one at a time.
	ANDQ $3, CX                 // CX = numPixels % 4
	JZ   subgreen_done

subgreen_tail_loop:
	MOVL  (SI), AX              // load one pixel

	// Extract green = (pixel >> 8) & 0xFF
	MOVL  AX, BX
	SHRL  $8, BX
	ANDL  $0xFF, BX             // BX = green

	// Compute new red and blue.
	MOVL  AX, R8
	SHRL  $16, R8
	ANDL  $0xFF, R8             // R8 = red
	SUBL  BX, R8                // R8 = red - green
	ANDL  $0xFF, R8             // mask to byte

	MOVL  AX, R9
	ANDL  $0xFF, R9             // R9 = blue
	SUBL  BX, R9                // R9 = blue - green
	ANDL  $0xFF, R9             // mask to byte

	// Reassemble pixel: (A,G unchanged) | (new_red << 16) | new_blue
	ANDL  $0xFF00FF00, AX       // keep alpha and green
	SHLL  $16, R8               // shift red to position
	ORL   R8, AX
	ORL   R9, AX
	MOVL  AX, (SI)              // store

	ADDQ  $4, SI                // advance by 1 pixel
	DECQ  CX
	JNZ   subgreen_tail_loop

subgreen_done:
	RET
