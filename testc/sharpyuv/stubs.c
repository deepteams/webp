// stubs.c - Stub definitions for unresolved symbols pulled in by sharpyuv.

#include <stdint.h>
#include <stddef.h>

#include "src/dsp/cpu.h"

// SharpYuvGetCPUInfo is used by the sharpyuv init code.
VP8CPUInfo SharpYuvGetCPUInfo = NULL;

// SSE2 init stub referenced by sharpyuv_dsp.c when WEBP_HAVE_SSE2 is defined.
void InitSharpYuvSSE2(void) {}

// NEON init stub referenced by sharpyuv_dsp.c when WEBP_HAVE_NEON is defined.
void InitSharpYuvNEON(void) {}
