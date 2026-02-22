#include "textflag.h"

// VP8 YUV→NRGBA batch converter — AMD64 SSE2 assembly.
//
// Converts N pixels from Y byte array + packed UV uint32 array into
// interleaved NRGBA output (R, G, B, 255 per pixel). Processes 4 pixels
// per iteration using PMADDWL (PMADDWD) for exact fixed-point multiplication.
//
// The packedUV input uses the loadUV format: u in bits [7:0], v in bits [23:16].
// Each dword naturally forms a [val, 0] int16 pair suitable for PMADDWL after
// masking, eliminating the need for PUNPCKLBW/PUNPCKLWL on chroma channels.
//
// Conversion formulas (matching yuv.go constants):
//   multHi(v, c) = (v * c) >> 8
//   R = clip((multHi(y,19077) + multHi(v,26149) - 14234) >> 6, 0, 255)
//   G = clip((multHi(y,19077) - multHi(u,6419) - multHi(v,13320) + 8708) >> 6, 0, 255)
//   B = clip((multHi(y,19077) + multHi(u,33050) - 17685) >> 6, 0, 255)
//
// For kBCb=33050 which exceeds int16, we use kBCb_half=16525 with >>7:
//   (u*16525)>>7 == (u*33050)>>8 since 33050 = 2*16525 exactly.

// Coefficient constants for PMADDWL: [coeff, 0] repeated 4 times as int16 pairs.
// Each 4-byte word = coeff as uint32 (low int16 = coeff, high int16 = 0).
DATA kYScale_pair<>+0x00(SB)/4, $19077
DATA kYScale_pair<>+0x04(SB)/4, $19077
DATA kYScale_pair<>+0x08(SB)/4, $19077
DATA kYScale_pair<>+0x0c(SB)/4, $19077
GLOBL kYScale_pair<>(SB), RODATA|NOPTR, $16

DATA kRCr_pair<>+0x00(SB)/4, $26149
DATA kRCr_pair<>+0x04(SB)/4, $26149
DATA kRCr_pair<>+0x08(SB)/4, $26149
DATA kRCr_pair<>+0x0c(SB)/4, $26149
GLOBL kRCr_pair<>(SB), RODATA|NOPTR, $16

DATA kGCb_pair<>+0x00(SB)/4, $6419
DATA kGCb_pair<>+0x04(SB)/4, $6419
DATA kGCb_pair<>+0x08(SB)/4, $6419
DATA kGCb_pair<>+0x0c(SB)/4, $6419
GLOBL kGCb_pair<>(SB), RODATA|NOPTR, $16

DATA kGCr_pair<>+0x00(SB)/4, $13320
DATA kGCr_pair<>+0x04(SB)/4, $13320
DATA kGCr_pair<>+0x08(SB)/4, $13320
DATA kGCr_pair<>+0x0c(SB)/4, $13320
GLOBL kGCr_pair<>(SB), RODATA|NOPTR, $16

// kBCb_half = 33050/2 = 16525. Use >>7 instead of >>8.
DATA kBCb_half_pair<>+0x00(SB)/4, $16525
DATA kBCb_half_pair<>+0x04(SB)/4, $16525
DATA kBCb_half_pair<>+0x08(SB)/4, $16525
DATA kBCb_half_pair<>+0x0c(SB)/4, $16525
GLOBL kBCb_half_pair<>(SB), RODATA|NOPTR, $16

// Bias constants as 4 x int32.
DATA kRBias_32<>+0x00(SB)/4, $14234
DATA kRBias_32<>+0x04(SB)/4, $14234
DATA kRBias_32<>+0x08(SB)/4, $14234
DATA kRBias_32<>+0x0c(SB)/4, $14234
GLOBL kRBias_32<>(SB), RODATA|NOPTR, $16

DATA kGBias_32<>+0x00(SB)/4, $8708
DATA kGBias_32<>+0x04(SB)/4, $8708
DATA kGBias_32<>+0x08(SB)/4, $8708
DATA kGBias_32<>+0x0c(SB)/4, $8708
GLOBL kGBias_32<>(SB), RODATA|NOPTR, $16

DATA kBBias_32<>+0x00(SB)/4, $17685
DATA kBBias_32<>+0x04(SB)/4, $17685
DATA kBBias_32<>+0x08(SB)/4, $17685
DATA kBBias_32<>+0x0c(SB)/4, $17685
GLOBL kBBias_32<>(SB), RODATA|NOPTR, $16

// func yuvPackedToNRGBABatchSSE2(y []byte, packedUV []uint32, dst []byte, width int)
// Converts width pixels (must be multiple of 4) from Y bytes + packed UV to NRGBA.
// packedUV[i] = u | (v << 16), where u,v are in [0,255].
// Output: dst[i*4+0]=R, dst[i*4+1]=G, dst[i*4+2]=B, dst[i*4+3]=255.
//
// Arguments (Plan9 ABI):
//   y_base+0(FP)         = pointer to Y bytes
//   y_len+8(FP)          = slice length (unused)
//   y_cap+16(FP)         = slice capacity (unused)
//   packedUV_base+24(FP) = pointer to packed UV uint32 array
//   packedUV_len+32(FP)  = slice length (unused)
//   packedUV_cap+40(FP)  = slice capacity (unused)
//   dst_base+48(FP)      = pointer to NRGBA output
//   dst_len+56(FP)       = slice length (unused)
//   dst_cap+64(FP)       = slice capacity (unused)
//   width+72(FP)         = number of pixels (must be multiple of 4)
TEXT ·yuvPackedToNRGBABatchSSE2(SB), NOSPLIT, $0-80
	MOVQ y_base+0(FP), SI          // SI = Y pointer
	MOVQ packedUV_base+24(FP), DI  // DI = packed UV pointer
	MOVQ dst_base+48(FP), R8       // R8 = dst pointer
	MOVQ width+72(FP), CX          // CX = pixel count (multiple of 4)

	SHRQ $2, CX                    // CX = iterations (width / 4)
	JZ   yuv_done

	PXOR X15, X15                  // X15 = zero register

	// Build 0x000000FF dword mask for extracting byte from each dword.
	PCMPEQL X14, X14               // X14 = all 1s
	PSRLL   $24, X14               // X14 = [0x000000FF × 4]

yuv_loop4:
	// Load 4 Y bytes → zero-extend to [val, 0] int16 pairs for PMADDWL.
	MOVD  (SI), X0                 // X0 = [y0,y1,y2,y3, 0..0] (4 bytes in low dword)
	PUNPCKLBW X15, X0              // byte→int16: [y0,y1,y2,y3, 0,0,0,0]
	PUNPCKLWL X15, X0              // int16→[y0,0, y1,0, y2,0, y3,0] pairs

	// yScaled = (y * kYScale) >> 8
	MOVOU kYScale_pair<>(SB), X12
	PMADDWL X12, X0                // X0 = y*kYScale as 4 x int32
	PSRAL $8, X0                   // X0 = yScaled
	MOVO  X0, X13                  // X13 = yScaled (preserved for R, G, B)

	// Load 4 packed UV values (4 × uint32 = 16 bytes).
	MOVOU (DI), X8                 // X8 = [uv0, uv1, uv2, uv3]

	// Extract U = uv & 0xFF (low byte of each dword).
	// Each dword becomes [u, 0, 0, 0] = [u, 0] as int16 pair → ready for PMADDWL.
	MOVO  X8, X2
	PAND  X14, X2                  // X2 = [u0, u1, u2, u3] as dwords

	// Extract V = (uv >> 16) & 0xFF (byte 2 of each dword).
	MOVO  X8, X4
	PSRLL $16, X4
	PAND  X14, X4                  // X4 = [v0, v1, v2, v3] as dwords

	// === R = (yScaled + (v*kRCr)>>8 - kRBias) >> 6 ===
	MOVO  X4, X1                   // copy V dwords
	MOVOU kRCr_pair<>(SB), X6
	PMADDWL X6, X1                 // X1 = v*kRCr as int32
	PSRAL $8, X1                   // (v*kRCr) >> 8
	MOVO  X13, X7                  // yScaled
	PADDL X1, X7                   // + (v*kRCr)>>8
	MOVOU kRBias_32<>(SB), X1
	PSUBL X1, X7                   // - kRBias
	PSRAL $6, X7                   // >> yuvFix2
	// X7 = R as 4 x int32

	// === G = (yScaled - (u*kGCb)>>8 - (v*kGCr)>>8 + kGBias) >> 6 ===
	MOVO  X2, X1                   // copy U dwords
	MOVOU kGCb_pair<>(SB), X6
	PMADDWL X6, X1                 // u*kGCb
	PSRAL $8, X1                   // (u*kGCb) >> 8
	MOVO  X4, X3                   // copy V dwords
	MOVOU kGCr_pair<>(SB), X6
	PMADDWL X6, X3                 // v*kGCr
	PSRAL $8, X3                   // (v*kGCr) >> 8
	MOVO  X13, X5                  // yScaled
	PSUBL X1, X5                   // - (u*kGCb)>>8
	PSUBL X3, X5                   // - (v*kGCr)>>8
	MOVOU kGBias_32<>(SB), X1
	PADDL X1, X5                   // + kGBias
	PSRAL $6, X5                   // >> yuvFix2
	// X5 = G as 4 x int32

	// === B = (yScaled + (u*kBCb_half)>>7 - kBBias) >> 6 ===
	MOVO  X2, X1                   // copy U dwords
	MOVOU kBCb_half_pair<>(SB), X6
	PMADDWL X6, X1                 // u*kBCb_half
	PSRAL $7, X1                   // (u*kBCb_half)>>7 = (u*kBCb)>>8
	MOVO  X13, X3                  // yScaled
	PADDL X1, X3                   // + (u*kBCb)>>8
	MOVOU kBBias_32<>(SB), X1
	PSUBL X1, X3                   // - kBBias
	PSRAL $6, X3                   // >> yuvFix2
	// X3 = B as 4 x int32

	// Pack int32 → int16 (signed saturation) → uint8 (unsigned saturation).
	PACKSSLW X7, X7                // X7 = [R0,R1,R2,R3, R0,R1,R2,R3] as int16
	PACKSSLW X5, X5                // X5 = [G0,G1,G2,G3, ...] as int16
	PACKSSLW X3, X3                // X3 = [B0,B1,B2,B3, ...] as int16
	PACKUSWB X7, X7                // X7 = [R0,R1,R2,R3, ...] as uint8
	PACKUSWB X5, X5                // X5 = [G0,G1,G2,G3, ...] as uint8
	PACKUSWB X3, X3                // X3 = [B0,B1,B2,B3, ...] as uint8

	// Alpha = 255 (all bits set).
	PCMPEQL X6, X6                 // X6 = [0xFF, 0xFF, ...] as bytes

	// Interleave R, G, B, A into NRGBA pixel format.
	PUNPCKLBW X5, X7               // X7 = [R0,G0, R1,G1, R2,G2, R3,G3, ...]
	PUNPCKLBW X6, X3               // X3 = [B0,FF, B1,FF, B2,FF, B3,FF, ...]
	MOVO  X7, X0
	PUNPCKLWL X3, X0               // X0 = [R0,G0,B0,FF, R1,G1,B1,FF, R2,G2,B2,FF, R3,G3,B3,FF]

	// Store 16 bytes (4 NRGBA pixels).
	MOVOU X0, (R8)

	ADDQ  $4, SI                   // Y += 4 pixels
	ADDQ  $16, DI                  // packed UV += 4 × 4 bytes
	ADDQ  $16, R8                  // dst += 4 × 4 bytes
	DECQ  CX
	JNZ   yuv_loop4

yuv_done:
	RET
