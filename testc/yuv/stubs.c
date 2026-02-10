// stubs.c - Stub definitions for unresolved symbols pulled in by yuv.c.

#include <stdint.h>
#include <stddef.h>

#include "src/webp/types.h"
#include "src/dsp/cpu.h"

// VP8GetCPUInfo is used by WEBP_DSP_INIT_FUNC.
VP8CPUInfo VP8GetCPUInfo = NULL;

// WebPExtractAlpha is called from ImportYUVAFromRGBA_C.
int (*WebPExtractAlpha)(const uint8_t* argb, int argb_stride,
                        int width, int height,
                        uint8_t* alpha, int alpha_stride) = NULL;

// NEON init stubs referenced by yuv.c via WEBP_DSP_INIT_FUNC on ARM.
void WebPInitConvertARGBToYUVNEON(void) {}
