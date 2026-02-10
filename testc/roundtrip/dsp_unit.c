// dsp_unit.c - DSP compilation unit.
// All src/dsp/*.c files (C fallback only, no SIMD variants).
// Static symbol conflicts are resolved with #define/#undef.
//
// NOTE: cpu.c is NOT included here. VP8GetCPUInfo = NULL is defined in
// stubs.c so that only C fallback paths are taken by the DSP init functions.

#include "src/dsp/alpha_processing.c"
#include "src/dsp/cost.c"
#include "src/dsp/dec_clip_tables.c"
#include "src/dsp/dec.c"

// enc.c redefines clip_8b, clip1, TrueMotion (from dec.c / dec_clip_tables.c)
#define clip_8b enc_clip_8b
#define clip1 enc_clip1
#define TrueMotion enc_TrueMotion
#include "src/dsp/enc.c"
#undef clip_8b
#undef clip1
#undef TrueMotion

#include "src/dsp/filters.c"
#include "src/dsp/lossless.c"

// lossless_enc.c redefines ColorTransformDelta (from lossless.c)
#define ColorTransformDelta lossless_enc_ColorTransformDelta
#include "src/dsp/lossless_enc.c"
#undef ColorTransformDelta

#include "src/dsp/rescaler.c"
#include "src/dsp/ssim.c"
#include "src/dsp/upsampling.c"
#include "src/dsp/yuv.c"
