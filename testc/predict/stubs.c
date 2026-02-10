// Stubs for unresolved symbols from dec.c
#include <stdint.h>
#include <stddef.h>

// VP8GetCPUInfo is used in VP8DspInit to select SIMD paths.
// We set it to NULL so only C fallbacks are used.
typedef int (*VP8CPUInfo)(int feature);
VP8CPUInfo VP8GetCPUInfo = NULL;

// SIMD init functions are conditionally called but never defined
// when compiling for generic C. Stub them out.
void VP8DspInitSSE2(void) {}
void VP8DspInitSSE41(void) {}
void VP8DspInitNEON(void) {}
void VP8DspInitMIPS32(void) {}
void VP8DspInitMIPSdspR2(void) {}
void VP8DspInitMSA(void) {}

// IsValidColorspace from common_dec.h
int IsValidColorspace(int webp_csp_mode) {
    (void)webp_csp_mode;
    return 1;
}
