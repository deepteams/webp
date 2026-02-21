#include "textflag.h"

// VP8 Simple Vertical Loop Filter — AMD64 AVX2 assembly.
//
// Same algorithm as SSE2 but processes all 16 pixels in a single YMM pass
// (16 int16 in one YMM register), eliminating the two-half split.

// Clipping constants (file-scoped duplicates for AVX2 file).
DATA kSclip1MaxAVX<>+0x00(SB)/8, $0x007F007F007F007F
DATA kSclip1MaxAVX<>+0x08(SB)/8, $0x007F007F007F007F
GLOBL kSclip1MaxAVX<>(SB), RODATA|NOPTR, $16

DATA kSclip1MinAVX<>+0x00(SB)/8, $0xFF80FF80FF80FF80
DATA kSclip1MinAVX<>+0x08(SB)/8, $0xFF80FF80FF80FF80
GLOBL kSclip1MinAVX<>(SB), RODATA|NOPTR, $16

DATA kSclip2MaxAVX<>+0x00(SB)/8, $0x000F000F000F000F
DATA kSclip2MaxAVX<>+0x08(SB)/8, $0x000F000F000F000F
GLOBL kSclip2MaxAVX<>(SB), RODATA|NOPTR, $16

DATA kSclip2MinAVX<>+0x00(SB)/8, $0xFFF0FFF0FFF0FFF0
DATA kSclip2MinAVX<>+0x08(SB)/8, $0xFFF0FFF0FFF0FFF0
GLOBL kSclip2MinAVX<>(SB), RODATA|NOPTR, $16

DATA kBias4AVX<>+0x00(SB)/8, $0x0004000400040004
DATA kBias4AVX<>+0x08(SB)/8, $0x0004000400040004
GLOBL kBias4AVX<>(SB), RODATA|NOPTR, $16

DATA kBias3AVX<>+0x00(SB)/8, $0x0003000300030003
DATA kBias3AVX<>+0x08(SB)/8, $0x0003000300030003
GLOBL kBias3AVX<>(SB), RODATA|NOPTR, $16

// func simpleVFilter16AVX2(p []byte, base, stride, thresh int)
TEXT ·simpleVFilter16AVX2(SB), NOSPLIT, $0-48
	MOVQ p_base+0(FP), SI
	MOVQ base+24(FP), AX
	MOVQ stride+32(FP), DX
	MOVQ thresh+40(FP), CX

	// Compute row addresses.
	LEAQ (SI)(AX*1), R8            // R8 = q0_addr
	LEAQ (R8)(DX*1), R10           // R10 = q1_addr
	MOVQ R8, R11
	SUBQ DX, R11                   // R11 = p0_addr
	MOVQ DX, R12
	SHLQ $1, R12
	NEGQ R12
	LEAQ (R8)(R12*1), R12          // R12 = p1_addr

	// Broadcast thresh2 = 2*thresh+1 to 16 int16 positions.
	ADDQ CX, CX
	INCQ CX
	MOVQ CX, X14
	VPBROADCASTW X14, Y14          // Y14 = thresh2 broadcast

	VPXOR Y15, Y15, Y15            // Y15 = zero

	// Load 4 rows of 16 bytes, zero-extend to 16 int16 in YMM.
	// p1
	VMOVDQU (R12), X0
	VPUNPCKHBW X15, X0, X8
	VPUNPCKLBW X15, X0, X0
	VINSERTI128 $1, X8, Y0, Y0     // Y0 = p1

	// p0
	VMOVDQU (R11), X1
	VPUNPCKHBW X15, X1, X8
	VPUNPCKLBW X15, X1, X1
	VINSERTI128 $1, X8, Y1, Y1     // Y1 = p0

	// q0
	VMOVDQU (R8), X2
	VPUNPCKHBW X15, X2, X8
	VPUNPCKLBW X15, X2, X2
	VINSERTI128 $1, X8, Y2, Y2     // Y2 = q0

	// q1
	VMOVDQU (R10), X3
	VPUNPCKHBW X15, X3, X8
	VPUNPCKLBW X15, X3, X3
	VINSERTI128 $1, X8, Y3, Y3     // Y3 = q1

	// --- needsFilter: 4*|p0-q0| + |p1-q1| <= thresh2 ---
	VPSUBW Y2, Y1, Y4              // p0-q0
	VPSUBW Y1, Y2, Y5              // q0-p0
	VPMAXSW Y5, Y4, Y4             // |p0-q0|

	VPSUBW Y3, Y0, Y5              // p1-q1 (SAVED for doFilter2)
	VPSUBW Y0, Y3, Y6              // q1-p1
	VPMAXSW Y5, Y6, Y6             // |p1-q1|

	VPSLLW  $2, Y4, Y4             // 4*|p0-q0|
	VPADDW  Y6, Y4, Y4             // sum
	VPSUBUSW Y14, Y4, Y4           // saturated sub
	VPCMPEQW Y15, Y4, Y4           // Y4 = mask

	// --- doFilter2 ---
	// sclip1(p1-q1)
	VBROADCASTI128 kSclip1MaxAVX<>(SB), Y6
	VPMINSW Y6, Y5, Y5
	VBROADCASTI128 kSclip1MinAVX<>(SB), Y6
	VPMAXSW Y6, Y5, Y5             // Y5 = sclip1(p1-q1)

	// a = 3*(q0-p0) + sclip1(p1-q1)
	VPSUBW  Y1, Y2, Y6             // q0-p0
	VMOVDQA Y6, Y7
	VPADDW  Y6, Y6, Y6             // 2*(q0-p0)
	VPADDW  Y7, Y6, Y6             // 3*(q0-p0)
	VPADDW  Y5, Y6, Y6             // Y6 = a

	// a1 = sclip2((a+4) >> 3)
	VMOVDQA Y6, Y7                 // save a
	VBROADCASTI128 kBias4AVX<>(SB), Y5
	VPADDW  Y5, Y6, Y6
	VPSRAW  $3, Y6, Y6
	VBROADCASTI128 kSclip2MaxAVX<>(SB), Y5
	VPMINSW Y5, Y6, Y6
	VBROADCASTI128 kSclip2MinAVX<>(SB), Y5
	VPMAXSW Y5, Y6, Y6             // Y6 = a1

	// a2 = sclip2((a+3) >> 3)
	VBROADCASTI128 kBias3AVX<>(SB), Y5
	VPADDW  Y5, Y7, Y7
	VPSRAW  $3, Y7, Y7
	VBROADCASTI128 kSclip2MaxAVX<>(SB), Y5
	VPMINSW Y5, Y7, Y7
	VBROADCASTI128 kSclip2MinAVX<>(SB), Y5
	VPMAXSW Y5, Y7, Y7             // Y7 = a2

	// Apply mask.
	VPAND Y4, Y7, Y7               // a2 masked
	VPAND Y4, Y6, Y6               // a1 masked

	// Compute filtered values.
	VPADDW Y7, Y1, Y1              // new_p0 = p0 + a2
	VPSUBW Y6, Y2, Y2              // new_q0 = q0 - a1

	// Pack 16 int16 → 16 uint8 with unsigned saturation.
	// Extract high lane, VPACKUSWB with low lane.
	VEXTRACTI128 $1, Y1, X5        // X5 = new_p0[8..15] as int16
	VPACKUSWB X5, X1, X1           // X1 = [p0_0..p0_7, p0_8..p0_15] bytes

	VEXTRACTI128 $1, Y2, X5        // X5 = new_q0[8..15] as int16
	VPACKUSWB X5, X2, X2           // X2 = [q0_0..q0_7, q0_8..q0_15] bytes

	// Store filtered rows.
	VMOVDQU X1, (R11)
	VMOVDQU X2, (R8)

	VZEROUPPER
	RET
