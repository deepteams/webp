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

	// Process 4 pixels (16 bytes) per iteration.
	MOVQ CX, DX
	SHRQ $2, DX                 // DX = numPixels / 4
	JZ   addgreen_tail          // fewer than 4 pixels, skip SSE loop

addgreen_loop4:
	MOVOU (SI), X0              // X0 = 4 pixels [B0,G0,R0,A0, B1,G1,R1,A1, ...]

	// Extract green into byte 0 of each lane.
	MOVO  X0, X1
	PSRLL $8, X1                // X1 = [G,R,A,0] per pixel
	PAND  X5, X1                // X1 = [G,0,0,0] per pixel

	// Replicate green into byte 2 (red position).
	MOVO  X1, X2
	PSLLL $16, X2               // X2 = [0,0,G,0] per pixel
	POR   X2, X1                // X1 = [G,0,G,0] per pixel

	// Add green to blue and red channels (byte-wise, no cross-byte carry).
	PADDB X1, X0                // X0 = [B+G, G+0, R+G, A+0] per pixel

	MOVOU X0, (SI)              // store result
	ADDQ  $16, SI               // advance pointer by 4 pixels
	DECQ  DX
	JNZ   addgreen_loop4

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

	// Process 4 pixels per iteration.
	MOVQ CX, DX
	SHRQ $2, DX                 // DX = numPixels / 4
	JZ   subgreen_tail

subgreen_loop4:
	MOVOU (SI), X0              // X0 = 4 pixels

	// Extract green into byte 0 of each lane.
	MOVO  X0, X1
	PSRLL $8, X1                // X1 = [G,R,A,0] per pixel
	PAND  X5, X1                // X1 = [G,0,0,0] per pixel

	// Replicate green into byte 2 (red position).
	MOVO  X1, X2
	PSLLL $16, X2               // X2 = [0,0,G,0] per pixel
	POR   X2, X1                // X1 = [G,0,G,0] per pixel

	// Subtract green from blue and red channels (byte-wise).
	PSUBB X1, X0                // X0 = [B-G, G-0, R-G, A-0] per pixel

	MOVOU X0, (SI)              // store result
	ADDQ  $16, SI               // advance pointer by 4 pixels
	DECQ  DX
	JNZ   subgreen_loop4

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
