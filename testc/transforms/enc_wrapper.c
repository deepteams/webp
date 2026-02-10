// enc_wrapper.c - Non-inline wrappers around libwebp encoder transform functions.
// We include enc.c directly to access the static FTransform_C etc.
// CRITICAL: dec.c and enc.c both define `static clip_8b`, so they must be
// in separate .c files to avoid redefinition.

#include "wrapper.h"

// Include enc.c to get the static functions.
// enc.c includes: cpu.h, dsp.h, vp8i_enc.h, utils.h, types.h
#include "src/dsp/enc.c"

void c_ftransform(const uint8_t* src, const uint8_t* ref, int16_t* out) {
    FTransform_C(src, ref, out);
}

void c_ftransform_wht(const int16_t* in, int16_t* out) {
    FTransformWHT_C(in, out);
}

void c_itransform(const uint8_t* ref, const int16_t* in, uint8_t* dst, int do_two) {
    ITransform_C(ref, in, dst, do_two);
}
