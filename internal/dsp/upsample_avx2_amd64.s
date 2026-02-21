#include "textflag.h"

// VP8 YUV→NRGBA batch converter — AMD64 AVX2 assembly.
//
// Processes 8 pixels per iteration (vs 4 for SSE2) using YMM registers.
// Same conversion formulas and coefficients as SSE2 version.
//
// Register allocation (constants preloaded before loop):
//   Y8  = kGBias (8708)      Y9  = kRBias (14234)
//   Y10 = kGCr (13320)       Y11 = kGCb (6419)
//   Y12 = kRCr (26149)       Y13 = kYScale (19077)
//   Y14 = 0x000000FF mask    Y15 = zero
//   Y0-Y7 = temporaries (Y2=U, Y4=V, Y6=yScaled preserved during compute)

// func yuvPackedToNRGBABatchAVX2(y []byte, packedUV []uint32, dst []byte, width int)
TEXT ·yuvPackedToNRGBABatchAVX2(SB), NOSPLIT, $0-80
	MOVQ y_base+0(FP), SI          // SI = Y pointer
	MOVQ packedUV_base+24(FP), DI  // DI = packed UV pointer
	MOVQ dst_base+48(FP), R8       // R8 = dst pointer
	MOVQ width+72(FP), CX          // CX = pixel count (must be multiple of 8)

	SHRQ $3, CX                    // CX = iterations (width / 8)
	JZ   yuv_avx2_done

	// Setup constants.
	VPXOR Y15, Y15, Y15            // Y15 = zero
	VPCMPEQD Y14, Y14, Y14
	VPSRLD   $24, Y14, Y14         // Y14 = 0x000000FF mask

	// Preload coefficient constants (each as [coeff, 0] int16 pair × 8 dwords).
	MOVL $19077, AX
	MOVQ AX, X13
	VPBROADCASTD X13, Y13          // kYScale

	MOVL $26149, AX
	MOVQ AX, X12
	VPBROADCASTD X12, Y12          // kRCr

	MOVL $6419, AX
	MOVQ AX, X11
	VPBROADCASTD X11, Y11          // kGCb

	MOVL $13320, AX
	MOVQ AX, X10
	VPBROADCASTD X10, Y10          // kGCr

	// Preload bias constants (as int32 × 8 dwords).
	MOVL $14234, AX
	MOVQ AX, X9
	VPBROADCASTD X9, Y9            // kRBias

	MOVL $8708, AX
	MOVQ AX, X8
	VPBROADCASTD X8, Y8            // kGBias

yuv_avx2_loop8:
	// --- Phase 1: Load and zero-extend 8 Y bytes to 8 dwords ---
	MOVQ     (SI), X0              // X0 = [y0..y7] (8 bytes)
	VPUNPCKLBW X15, X0, X0         // 8 int16 in XMM
	// VPUNPCKLWD X1, X0, X15 — VEX.128.66.0F 61 /r
	LONG $0x6179C1C4; BYTE $0xCF  // X1 = [y0d,y1d,y2d,y3d]
	// VPUNPCKHWD X0, X0, X15 — VEX.128.66.0F 69 /r
	LONG $0x6979C1C4; BYTE $0xC7  // X0 = [y4d,y5d,y6d,y7d]
	VINSERTI128 $1, X0, Y1, Y0     // Y0 = [y0d..y3d | y4d..y7d]

	// --- Phase 2: yScaled = (y * 19077) >> 8 ---
	// VPMADDWD Y0, Y0, Y13 — raw VEX encoding
	LONG $0xF57DC1C4; BYTE $0xC5  // y * kYScale
	VPSRAD   $8, Y0, Y6            // Y6 = yScaled (PRESERVED)

	// --- Phase 3: Extract U and V from packed UV ---
	VMOVDQU  (DI), Y0              // 8 packed UV dwords
	VPAND    Y14, Y0, Y2           // Y2 = U dwords (PRESERVED)
	VPSRLD   $16, Y0, Y4
	VPAND    Y14, Y4, Y4           // Y4 = V dwords (PRESERVED)

	// --- Phase 4: R = (yScaled + (v*26149)>>8 - 14234) >> 6 ---
	// VPMADDWD Y1, Y4, Y12 — raw VEX encoding
	LONG $0xF55DC1C4; BYTE $0xCC  // v * kRCr (Y4 preserved)
	VPSRAD   $8, Y1, Y1
	VPADDD   Y1, Y6, Y7            // yScaled + v_term
	VPSUBD   Y9, Y7, Y7            // - kRBias
	VPSRAD   $6, Y7, Y7            // Y7 = R [8 × int32]

	// --- Phase 5: G = (yScaled - (u*6419)>>8 - (v*13320)>>8 + 8708) >> 6 ---
	// VPMADDWD Y1, Y2, Y11 — raw VEX encoding
	LONG $0xF56DC1C4; BYTE $0xCB  // u * kGCb (Y2 preserved)
	VPSRAD   $8, Y1, Y1
	// VPMADDWD Y3, Y4, Y10 — raw VEX encoding
	LONG $0xF55DC1C4; BYTE $0xDA  // v * kGCr (Y4 preserved)
	VPSRAD   $8, Y3, Y3
	VMOVDQA  Y6, Y5                // yScaled
	VPSUBD   Y1, Y5, Y5            // - u_term
	VPSUBD   Y3, Y5, Y5            // - v_term
	VPADDD   Y8, Y5, Y5            // + kGBias
	VPSRAD   $6, Y5, Y5            // Y5 = G [8 × int32]

	// --- Phase 6: B = (yScaled + (u*16525)>>7 - 17685) >> 6 ---
	// Load kBCb_half and kBBias in-loop (not enough YMM regs).
	MOVL     $16525, AX
	MOVQ     AX, X0
	VPBROADCASTD X0, Y0            // kBCb_half
	// VPMADDWD Y1, Y2, Y0 — raw VEX encoding
	LONG $0xF56DE1C4; BYTE $0xC8  // u * kBCb_half
	VPSRAD   $7, Y1, Y1
	VPADDD   Y1, Y6, Y3            // yScaled + u_term
	MOVL     $17685, AX
	MOVQ     AX, X0
	VPBROADCASTD X0, Y0            // kBBias
	VPSUBD   Y0, Y3, Y3            // - kBBias
	VPSRAD   $6, Y3, Y3            // Y3 = B [8 × int32]

	// --- Phase 7: Pack int32 → int16 → uint8 ---
	// R (Y7): pack to int16, reorder lanes, pack to uint8.
	VPACKSSDW Y15, Y7, Y7          // lane0=[R0h..R3h,0..0], lane1=[R4h..R7h,0..0]
	VPERMQ    $0xD8, Y7, Y7        // lane0=[R0h..R3h,R4h..R7h], lane1=zeros
	VPACKUSWB Y15, Y7, Y7          // X7 = [R0..R7, 0..0]

	// G (Y5)
	VPACKSSDW Y15, Y5, Y5
	VPERMQ    $0xD8, Y5, Y5
	VPACKUSWB Y15, Y5, Y5          // X5 = [G0..G7, 0..0]

	// B (Y3)
	VPACKSSDW Y15, Y3, Y3
	VPERMQ    $0xD8, Y3, Y3
	VPACKUSWB Y15, Y3, Y3          // X3 = [B0..B7, 0..0]

	// --- Phase 8: Interleave R,G,B,A into NRGBA (XMM operations) ---
	VPCMPEQD X0, X0, X0            // X0 = 0xFF (alpha)
	VPUNPCKLBW X5, X7, X7          // X7 = [R0,G0,R1,G1,...,R7,G7]
	VPUNPCKLBW X0, X3, X3          // X3 = [B0,FF,B1,FF,...,B7,FF]
	VPUNPCKLWD X3, X7, X1          // X1 = pixels 0-3 [R0,G0,B0,FF,...]
	VPUNPCKHWD X3, X7, X0          // X0 = pixels 4-7 [R4,G4,B4,FF,...]
	VINSERTI128 $1, X0, Y1, Y1     // Y1 = [pixels 0-3 | pixels 4-7]
	VMOVDQU  Y1, (R8)              // Store 32 bytes (8 NRGBA pixels)

	ADDQ  $8, SI                    // Y += 8 pixels
	ADDQ  $32, DI                   // packed UV += 8 × 4 bytes
	ADDQ  $32, R8                   // dst += 8 × 4 bytes
	DECQ  CX
	JNZ   yuv_avx2_loop8

yuv_avx2_done:
	VZEROUPPER
	RET
