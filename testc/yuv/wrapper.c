// wrapper.c - Non-inline wrappers around libwebp YUV inline functions.
// We include yuv.h (which has the inline functions) and yuv.c (which has
// WebPInitSamplers etc. needed by the compilation unit).

#include "wrapper.h"

// Prevent pulling in fancy upsampling code
#define FANCY_UPSAMPLING 0

// We need to include yuv.h for the inline VP8YUVToR/G/B and VP8RGBToY/U/V.
// yuv.h includes vp8_dec.h -> dsp.h -> common_dec.h etc.
// yuv.c includes yuv.h already plus defines WebPInitSamplers etc.
#include "src/dsp/yuv.c"

uint8_t c_yuv_to_r(int y, int v) {
    return (uint8_t)VP8YUVToR(y, v);
}

uint8_t c_yuv_to_g(int y, int u, int v) {
    return (uint8_t)VP8YUVToG(y, u, v);
}

uint8_t c_yuv_to_b(int y, int u) {
    return (uint8_t)VP8YUVToB(y, u);
}

uint8_t c_rgb_to_y(int r, int g, int b, int rounding) {
    return (uint8_t)VP8RGBToY(r, g, b, rounding);
}

uint8_t c_rgb_to_u(int r, int g, int b, int rounding) {
    return (uint8_t)VP8RGBToU(r, g, b, rounding);
}

uint8_t c_rgb_to_v(int r, int g, int b, int rounding) {
    return (uint8_t)VP8RGBToV(r, g, b, rounding);
}
