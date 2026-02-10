// stubs.c - Stub definitions for unresolved symbols pulled in by dec.c and enc.c.

#include <stdint.h>
#include <stddef.h>
#include <string.h>

#include "src/webp/types.h"
#include "src/dsp/cpu.h"
#include "src/dsp/dsp.h"

// VP8GetCPUInfo is used by WEBP_DSP_INIT_FUNC in both dec.c and enc.c.
VP8CPUInfo VP8GetCPUInfo = NULL;

// Include the clip tables implementation to satisfy VP8kclip1, VP8ksclip1, etc.
#include "src/dsp/dec_clip_tables.c"

// NEON init stubs (called on ARM64 from VP8DspInit / VP8EncDspInit).
void VP8DspInitNEON(void) {}
void VP8EncDspInitNEON(void) {}
