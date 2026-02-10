#ifndef SSIM_WRAPPER_H
#define SSIM_WRAPPER_H
#include <stdint.h>

int c_sse_4x4(const uint8_t* a, const uint8_t* b);
int c_sse_16x16(const uint8_t* a, const uint8_t* b);
int c_tdisto_4x4(const uint8_t* a, const uint8_t* b, const uint16_t* w);
double c_ssim_from_stats(uint32_t w, uint32_t xm, uint32_t ym,
                         uint32_t xxm, uint32_t xym, uint32_t yym);

void init_enc_dsp(void);
void init_ssim_dsp(void);
#endif
