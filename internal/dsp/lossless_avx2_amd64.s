#include "textflag.h"

// VP8L lossless color transforms — AMD64 AVX2 assembly.
//
// AVX2 versions of addGreenToBlueAndRed and subtractGreen.
// Same algorithm as SSE2 but processes 8 pixels (32 bytes) per YMM iteration
// in the main loop body (single instruction stream, no unroll split).

// func addGreenToBlueAndRedAVX2(argb []uint32, numPixels int)
TEXT ·addGreenToBlueAndRedAVX2(SB), NOSPLIT, $0-32
	MOVQ argb_base+0(FP), SI    // SI = pointer to pixel data
	MOVQ numPixels+24(FP), CX   // CX = number of pixels

	CMPQ CX, $0
	JLE  addgreen_avx2_done

	// Build the 0x000000FF mask in Y5 using register operations.
	VPCMPEQD Y5, Y5, Y5         // Y5 = all 1s
	VPSRLD   $24, Y5, Y5        // Y5 = 0x000000FF x8

	// Process 8 pixels (32 bytes) per iteration with AVX2.
	MOVQ CX, DX
	SHRQ $3, DX                  // DX = numPixels / 8
	JZ   addgreen_avx2_tail4

addgreen_avx2_loop8:
	VMOVDQU (SI), Y0             // Y0 = 8 pixels
	VPSRLD  $8, Y0, Y1          // Y1 = pixels >> 8
	VPAND   Y5, Y1, Y1          // Y1 = green in byte 0
	VPSLLD  $16, Y1, Y2         // Y2 = green in byte 2 (red position)
	VPOR    Y2, Y1, Y1          // Y1 = [G, 0, G, 0] per pixel
	VPADDB  Y1, Y0, Y0          // Y0 = pixels + green
	VMOVDQU Y0, (SI)

	ADDQ $32, SI
	DECQ DX
	JNZ  addgreen_avx2_loop8

addgreen_avx2_tail4:
	// Check if 4 remaining pixels (SSE2 width).
	TESTQ $4, CX
	JZ    addgreen_avx2_tail

	VMOVDQU (SI), X0             // X0 = 4 pixels (128-bit)
	VPSRLD  $8, X0, X1
	VPAND   X5, X1, X1          // Use low 128 bits of Y5
	VPSLLD  $16, X1, X2
	VPOR    X2, X1, X1
	VPADDB  X1, X0, X0
	VMOVDQU X0, (SI)
	ADDQ    $16, SI

addgreen_avx2_tail:
	// Handle remaining 0-3 pixels one at a time.
	ANDQ $3, CX
	JZ   addgreen_avx2_done

addgreen_avx2_tail_loop:
	MOVL  (SI), AX
	MOVL  AX, BX
	SHRL  $8, BX
	ANDL  $0xFF, BX
	MOVL  BX, DX
	SHLL  $16, DX
	ORL   BX, DX
	MOVL  AX, R8
	ANDL  $0x00FF00FF, R8
	ADDL  DX, R8
	ANDL  $0x00FF00FF, R8
	ANDL  $0xFF00FF00, AX
	ORL   R8, AX
	MOVL  AX, (SI)
	ADDQ  $4, SI
	DECQ  CX
	JNZ   addgreen_avx2_tail_loop

addgreen_avx2_done:
	VZEROUPPER
	RET

// func subtractGreenAVX2(argb []uint32, numPixels int)
TEXT ·subtractGreenAVX2(SB), NOSPLIT, $0-32
	MOVQ argb_base+0(FP), SI    // SI = pointer to pixel data
	MOVQ numPixels+24(FP), CX   // CX = number of pixels

	CMPQ CX, $0
	JLE  subgreen_avx2_done

	// Build the 0x000000FF mask in Y5.
	VPCMPEQD Y5, Y5, Y5
	VPSRLD   $24, Y5, Y5        // Y5 = 0x000000FF x8

	// Process 8 pixels per iteration with AVX2.
	MOVQ CX, DX
	SHRQ $3, DX
	JZ   subgreen_avx2_tail4

subgreen_avx2_loop8:
	VMOVDQU (SI), Y0             // Y0 = 8 pixels
	VPSRLD  $8, Y0, Y1          // shift right 8
	VPAND   Y5, Y1, Y1          // isolate green
	VPSLLD  $16, Y1, Y2         // green to red position
	VPOR    Y2, Y1, Y1          // [G, 0, G, 0]
	VPSUBB  Y1, Y0, Y0          // pixels - green
	VMOVDQU Y0, (SI)

	ADDQ $32, SI
	DECQ DX
	JNZ  subgreen_avx2_loop8

subgreen_avx2_tail4:
	TESTQ $4, CX
	JZ    subgreen_avx2_tail

	VMOVDQU (SI), X0
	VPSRLD  $8, X0, X1
	VPAND   X5, X1, X1
	VPSLLD  $16, X1, X2
	VPOR    X2, X1, X1
	VPSUBB  X1, X0, X0
	VMOVDQU X0, (SI)
	ADDQ    $16, SI

subgreen_avx2_tail:
	ANDQ $3, CX
	JZ   subgreen_avx2_done

subgreen_avx2_tail_loop:
	MOVL  (SI), AX
	MOVL  AX, BX
	SHRL  $8, BX
	ANDL  $0xFF, BX
	MOVL  AX, R8
	SHRL  $16, R8
	ANDL  $0xFF, R8
	SUBL  BX, R8
	ANDL  $0xFF, R8
	MOVL  AX, R9
	ANDL  $0xFF, R9
	SUBL  BX, R9
	ANDL  $0xFF, R9
	ANDL  $0xFF00FF00, AX
	SHLL  $16, R8
	ORL   R8, AX
	ORL   R9, AX
	MOVL  AX, (SI)
	ADDQ  $4, SI
	DECQ  CX
	JNZ   subgreen_avx2_tail_loop

subgreen_avx2_done:
	VZEROUPPER
	RET
