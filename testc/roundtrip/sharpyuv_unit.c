// sharpyuv_unit.c - SharpYUV compilation unit.
// All sharpyuv/*.c files compiled together in one unit.

// sharpyuv_gamma.c defines Shift, which conflicts with sharpyuv.c
#define Shift gamma_Shift
#include "sharpyuv/sharpyuv_gamma.c"
#undef Shift

#include "sharpyuv/sharpyuv_cpu.c"
#include "sharpyuv/sharpyuv_csp.c"

// sharpyuv_dsp.c defines clip, which conflicts with sharpyuv.c
#define clip sharpyuv_dsp_clip
#include "sharpyuv/sharpyuv_dsp.c"
#undef clip

#include "sharpyuv/sharpyuv.c"
