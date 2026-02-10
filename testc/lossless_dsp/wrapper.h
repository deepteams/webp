#ifndef LOSSLESS_DSP_WRAPPER_H
#define LOSSLESS_DSP_WRAPPER_H
#include <stdint.h>

// Predictors: C convention - top points to T, top[-1]=TL, top[1]=TR
uint32_t c_predictor(int mode, const uint32_t* left, const uint32_t* top);

// Color transforms
void c_add_green(const uint32_t* src, int num_pixels, uint32_t* dst);
void c_subtract_green(uint32_t* argb, int num_pixels);

typedef struct {
    uint8_t green_to_red;
    uint8_t green_to_blue;
    uint8_t red_to_blue;
} CMultipliers;

void c_transform_color(const CMultipliers* m, uint32_t* data, int num_pixels);
void c_transform_color_inverse(const CMultipliers* m, const uint32_t* src,
                                int num_pixels, uint32_t* dst);

#endif
