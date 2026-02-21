#include "textflag.h"

// func dequantCoeffsSSE2(in, out []int16, q, dcq int)
// Dequantizes 16 int16 coefficients: out[0] = in[0]*dcq, out[1..15] = in[1..15]*q.
// SSE2: 2 PMULLW instructions replace 16 scalar multiplications.
//
// Arguments (Plan9 ABI):
//   in_base+0(FP)   = pointer to in []int16
//   in_len+8(FP)    = slice length (unused)
//   in_cap+16(FP)   = slice capacity (unused)
//   out_base+24(FP) = pointer to out []int16
//   out_len+32(FP)  = slice length (unused)
//   out_cap+40(FP)  = slice capacity (unused)
//   q+48(FP)        = AC quantizer (int)
//   dcq+56(FP)      = DC quantizer (int)
TEXT ·dequantCoeffsSSE2(SB), NOSPLIT, $0-64
	MOVQ in_base+0(FP), SI     // SI = &in[0]
	MOVQ out_base+24(FP), DI   // DI = &out[0]
	MOVQ q+48(FP), AX          // AX = q (AC quantizer)
	MOVQ dcq+56(FP), BX        // BX = dcq (DC quantizer)

	// Broadcast q (int16) into all 8 lanes of X2.
	MOVD AX, X2
	PSHUFLW $0, X2, X2         // X2_low64 = [q, q, q, q]
	PSHUFD $0x44, X2, X2       // X2 = [q, q, q, q, q, q, q, q]

	// Load 16 coefficients (32 bytes).
	MOVOU (SI), X0              // X0 = in[0..7]
	MOVOU 16(SI), X1            // X1 = in[8..15]

	// Multiply all by q (low 16 bits of product).
	PMULLW X2, X0               // X0 = in[0..7] * q
	PMULLW X2, X1               // X1 = in[8..15] * q

	// Store results.
	MOVOU X0, (DI)              // out[0..7]
	MOVOU X1, 16(DI)            // out[8..15]

	// Fix DC: out[0] = in[0] * dcq (overwrite the AC-quantized value).
	MOVWLSX (SI), AX            // AX = sign-extend(in[0]) to int32
	IMULL BX, AX                // EAX = in[0] * dcq
	MOVW AX, (DI)               // out[0] = int16(result)

	RET

// func quantizeACSSE2(in, out, sharpen []int16, iQuant, bias int)
// Quantizes 16 int16 coefficients using QUANTDIV:
//   out[n] = sign(in[n]) * min((abs(in[n]) + sharpen[n]) * iQuant + bias) >> 17, 2047)
// Processes all 16 positions; caller must fix DC (position 0) separately.
// SSE2: uses PMULUDQ (raw-encoded) for uint32 widening multiply.
//
// Register allocation:
//   X4 = zero constant
//   X5 = [2047 x4] int32 constant
//   X6 = [iQuant x4] uint32 constant (PMULUDQ source, must be X0-X7)
//   X7 = [bias x2] uint64 constant
//   X8 = saved sign mask
//   X9 = saved v values (high half)
//   X0-X3 = computation temporaries (PMULUDQ operands, must be X0-X7)
//
// PMULUDQ is not in Go's assembler; we use raw LONG encoding:
//   PMULUDQ X6, X0 = 66 0F F4 C6 → LONG $0xC6F40F66
//   PMULUDQ X6, X1 = 66 0F F4 CE → LONG $0xCEF40F66
//   PMULUDQ X6, X2 = 66 0F F4 D6 → LONG $0xD6F40F66
//
// Arguments (Plan9 ABI):
//   in_base+0(FP)       = pointer to in []int16
//   in_len+8(FP)        = slice length (unused)
//   in_cap+16(FP)       = slice capacity (unused)
//   out_base+24(FP)     = pointer to out []int16
//   out_len+32(FP)      = slice length (unused)
//   out_cap+40(FP)      = slice capacity (unused)
//   sharpen_base+48(FP) = pointer to sharpen []int16
//   sharpen_len+56(FP)  = slice length (unused)
//   sharpen_cap+64(FP)  = slice capacity (unused)
//   iQuant+72(FP)       = inverse quantizer (Q17 fixed-point)
//   bias+80(FP)         = rounding bias
TEXT ·quantizeACSSE2(SB), NOSPLIT, $0-88
	MOVQ in_base+0(FP), SI        // SI = &in[0]
	MOVQ out_base+24(FP), DI      // DI = &out[0]
	MOVQ sharpen_base+48(FP), R8  // R8 = &sharpen[0]
	MOVQ iQuant+72(FP), AX        // AX = iQuant
	MOVQ bias+80(FP), BX          // BX = bias

	// Setup constants in X4-X7.
	PXOR X4, X4                    // X4 = zero

	MOVQ AX, X6
	PSHUFD $0x00, X6, X6          // X6 = [iQuant x4] uint32

	MOVQ BX, X7
	PSHUFD $0x44, X7, X7          // X7 = [bias, 0, bias, 0] = [bias x2] uint64

	MOVL $2047, AX
	MOVQ AX, X5
	PSHUFD $0x00, X5, X5          // X5 = [2047 x4] int32

	// ==== Process positions 0-7 ====
	MOVOU (SI), X0                 // X0 = in[0..7]
	MOVOU (R8), X1                 // X1 = sharpen[0..7]

	// Extract signs: -1 for negative, 0 for non-negative.
	MOVO X0, X8
	PSRAW $15, X8                  // X8 = sign[0..7] (saved for later)

	// Absolute value: abs(x) = (x ^ sign) - sign.
	PXOR X8, X0
	PSUBW X8, X0                   // X0 = |in[0..7]|

	// Add sharpen and clamp negative to 0.
	PADDW X1, X0                   // X0 = |in| + sharpen
	PMAXSW X4, X0                  // X0 = max(v, 0)

	MOVO X0, X9                    // X9 = save v[0..7] for high-half processing

	// --- Quantize low 4 (positions 0-3): widen to uint32, multiply, shift ---
	PUNPCKLWL X4, X0               // X0 = [v0, v1, v2, v3] as uint32

	MOVO X0, X1                    // X1 = copy for odd-position multiply
	LONG $0xC6F40F66               // PMULUDQ X6, X0 → X0 = [v0*iq (64), v2*iq (64)]
	PSHUFD $0xF5, X1, X1          // X1 = [v1, v1, v3, v3]
	LONG $0xCEF40F66               // PMULUDQ X6, X1 → X1 = [v1*iq (64), v3*iq (64)]

	PADDQ X7, X0                   // add bias
	PADDQ X7, X1

	PSRLQ $17, X0                  // >> 17
	PSRLQ $17, X1

	// Merge: X0 = [c0, 0, c2, 0], X1 = [c1, 0, c3, 0]
	// Shuffle X1 to [0, c1, 0, c3] then OR.
	PSHUFD $0xB1, X1, X1          // X1 = [0, c1, 0, c3]
	POR X1, X0                     // X0 = [c0, c1, c2, c3]

	// Clamp to 2047: min(coeff, 2047) using compare-and-select.
	MOVO X0, X1
	PCMPGTL X5, X1                 // X1 = -1 where coeff > 2047
	MOVO X5, X2
	PAND X1, X2                    // X2 = 2047 where > 2047
	PANDN X0, X1                   // X1 = coeff where <= 2047
	POR X2, X1                     // X1 = clamped [c0, c1, c2, c3]

	// --- Quantize high 4 (positions 4-7) ---
	MOVO X9, X0
	PUNPCKHWL X4, X0               // X0 = [v4, v5, v6, v7] as uint32

	MOVO X0, X2                    // X2 = copy for odd-position multiply
	LONG $0xC6F40F66               // PMULUDQ X6, X0
	PSHUFD $0xF5, X2, X2
	LONG $0xD6F40F66               // PMULUDQ X6, X2

	PADDQ X7, X0
	PADDQ X7, X2

	PSRLQ $17, X0
	PSRLQ $17, X2

	PSHUFD $0xB1, X2, X2          // [0, c5, 0, c7]
	POR X2, X0                     // X0 = [c4, c5, c6, c7]

	// Clamp to 2047.
	MOVO X0, X2
	PCMPGTL X5, X2
	MOVO X5, X3
	PAND X2, X3
	PANDN X0, X2
	POR X3, X2                     // X2 = clamped [c4, c5, c6, c7]

	// Pack int32 → int16 with signed saturation.
	PACKSSLW X2, X1                // X1 = [c0..c3, c4..c7] as int16

	// Apply sign: result = (coeff ^ sign) - sign.
	PXOR X8, X1
	PSUBW X8, X1

	// Store positions 0-7.
	MOVOU X1, (DI)                 // out[0..7]

	// ==== Process positions 8-15 ====
	MOVOU 16(SI), X0               // in[8..15]
	MOVOU 16(R8), X1               // sharpen[8..15]

	// Signs.
	MOVO X0, X8
	PSRAW $15, X8

	// Absolute value.
	PXOR X8, X0
	PSUBW X8, X0

	// Add sharpen, clamp.
	PADDW X1, X0
	PMAXSW X4, X0

	MOVO X0, X9                    // save for high half

	// --- Quantize low 4 (positions 8-11) ---
	PUNPCKLWL X4, X0

	MOVO X0, X1
	LONG $0xC6F40F66               // PMULUDQ X6, X0
	PSHUFD $0xF5, X1, X1
	LONG $0xCEF40F66               // PMULUDQ X6, X1

	PADDQ X7, X0
	PADDQ X7, X1

	PSRLQ $17, X0
	PSRLQ $17, X1

	PSHUFD $0xB1, X1, X1
	POR X1, X0

	MOVO X0, X1
	PCMPGTL X5, X1
	MOVO X5, X2
	PAND X1, X2
	PANDN X0, X1
	POR X2, X1

	// --- Quantize high 4 (positions 12-15) ---
	MOVO X9, X0
	PUNPCKHWL X4, X0

	MOVO X0, X2
	LONG $0xC6F40F66               // PMULUDQ X6, X0
	PSHUFD $0xF5, X2, X2
	LONG $0xD6F40F66               // PMULUDQ X6, X2

	PADDQ X7, X0
	PADDQ X7, X2

	PSRLQ $17, X0
	PSRLQ $17, X2

	PSHUFD $0xB1, X2, X2
	POR X2, X0

	MOVO X0, X2
	PCMPGTL X5, X2
	MOVO X5, X3
	PAND X2, X3
	PANDN X0, X2
	POR X3, X2

	PACKSSLW X2, X1

	PXOR X8, X1
	PSUBW X8, X1

	MOVOU X1, 16(DI)               // out[8..15]

	RET

// Zigzag lookup vectors for nzCount computation.
// Position 0 is set to -1 (0xFFFF) to exclude DC from the AC scan.
// kReverseZigzag = [0, 1, 5, 6, 2, 4, 7, 12, 3, 8, 11, 13, 9, 10, 14, 15]
DATA kZigzagLo<>+0x00(SB)/8, $0x000600050001FFFF
DATA kZigzagLo<>+0x08(SB)/8, $0x000C000700040002
GLOBL kZigzagLo<>(SB), (NOPTR+RODATA), $16

DATA kZigzagHi<>+0x00(SB)/8, $0x000D000B00080003
DATA kZigzagHi<>+0x08(SB)/8, $0x000F000E000A0009
GLOBL kZigzagHi<>(SB), (NOPTR+RODATA), $16

// func nzCountACSSE2(out []int16) int
// Scans positions 1-15 of out[] and returns the maximum kReverseZigzag value
// among non-zero coefficients. Returns -1 if all AC coefficients are zero.
// Position 0 (DC) is excluded from the scan.
//
// Strategy:
//   1. Compare each coefficient with zero -> zero_mask
//   2. Blend: zigzag_value where non-zero, -1 where zero
//   3. PMAXSW horizontal reduction -> max zigzag position
//
// Arguments:
//   out_base+0(FP) = pointer to out []int16
//   out_len+8(FP)  = slice length (unused)
//   out_cap+16(FP) = slice capacity (unused)
//   ret+24(FP)     = return value (int)
TEXT ·nzCountACSSE2(SB), NOSPLIT, $0-32
    MOVQ out_base+0(FP), SI

    // Load quantized coefficients.
    MOVOU (SI), X0               // out[0..7]
    MOVOU 16(SI), X1             // out[8..15]

    // Create zero masks: 0xFFFF where coefficient == 0.
    PXOR X4, X4
    MOVO X0, X2
    PCMPEQW X4, X2               // X2 = zero_mask_lo
    MOVO X1, X3
    PCMPEQW X4, X3               // X3 = zero_mask_hi

    // Load zigzag lookup vectors.
    MOVOU kZigzagLo<>(SB), X4    // [-1, 1, 5, 6, 2, 4, 7, 12]
    MOVOU kZigzagHi<>(SB), X5    // [3, 8, 11, 13, 9, 10, 14, 15]

    // Blend: zigzag where non-zero, -1 where zero.
    // result = (zigzag AND NOT(zero_mask)) OR zero_mask
    // Where coeff != 0: zero_mask = 0x0000 -> result = zigzag
    // Where coeff == 0: zero_mask = 0xFFFF -> result = 0xFFFF = -1
    MOVO X2, X6
    PANDN X4, X6                 // X6 = NOT(zero_mask) AND zigzag = zigzag at non-zero positions
    POR X2, X6                   // X6 |= zero_mask = -1 at zero positions

    MOVO X3, X7
    PANDN X5, X7                 // X7 = NOT(zero_mask_hi) AND zigzag_hi
    POR X3, X7                   // X7 |= zero_mask_hi

    // Horizontal PMAXSW reduction: 16 values -> 1.
    PMAXSW X7, X6                // 8 values (element-wise max of lo and hi)

    PSHUFD $0x4E, X6, X0         // swap high/low 64-bit halves
    PMAXSW X0, X6                // 4 values

    PSHUFLW $0x4E, X6, X0        // swap word pairs within low 64 bits
    PMAXSW X0, X6                // 2 values

    PSHUFLW $0xB1, X6, X0        // swap adjacent words
    PMAXSW X0, X6                // 1 value in word 0

    // Extract and sign-extend word 0 to int64.
    PEXTRW $0, X6, AX            // AX = zero-extended 16-bit value
    SHLQ $48, AX                 // shift to top for sign extension
    SARQ $48, AX                 // arithmetic shift right to sign-extend

    MOVQ AX, ret+24(FP)
    RET
