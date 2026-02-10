#ifndef ALPHA_WRAPPER_H
#define ALPHA_WRAPPER_H
#include <stdint.h>

void c_mult_argb_row(uint32_t* ptr, int width, int inverse);
int c_dispatch_alpha(const uint8_t* alpha, int alpha_stride,
                     int width, int height,
                     uint8_t* dst, int dst_stride);
int c_extract_alpha(const uint8_t* argb, int argb_stride,
                    int width, int height,
                    uint8_t* alpha, int alpha_stride);
int c_has_alpha_8b(const uint8_t* src, int length);
int c_has_alpha_32b(const uint8_t* src, int length);

#endif
