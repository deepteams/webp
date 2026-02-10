// dec_wrapper.c - Non-inline wrappers around libwebp decoder transform functions.
// We include dec.c directly to access the static TransformOne_C etc.

#include "wrapper.h"

// Include dec.c to get the static functions.
// dec.c includes: common_dec.h, vp8i_dec.h, cpu.h, dsp.h, utils.h, types.h
#include "src/dsp/dec.c"

void c_transform_one(const int16_t* in, uint8_t* dst) {
    TransformOne_C(in, dst);
}

void c_transform_dc(const int16_t* in, uint8_t* dst) {
    TransformDC_C(in, dst);
}

void c_transform_ac3(const int16_t* in, uint8_t* dst) {
    TransformAC3_C(in, dst);
}

void c_transform_wht(const int16_t* in, int16_t* out) {
    TransformWHT_C(in, out);
}
