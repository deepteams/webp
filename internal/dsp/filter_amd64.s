#include "textflag.h"

// VP8 Simple Vertical Loop Filter — AMD64 SSE2 assembly.
//
// Applies the 2-tap simple loop filter to a 16-wide vertical edge.
// For each of the 16 columns, computes:
//   needsFilter: 4*|p0-q0| + |p1-q1| <= 2*thresh+1
//   doFilter2:   a = 3*(q0-p0) + sclip1(p1-q1)
//                a1 = sclip2((a+4)>>3), a2 = sclip2((a+3)>>3)
//                p0 += a2, q0 -= a1
//
// All 16 pixels are processed in parallel using SSE2 int16 arithmetic.
// Processing is done in two 8-pixel halves for int16 precision.
//
// The needsFilter check uses int16 to correctly handle thresh2 > 255
// (which occurs for high VP8 filter levels). The comparison:
//   sum = 4*|p0-q0| + |p1-q1| (max 1275, fits uint16)
//   PSUBUSW(sum, thresh2) == 0  ⟺  sum <= thresh2

// Int16 broadcast constants for clipping operations.
// sclip1 clips to [-128, 127]
DATA kSclip1Max_w<>+0x00(SB)/4, $0x007F007F
DATA kSclip1Max_w<>+0x04(SB)/4, $0x007F007F
DATA kSclip1Max_w<>+0x08(SB)/4, $0x007F007F
DATA kSclip1Max_w<>+0x0c(SB)/4, $0x007F007F
GLOBL kSclip1Max_w<>(SB), RODATA|NOPTR, $16

DATA kSclip1Min_w<>+0x00(SB)/4, $0xFF80FF80
DATA kSclip1Min_w<>+0x04(SB)/4, $0xFF80FF80
DATA kSclip1Min_w<>+0x08(SB)/4, $0xFF80FF80
DATA kSclip1Min_w<>+0x0c(SB)/4, $0xFF80FF80
GLOBL kSclip1Min_w<>(SB), RODATA|NOPTR, $16

// sclip2 clips to [-16, 15]
DATA kSclip2Max_w<>+0x00(SB)/4, $0x000F000F
DATA kSclip2Max_w<>+0x04(SB)/4, $0x000F000F
DATA kSclip2Max_w<>+0x08(SB)/4, $0x000F000F
DATA kSclip2Max_w<>+0x0c(SB)/4, $0x000F000F
GLOBL kSclip2Max_w<>(SB), RODATA|NOPTR, $16

DATA kSclip2Min_w<>+0x00(SB)/4, $0xFFF0FFF0
DATA kSclip2Min_w<>+0x04(SB)/4, $0xFFF0FFF0
DATA kSclip2Min_w<>+0x08(SB)/4, $0xFFF0FFF0
DATA kSclip2Min_w<>+0x0c(SB)/4, $0xFFF0FFF0
GLOBL kSclip2Min_w<>(SB), RODATA|NOPTR, $16

// Bias constants: 4 and 3 as int16
DATA kBias4_w<>+0x00(SB)/4, $0x00040004
DATA kBias4_w<>+0x04(SB)/4, $0x00040004
DATA kBias4_w<>+0x08(SB)/4, $0x00040004
DATA kBias4_w<>+0x0c(SB)/4, $0x00040004
GLOBL kBias4_w<>(SB), RODATA|NOPTR, $16

DATA kBias3_w<>+0x00(SB)/4, $0x00030003
DATA kBias3_w<>+0x04(SB)/4, $0x00030003
DATA kBias3_w<>+0x08(SB)/4, $0x00030003
DATA kBias3_w<>+0x0c(SB)/4, $0x00030003
GLOBL kBias3_w<>(SB), RODATA|NOPTR, $16

// func simpleVFilter16SSE2(p []byte, base, stride, thresh int)
//
// Arguments (Plan9 ABI):
//   p_base+0(FP)   = pointer to buffer
//   p_len+8(FP)    = slice length (unused)
//   p_cap+16(FP)   = slice capacity (unused)
//   base+24(FP)    = offset of the edge row (q0)
//   stride+32(FP)  = row stride
//   thresh+40(FP)  = filter threshold
//
// Register allocation:
//   SI  = buffer pointer          R8  = q0_addr (base)
//   R10 = q1_addr (base+stride)   R11 = p0_addr (base-stride)
//   R12 = p1_addr (base-2*stride)
//   X0  = p1 bytes (preserved)    X1  = p0 bytes (preserved)
//   X2  = q0 bytes (preserved)    X3  = q1 bytes (preserved)
//   X14 = thresh2 broadcast       X15 = zero
//   X12 = new_p0_lo (saved between halves)
//   X13 = new_q0_lo (saved between halves)
//   X4-X11 = temporaries
TEXT ·simpleVFilter16SSE2(SB), NOSPLIT, $0-48
	MOVQ p_base+0(FP), SI          // SI = buffer pointer
	MOVQ base+24(FP), AX           // AX = base offset
	MOVQ stride+32(FP), DX         // DX = stride
	MOVQ thresh+40(FP), CX         // CX = thresh

	// Compute row addresses.
	LEAQ (SI)(AX*1), R8            // R8 = q0_addr = buf + base
	LEAQ (R8)(DX*1), R10           // R10 = q1_addr = base + stride
	MOVQ R8, R11
	SUBQ DX, R11                   // R11 = p0_addr = base - stride
	MOVQ DX, R12
	SHLQ $1, R12                   // R12 = 2*stride
	NEGQ R12
	LEAQ (R8)(R12*1), R12          // R12 = p1_addr = base - 2*stride

	// Broadcast thresh2 = 2*thresh+1 to all 8 int16 positions.
	ADDQ CX, CX                    // 2*thresh
	INCQ CX                        // 2*thresh + 1
	MOVD CX, X14
	PSHUFLW $0, X14, X14           // replicate word to low qword
	PSHUFD $0x00, X14, X14         // replicate dword 0 to all dwords

	PXOR X15, X15                  // X15 = zero

	// Load 4 rows of 16 bytes each.
	MOVOU (R12), X0                // X0 = p1 row
	MOVOU (R11), X1                // X1 = p0 row
	MOVOU (R8), X2                 // X2 = q0 row
	MOVOU (R10), X3                // X3 = q1 row

	// ========== LOW HALF (pixels 0-7) ==========

	// Zero-extend low 8 bytes of each row to int16.
	MOVO X0, X4
	PUNPCKLBW X15, X4              // X4 = p1_lo
	MOVO X1, X5
	PUNPCKLBW X15, X5              // X5 = p0_lo
	MOVO X2, X6
	PUNPCKLBW X15, X6              // X6 = q0_lo
	MOVO X3, X7
	PUNPCKLBW X15, X7              // X7 = q1_lo

	// --- needsFilter: 4*|p0-q0| + |p1-q1| <= thresh2 ---
	MOVO  X5, X8
	PSUBW X6, X8                   // X8 = p0-q0
	MOVO  X6, X9
	PSUBW X5, X9                   // X9 = q0-p0
	PMAXSW X9, X8                  // X8 = |p0-q0|

	MOVO  X4, X9
	PSUBW X7, X9                   // X9 = p1-q1 (SAVED for doFilter2)
	MOVO  X7, X10
	PSUBW X4, X10                  // X10 = q1-p1
	PMAXSW X9, X10                 // X10 = |p1-q1|

	PSLLW  $2, X8                  // X8 = 4*|p0-q0|
	PADDW  X10, X8                 // X8 = 4*|p0-q0| + |p1-q1|
	PSUBUSW X14, X8                // X8 = max(sum - thresh2, 0)
	PCMPEQW X15, X8                // X8 = mask_lo (0xFFFF where filter needed)

	// --- doFilter2 ---
	// sclip1(p1-q1): clip X9 to [-128, 127]
	MOVOU kSclip1Max_w<>(SB), X10
	PMINSW X10, X9
	MOVOU kSclip1Min_w<>(SB), X10
	PMAXSW X10, X9                 // X9 = sclip1(p1-q1)

	// a = 3*(q0-p0) + sclip1(p1-q1)
	MOVO  X6, X10                  // q0_lo
	PSUBW X5, X10                  // X10 = q0-p0
	MOVO  X10, X11                 // save q0-p0
	PADDW X10, X10                 // 2*(q0-p0)
	PADDW X11, X10                 // 3*(q0-p0)
	PADDW X9, X10                  // X10 = a

	// a1 = sclip2((a+4) >> 3)
	MOVO  X10, X11                 // save a
	MOVOU kBias4_w<>(SB), X9
	PADDW X9, X10                  // a + 4
	PSRAW $3, X10                  // (a+4) >> 3
	MOVOU kSclip2Max_w<>(SB), X9
	PMINSW X9, X10
	MOVOU kSclip2Min_w<>(SB), X9
	PMAXSW X9, X10                 // X10 = a1

	// a2 = sclip2((a+3) >> 3)
	MOVOU kBias3_w<>(SB), X9
	PADDW X9, X11                  // a + 3
	PSRAW $3, X11                  // (a+3) >> 3
	MOVOU kSclip2Max_w<>(SB), X9
	PMINSW X9, X11
	MOVOU kSclip2Min_w<>(SB), X9
	PMAXSW X9, X11                 // X11 = a2

	// Apply mask: zero adjustments where filter not needed.
	PAND X8, X11                   // a2 masked
	PAND X8, X10                   // a1 masked

	// Compute filtered values.
	PADDW X11, X5                  // new_p0_lo = p0_lo + a2
	PSUBW X10, X6                  // new_q0_lo = q0_lo - a1

	// Save low half results.
	MOVO X5, X12                   // X12 = new_p0_lo
	MOVO X6, X13                   // X13 = new_q0_lo

	// ========== HIGH HALF (pixels 8-15) ==========

	// Zero-extend high 8 bytes of each row to int16.
	MOVO X0, X4
	PUNPCKHBW X15, X4              // X4 = p1_hi
	MOVO X1, X5
	PUNPCKHBW X15, X5              // X5 = p0_hi
	MOVO X2, X6
	PUNPCKHBW X15, X6              // X6 = q0_hi
	MOVO X3, X7
	PUNPCKHBW X15, X7              // X7 = q1_hi

	// --- needsFilter ---
	MOVO  X5, X8
	PSUBW X6, X8                   // p0-q0
	MOVO  X6, X9
	PSUBW X5, X9                   // q0-p0
	PMAXSW X9, X8                  // |p0-q0|

	MOVO  X4, X9
	PSUBW X7, X9                   // p1-q1 (saved for doFilter2)
	MOVO  X7, X10
	PSUBW X4, X10                  // q1-p1
	PMAXSW X9, X10                 // |p1-q1|

	PSLLW  $2, X8
	PADDW  X10, X8
	PSUBUSW X14, X8
	PCMPEQW X15, X8                // X8 = mask_hi

	// --- doFilter2 ---
	MOVOU kSclip1Max_w<>(SB), X10
	PMINSW X10, X9
	MOVOU kSclip1Min_w<>(SB), X10
	PMAXSW X10, X9                 // sclip1(p1-q1)

	MOVO  X6, X10                  // q0_hi
	PSUBW X5, X10                  // q0-p0
	MOVO  X10, X11
	PADDW X10, X10                 // 2*(q0-p0)
	PADDW X11, X10                 // 3*(q0-p0)
	PADDW X9, X10                  // a

	MOVO  X10, X4                  // save a (reuse X4, p1_hi no longer needed)
	MOVOU kBias4_w<>(SB), X9
	PADDW X9, X10                  // a + 4
	PSRAW $3, X10
	MOVOU kSclip2Max_w<>(SB), X9
	PMINSW X9, X10
	MOVOU kSclip2Min_w<>(SB), X9
	PMAXSW X9, X10                 // a1

	MOVOU kBias3_w<>(SB), X9
	PADDW X9, X4                   // a + 3
	PSRAW $3, X4
	MOVOU kSclip2Max_w<>(SB), X9
	PMINSW X9, X4
	MOVOU kSclip2Min_w<>(SB), X9
	PMAXSW X9, X4                  // a2

	PAND X8, X4                    // a2 masked
	PAND X8, X10                   // a1 masked

	PADDW X4, X5                   // new_p0_hi = p0_hi + a2
	PSUBW X10, X6                  // new_q0_hi = q0_hi - a1

	// Pack both halves: int16 -> uint8 with unsigned saturation [0, 255].
	PACKUSWB X5, X12               // X12 = [p0_lo_bytes, p0_hi_bytes]
	PACKUSWB X6, X13               // X13 = [q0_lo_bytes, q0_hi_bytes]

	// Store filtered p0 and q0 rows.
	MOVOU X12, (R11)               // p0 row at base - stride
	MOVOU X13, (R8)                // q0 row at base

	RET
