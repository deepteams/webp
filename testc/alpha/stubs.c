// Stubs for alpha_processing.c external dependencies.
#include <stddef.h>
#include <stdint.h>

// VP8GetCPUInfo - needed by the init function
typedef int (*VP8CPUInfo)(int feature);
VP8CPUInfo VP8GetCPUInfo = NULL;

// NEON/SSE/MIPS init stubs - referenced by WebPInitAlphaProcessing
void WebPInitAlphaProcessingNEON(void) {}
void WebPInitAlphaProcessingSSE2(void) {}
void WebPInitAlphaProcessingSSE41(void) {}
void WebPInitAlphaProcessingMIPSdspR2(void) {}

// WebPMalloc / WebPFree - may be referenced transitively
void* WebPMalloc(size_t size) { (void)size; return NULL; }
void WebPFree(void* ptr) { (void)ptr; }
