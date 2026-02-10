// stubs.c - Stub definitions for SIMD init functions and CPU info.
//
// VP8GetCPUInfo is set to NULL so only C fallback code paths are used.
// All SIMD init functions are stubbed as empty since they won't be called
// when VP8GetCPUInfo is NULL (the WEBP_DSP_INIT_FUNC macro skips SIMD
// dispatch when VP8GetCPUInfo is NULL).

#include <stdint.h>
#include <stddef.h>

#include "src/dsp/cpu.h"

// CPU info - set to NULL to disable all SIMD dispatch.
VP8CPUInfo VP8GetCPUInfo = NULL;

// --- Decoder DSP SIMD stubs ---
void VP8DspInitSSE2(void) {}
void VP8DspInitSSE41(void) {}
void VP8DspInitNEON(void) {}
void VP8DspInitMIPS32(void) {}
void VP8DspInitMIPSdspR2(void) {}
void VP8DspInitMSA(void) {}

// --- Encoder DSP SIMD stubs ---
void VP8EncDspInitSSE2(void) {}
void VP8EncDspInitSSE41(void) {}
void VP8EncDspInitNEON(void) {}
void VP8EncDspInitMIPS32(void) {}
void VP8EncDspInitMIPSdspR2(void) {}
void VP8EncDspInitMSA(void) {}

// --- Lossless DSP SIMD stubs ---
void VP8LDspInitSSE2(void) {}
void VP8LDspInitSSE41(void) {}
void VP8LDspInitAVX2(void) {}
void VP8LDspInitNEON(void) {}
void VP8LDspInitMIPS32(void) {}
void VP8LDspInitMIPSdspR2(void) {}
void VP8LDspInitMSA(void) {}

// --- Lossless encoder DSP SIMD stubs ---
void VP8LEncDspInitSSE2(void) {}
void VP8LEncDspInitSSE41(void) {}
void VP8LEncDspInitAVX2(void) {}
void VP8LEncDspInitNEON(void) {}
void VP8LEncDspInitMIPS32(void) {}
void VP8LEncDspInitMIPSdspR2(void) {}
void VP8LEncDspInitMSA(void) {}

// --- Cost DSP SIMD stubs ---
void VP8EncDspCostInitSSE2(void) {}
void VP8EncDspCostInitMIPS32(void) {}
void VP8EncDspCostInitMIPSdspR2(void) {}
void VP8EncDspCostInitNEON(void) {}

// --- Filters DSP SIMD stubs ---
void VP8FiltersInitSSE2(void) {}
void VP8FiltersInitNEON(void) {}
void VP8FiltersInitMIPSdspR2(void) {}
void VP8FiltersInitMSA(void) {}

// --- Rescaler DSP SIMD stubs ---
void WebPRescalerDspInitSSE2(void) {}
void WebPRescalerDspInitNEON(void) {}
void WebPRescalerDspInitMIPS32(void) {}
void WebPRescalerDspInitMIPSdspR2(void) {}
void WebPRescalerDspInitMSA(void) {}

// --- SSIM DSP SIMD stubs ---
void VP8SSIMDspInitSSE2(void) {}

// --- Upsampling DSP SIMD stubs ---
void WebPInitUpsamplersSSE2(void) {}
void WebPInitUpsamplersSSE41(void) {}
void WebPInitUpsamplersNEON(void) {}
void WebPInitUpsamplersMIPSdspR2(void) {}
void WebPInitUpsamplersMSA(void) {}

// --- YUV DSP SIMD stubs ---
void WebPInitConvertARGBToYUVSSE2(void) {}
void WebPInitConvertARGBToYUVSSE41(void) {}
void WebPInitConvertARGBToYUVNEON(void) {}

// --- Alpha processing SIMD stubs ---
void WebPInitAlphaProcessingSSE2(void) {}
void WebPInitAlphaProcessingSSE41(void) {}
void WebPInitAlphaProcessingNEON(void) {}
void WebPInitAlphaProcessingMIPSdspR2(void) {}

// --- SharpYUV SIMD stubs ---
void InitSharpYuvSSE2(void) {}
void InitSharpYuvNEON(void) {}
