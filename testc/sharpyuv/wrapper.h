#ifndef WRAPPER_SHARPYUV_H
#define WRAPPER_SHARPYUV_H

#include <stdint.h>

// Thin wrapper around SharpYuvConvert for interleaved RGB input.
// Takes interleaved RGB, converts to separate Y, U, V planes.
int c_sharp_yuv_convert(const uint8_t* rgb, int width, int height, int rgb_stride,
                        uint8_t* y, int y_stride,
                        uint8_t* u, int u_stride,
                        uint8_t* v, int v_stride,
                        const int* rgb_to_y, const int* rgb_to_u, const int* rgb_to_v);

// Thin wrapper around SharpYuvComputeConversionMatrix.
int c_compute_conversion_matrix(float kr, float kb, int bit_depth, int range_min, int range_max,
                                int* rgb_to_y, int* rgb_to_u, int* rgb_to_v);

#endif // WRAPPER_SHARPYUV_H
