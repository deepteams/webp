#include "textflag.h"

// CPU feature detection for AVX2 support.
//
// Checks three requirements for safe AVX2 usage:
//   1. CPUID leaf 1: OSXSAVE bit (ECX bit 27) — OS supports XSAVE/XGETBV
//   2. XGETBV ECX=0: XMM state (bit 1) and YMM state (bit 2) enabled by OS
//   3. CPUID leaf 7: AVX2 bit (EBX bit 5) — CPU supports AVX2 instructions
//
// Returns 1 if all checks pass, 0 otherwise.

// func cpuidAVX2Check() bool
TEXT ·cpuidAVX2Check(SB), NOSPLIT, $0-1
	// Step 1: CPUID leaf 1 — check OSXSAVE (ECX bit 27).
	MOVL $1, AX
	XORL CX, CX
	CPUID
	BTL  $27, CX           // test OSXSAVE bit
	JCC  no_avx2

	// Step 2: XGETBV ECX=0 — check OS enabled XMM (bit 1) and YMM (bit 2).
	XORL CX, CX
	// XGETBV: 0F 01 D0
	BYTE $0x0F; BYTE $0x01; BYTE $0xD0
	ANDL $0x06, AX         // mask bits 1 and 2
	CMPL AX, $0x06         // both must be set
	JNE  no_avx2

	// Step 3: CPUID leaf 7, subleaf 0 — check AVX2 (EBX bit 5).
	MOVL $7, AX
	XORL CX, CX
	CPUID
	BTL  $5, BX            // test AVX2 bit
	JCC  no_avx2

	MOVB $1, ret+0(FP)
	RET

no_avx2:
	MOVB $0, ret+0(FP)
	RET
