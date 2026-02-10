#ifndef WRAPPER_YUV_H
#define WRAPPER_YUV_H

#include <stdint.h>

// YUV -> RGB
uint8_t c_yuv_to_r(int y, int v);
uint8_t c_yuv_to_g(int y, int u, int v);
uint8_t c_yuv_to_b(int y, int u);

// RGB -> YUV
uint8_t c_rgb_to_y(int r, int g, int b, int rounding);
uint8_t c_rgb_to_u(int r, int g, int b, int rounding);
uint8_t c_rgb_to_v(int r, int g, int b, int rounding);

#endif // WRAPPER_YUV_H
