#include "src/dsp/cpu.h"

// VP8GetCPUInfo is NULL so no SIMD dispatch happens.
VP8CPUInfo VP8GetCPUInfo = 0;

// Stub SIMD init functions that may be referenced on this platform.
#if defined(WEBP_HAVE_NEON)
void VP8EncDspCostInitNEON(void) {}
#endif
#if defined(WEBP_HAVE_SSE2)
void VP8EncDspCostInitSSE2(void) {}
#endif
#if defined(WEBP_USE_MIPS32)
void VP8EncDspCostInitMIPS32(void) {}
#endif
#if defined(WEBP_USE_MIPS_DSP_R2)
void VP8EncDspCostInitMIPSdspR2(void) {}
#endif
