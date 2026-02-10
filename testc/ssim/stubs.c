// Stubs for unresolved symbols pulled in by enc.c and ssim.c.

#include <stdint.h>
#include <stddef.h>

#include "src/dsp/cpu.h"

// VP8GetCPUInfo - set to NULL so only C fallbacks are used.
VP8CPUInfo VP8GetCPUInfo = NULL;

// VP8DspInit is defined in dec.c - stub it since we don't need decoder DSP.
void VP8DspInit(void) {}

// SIMD init functions referenced by enc.c
void VP8EncDspInitSSE2(void) {}
void VP8EncDspInitSSE41(void) {}
void VP8EncDspInitNEON(void) {}
void VP8EncDspInitMIPS32(void) {}
void VP8EncDspInitMIPSdspR2(void) {}
void VP8EncDspInitMSA(void) {}

// SIMD init function referenced by ssim.c
void VP8SSIMDspInitSSE2(void) {}
