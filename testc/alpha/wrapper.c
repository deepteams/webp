#include "wrapper.h"
#include "src/dsp/alpha_processing.c"

void c_mult_argb_row(uint32_t* ptr, int width, int inverse) {
    WebPMultARGBRow_C(ptr, width, inverse);
}

int c_dispatch_alpha(const uint8_t* alpha, int alpha_stride,
                     int width, int height,
                     uint8_t* dst, int dst_stride) {
    return DispatchAlpha_C(alpha, alpha_stride, width, height, dst, dst_stride);
}

int c_extract_alpha(const uint8_t* argb, int argb_stride,
                    int width, int height,
                    uint8_t* alpha, int alpha_stride) {
    return ExtractAlpha_C(argb, argb_stride, width, height, alpha, alpha_stride);
}

int c_has_alpha_8b(const uint8_t* src, int length) {
    return HasAlpha8b_C(src, length);
}

int c_has_alpha_32b(const uint8_t* src, int length) {
    return HasAlpha32b_C(src, length);
}
