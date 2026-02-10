#include "wrapper.h"

// Include all sharpyuv source files to build as a single compilation unit.
#include "sharpyuv/sharpyuv_csp.c"
#include "sharpyuv/sharpyuv_dsp.c"
#include "sharpyuv/sharpyuv_gamma.c"

// Rename conflicting static functions to avoid redefinition errors when
// sharpyuv.c is included in the same compilation unit as sharpyuv_gamma.c
// and sharpyuv_dsp.c.
#define Shift Shift_sharpyuv_main
#define clip clip_sharpyuv_main
#include "sharpyuv/sharpyuv.c"
#undef clip
#undef Shift

int c_sharp_yuv_convert(const uint8_t* rgb, int width, int height, int rgb_stride,
                        uint8_t* y, int y_stride,
                        uint8_t* u, int u_stride,
                        uint8_t* v, int v_stride,
                        const int* rgb_to_y, const int* rgb_to_u, const int* rgb_to_v) {
    SharpYuvConversionMatrix matrix;
    int i;
    for (i = 0; i < 4; ++i) {
        matrix.rgb_to_y[i] = rgb_to_y[i];
        matrix.rgb_to_u[i] = rgb_to_u[i];
        matrix.rgb_to_v[i] = rgb_to_v[i];
    }
    // For interleaved RGB: r_ptr = &rgb[0], g_ptr = &rgb[1], b_ptr = &rgb[2], step = 3
    return SharpYuvConvert(
        &rgb[0], &rgb[1], &rgb[2],
        3,             // rgb_step (3 bytes between same channel of adjacent pixels)
        rgb_stride,    // rgb_stride
        8,             // rgb_bit_depth
        y, y_stride,
        u, u_stride,
        v, v_stride,
        8,             // yuv_bit_depth
        width, height,
        &matrix
    );
}

int c_compute_conversion_matrix(float kr, float kb, int bit_depth, int range_min, int range_max,
                                int* rgb_to_y, int* rgb_to_u, int* rgb_to_v) {
    SharpYuvColorSpace cs;
    SharpYuvConversionMatrix matrix;
    int i;
    (void)range_max;

    cs.kr = kr;
    cs.kb = kb;
    cs.bit_depth = bit_depth;
    // range_min == 0 means full range, otherwise limited
    cs.range = (range_min == 0) ? kSharpYuvRangeFull : kSharpYuvRangeLimited;

    SharpYuvComputeConversionMatrix(&cs, &matrix);

    for (i = 0; i < 4; ++i) {
        rgb_to_y[i] = matrix.rgb_to_y[i];
        rgb_to_u[i] = matrix.rgb_to_u[i];
        rgb_to_v[i] = matrix.rgb_to_v[i];
    }
    return 1;
}
