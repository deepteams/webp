#include "textflag.h"

// VP8 intra prediction modes - AMD64 SSE2 assembly.
//
// All functions have signature: func xxxasmSSE2(dst []byte, off int)
// Arguments on stack (Plan9 ABI):
//   dst_base+0(FP)   = pointer to dst data
//   dst_len+8(FP)    = length (unused)
//   dst_cap+16(FP)   = capacity (unused)
//   off+24(FP)       = byte offset into dst
//
// BPS = 32 (stride between rows).

#define BPS 32

// ============================================================================
// 16x16 luma prediction modes
// ============================================================================

// func ve16asmSSE2(dst []byte, off int)
// Vertical 16x16: copy the 16 top-row pixels to all 16 rows.
TEXT ·ve16asmSSE2(SB), NOSPLIT, $0-32
	MOVQ dst_base+0(FP), SI
	MOVQ off+24(FP), AX
	ADDQ AX, SI              // SI = &dst[off]

	// Load 16 bytes from top row: dst[off - BPS .. off - BPS + 15]
	MOVOU -BPS(SI), X0

	// Store to 16 rows at stride BPS
	MOVOU X0, 0(SI)
	MOVOU X0, BPS(SI)
	MOVOU X0, 2*BPS(SI)
	MOVOU X0, 3*BPS(SI)
	MOVOU X0, 4*BPS(SI)
	MOVOU X0, 5*BPS(SI)
	MOVOU X0, 6*BPS(SI)
	MOVOU X0, 7*BPS(SI)
	MOVOU X0, 8*BPS(SI)
	MOVOU X0, 9*BPS(SI)
	MOVOU X0, 10*BPS(SI)
	MOVOU X0, 11*BPS(SI)
	MOVOU X0, 12*BPS(SI)
	MOVOU X0, 13*BPS(SI)
	MOVOU X0, 14*BPS(SI)
	MOVOU X0, 15*BPS(SI)
	RET

// func he16asmSSE2(dst []byte, off int)
// Horizontal 16x16: for each row j, broadcast dst[off - 1 + j*BPS] to 16 bytes.
TEXT ·he16asmSSE2(SB), NOSPLIT, $0-32
	MOVQ dst_base+0(FP), SI
	MOVQ off+24(FP), AX
	ADDQ AX, SI              // SI = &dst[off]

	MOVQ $16, CX             // row counter
	XORQ DX, DX              // row offset = 0

he16_loop:
	// Load left pixel: dst[off - 1 + j*BPS]
	MOVBQZX -1(SI)(DX*1), AX

	// Broadcast byte to all 16 positions in XMM register.
	// MOVD AX -> X0 (low 32 bits), then replicate via PUNPCKLBW chain + PSHUFD.
	MOVD AX, X0              // X0 = [0 0 0 b 0 0 0 0 0 0 0 0 0 0 0 0]
	PUNPCKLBW X0, X0         // X0 = [0 0 0 0 0 0 b b 0 0 0 0 0 0 0 0]
	PUNPCKLWL X0, X0         // X0 = [0 0 0 0 b b b b 0 0 0 0 0 0 0 0]
	PSHUFD $0x00, X0, X0     // broadcast low dword to all 4 dwords

	// Store 16 bytes to this row
	MOVOU X0, (SI)(DX*1)

	ADDQ $BPS, DX
	DECQ CX
	JNZ he16_loop
	RET

// func dc16asmSSE2(dst []byte, off int)
// DC 16x16: average of 16 top + 16 left pixels, fill 16x16 block.
TEXT ·dc16asmSSE2(SB), NOSPLIT, $0-32
	MOVQ dst_base+0(FP), SI
	MOVQ off+24(FP), AX
	ADDQ AX, SI              // SI = &dst[off]

	PXOR X7, X7              // zero register

	// Sum 16 top pixels using PSADBW (sum of absolute differences vs zero = sum of bytes).
	// PSADBW computes: sum(|a[i] - 0|) for each 8-byte half -> two uint16 sums in positions 0 and 8.
	MOVOU -BPS(SI), X0       // load 16 top pixels
	PSADBW X7, X0            // X0 = [sum_hi(63:0) | sum_lo(63:0)]
	// X0 now has sum of bytes[0..7] in bits[15:0] and sum of bytes[8..15] in bits[79:64].

	// Sum 16 left pixels (non-contiguous, stride BPS).
	// Load them one by one and accumulate.
	XORQ AX, AX              // left sum accumulator

	MOVBQZX -1(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+2*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+3*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+4*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+5*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+6*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+7*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+8*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+9*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+10*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+11*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+12*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+13*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+14*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+15*BPS(SI), DX
	ADDQ DX, AX

	// Add top sum (from X0) to left sum (in AX).
	// Extract the two 64-bit halves and add them.
	MOVQ X0, DX              // low 64 bits (sum of bytes 0..7)
	PSRLDQ $8, X0            // shift right 8 bytes
	MOVQ X0, BX              // high 64 bits (sum of bytes 8..15)
	ADDQ BX, DX              // DX = top_sum
	ADDQ DX, AX              // AX = top_sum + left_sum

	// DC value = (sum + 16) >> 5
	ADDQ $16, AX
	SHRQ $5, AX

	// Broadcast DC byte to XMM register
	MOVD AX, X0
	PUNPCKLBW X0, X0
	PUNPCKLWL X0, X0
	PSHUFD $0x00, X0, X0

	// Fill 16 rows
	MOVOU X0, 0(SI)
	MOVOU X0, BPS(SI)
	MOVOU X0, 2*BPS(SI)
	MOVOU X0, 3*BPS(SI)
	MOVOU X0, 4*BPS(SI)
	MOVOU X0, 5*BPS(SI)
	MOVOU X0, 6*BPS(SI)
	MOVOU X0, 7*BPS(SI)
	MOVOU X0, 8*BPS(SI)
	MOVOU X0, 9*BPS(SI)
	MOVOU X0, 10*BPS(SI)
	MOVOU X0, 11*BPS(SI)
	MOVOU X0, 12*BPS(SI)
	MOVOU X0, 13*BPS(SI)
	MOVOU X0, 14*BPS(SI)
	MOVOU X0, 15*BPS(SI)
	RET

// func tm16asmSSE2(dst []byte, off int)
// TrueMotion 16x16: dst[i,j] = clip(left[j] + top[i] - top_left)
// For each row j:
//   left_minus_tl = left[j] - top_left (scalar, broadcast to int16 x 8)
//   result[i] = clip_u8(left_minus_tl + top[i])
// Process 16 columns as two groups of 8 int16 values.
TEXT ·tm16asmSSE2(SB), NOSPLIT, $0-32
	MOVQ dst_base+0(FP), SI
	MOVQ off+24(FP), AX
	ADDQ AX, SI              // SI = &dst[off]

	PXOR X7, X7              // zero register

	// Load top-left pixel, broadcast as int16
	MOVBQZX -1-BPS(SI), AX   // top_left

	// Load 16 top pixels, zero-extend to two registers of 8 x int16
	MOVOU -BPS(SI), X4       // top row (16 bytes)
	MOVO X4, X5
	PUNPCKLBW X7, X4         // X4 = top[0..7] as int16
	PUNPCKHBW X7, X5         // X5 = top[8..15] as int16

	// Broadcast top_left as int16
	MOVD AX, X6
	PUNPCKLWL X6, X6
	PSHUFD $0x00, X6, X6     // X6 = tl tl tl tl tl tl tl tl (int16)

	// Subtract top_left from top to get (top - tl) as int16
	// This gives us a base that we add left[j] to for each row.
	PSUBW X6, X4             // X4 = top[0..7] - tl (int16)
	PSUBW X6, X5             // X5 = top[8..15] - tl (int16)

	MOVQ $16, CX             // row counter
	XORQ DX, DX              // row offset

tm16_loop:
	// Load left pixel for this row
	MOVBQZX -1(SI)(DX*1), AX

	// Broadcast left as int16
	MOVD AX, X0
	PUNPCKLWL X0, X0
	PSHUFD $0x00, X0, X0     // X0 = left left left left ... (8 x int16)

	// result = left + (top - tl) = left + top - tl
	MOVO X4, X2
	PADDW X0, X2             // X2 = left + top[0..7] - tl (int16)
	MOVO X5, X3
	PADDW X0, X3             // X3 = left + top[8..15] - tl (int16)

	// Clip to [0, 255] using PACKUSWB (saturating pack int16 -> uint8)
	PACKUSWB X3, X2          // X2 = 16 clipped bytes

	// Store result
	MOVOU X2, (SI)(DX*1)

	ADDQ $BPS, DX
	DECQ CX
	JNZ tm16_loop
	RET

// ============================================================================
// 8x8 chroma prediction modes
// ============================================================================

// func ve8uvasmSSE2(dst []byte, off int)
// Vertical 8x8: copy top 8 bytes to all 8 rows.
TEXT ·ve8uvasmSSE2(SB), NOSPLIT, $0-32
	MOVQ dst_base+0(FP), SI
	MOVQ off+24(FP), AX
	ADDQ AX, SI              // SI = &dst[off]

	// Load 8 bytes from top row
	MOVQ -BPS(SI), X0

	// Store to 8 rows
	MOVQ X0, 0(SI)
	MOVQ X0, BPS(SI)
	MOVQ X0, 2*BPS(SI)
	MOVQ X0, 3*BPS(SI)
	MOVQ X0, 4*BPS(SI)
	MOVQ X0, 5*BPS(SI)
	MOVQ X0, 6*BPS(SI)
	MOVQ X0, 7*BPS(SI)
	RET

// func he8uvasmSSE2(dst []byte, off int)
// Horizontal 8x8: for each row j, broadcast dst[off - 1 + j*BPS] to 8 bytes.
TEXT ·he8uvasmSSE2(SB), NOSPLIT, $0-32
	MOVQ dst_base+0(FP), SI
	MOVQ off+24(FP), AX
	ADDQ AX, SI              // SI = &dst[off]

	MOVQ $8, CX              // row counter
	XORQ DX, DX              // row offset = 0

he8uv_loop:
	// Load left pixel
	MOVBQZX -1(SI)(DX*1), AX

	// Broadcast byte to XMM using PUNPCKLBW chain, then store low 8 bytes.
	MOVD AX, X0
	PUNPCKLBW X0, X0
	PUNPCKLWL X0, X0
	PSHUFD $0x00, X0, X0

	// Store low 8 bytes
	MOVQ X0, (SI)(DX*1)

	ADDQ $BPS, DX
	DECQ CX
	JNZ he8uv_loop
	RET

// func dc8uvasmSSE2(dst []byte, off int)
// DC 8x8: average of 8 top + 8 left pixels, fill 8x8 block.
TEXT ·dc8uvasmSSE2(SB), NOSPLIT, $0-32
	MOVQ dst_base+0(FP), SI
	MOVQ off+24(FP), AX
	ADDQ AX, SI              // SI = &dst[off]

	PXOR X7, X7              // zero register

	// Sum 8 top pixels using PSADBW.
	MOVQ -BPS(SI), X0        // load 8 top pixels into low 64 bits
	PSADBW X7, X0            // X0[15:0] = sum of 8 bytes

	// Sum 8 left pixels.
	XORQ AX, AX

	MOVBQZX -1(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+2*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+3*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+4*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+5*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+6*BPS(SI), DX
	ADDQ DX, AX
	MOVBQZX -1+7*BPS(SI), DX
	ADDQ DX, AX

	// Add top sum to left sum.
	MOVQ X0, DX              // top sum
	ADDQ DX, AX              // AX = top_sum + left_sum

	// DC value = (sum + 8) >> 4
	ADDQ $8, AX
	SHRQ $4, AX

	// Broadcast DC byte to XMM, store low 8 bytes per row.
	MOVD AX, X0
	PUNPCKLBW X0, X0
	PUNPCKLWL X0, X0
	PSHUFD $0x00, X0, X0

	// Fill 8 rows (8 bytes each)
	MOVQ X0, 0(SI)
	MOVQ X0, BPS(SI)
	MOVQ X0, 2*BPS(SI)
	MOVQ X0, 3*BPS(SI)
	MOVQ X0, 4*BPS(SI)
	MOVQ X0, 5*BPS(SI)
	MOVQ X0, 6*BPS(SI)
	MOVQ X0, 7*BPS(SI)
	RET

// func tm8uvasmSSE2(dst []byte, off int)
// TrueMotion 8x8: dst[i,j] = clip(left[j] + top[i] - top_left)
TEXT ·tm8uvasmSSE2(SB), NOSPLIT, $0-32
	MOVQ dst_base+0(FP), SI
	MOVQ off+24(FP), AX
	ADDQ AX, SI              // SI = &dst[off]

	PXOR X7, X7              // zero register

	// Load top-left pixel
	MOVBQZX -1-BPS(SI), AX

	// Load 8 top pixels, zero-extend to int16
	MOVQ -BPS(SI), X4        // 8 top bytes
	PUNPCKLBW X7, X4         // X4 = top[0..7] as 8 x int16

	// Broadcast top_left as int16
	MOVD AX, X6
	PUNPCKLWL X6, X6
	PSHUFD $0x00, X6, X6     // X6 = tl (8 x int16)

	// top - tl as int16 base
	PSUBW X6, X4             // X4 = top[0..7] - tl

	MOVQ $8, CX              // row counter
	XORQ DX, DX              // row offset

tm8uv_loop:
	// Load left pixel
	MOVBQZX -1(SI)(DX*1), AX

	// Broadcast left as int16
	MOVD AX, X0
	PUNPCKLWL X0, X0
	PSHUFD $0x00, X0, X0     // X0 = left (8 x int16)

	// result = left + top - tl
	MOVO X4, X2
	PADDW X0, X2             // X2 = left + top[0..7] - tl

	// Clip to [0, 255]: PACKUSWB packs two int16 vectors into one uint8 vector.
	// We need a second source for the high 8 bytes; use zero for the high half
	// since we only have 8 pixels.
	PACKUSWB X7, X2          // X2 low 8 bytes = clipped result, high 8 bytes = 0

	// Store 8 bytes
	MOVQ X2, (SI)(DX*1)

	ADDQ $BPS, DX
	DECQ CX
	JNZ tm8uv_loop
	RET
