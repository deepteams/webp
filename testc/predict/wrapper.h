#ifndef PREDICT_WRAPPER_H
#define PREDICT_WRAPPER_H
#include <stdint.h>

void init_predict(void);

// 4x4 prediction modes
void c_dc4(uint8_t* dst);
void c_tm4(uint8_t* dst);
void c_ve4(uint8_t* dst);
void c_he4(uint8_t* dst);
void c_rd4(uint8_t* dst);
void c_vr4(uint8_t* dst);
void c_ld4(uint8_t* dst);
void c_vl4(uint8_t* dst);
void c_hd4(uint8_t* dst);
void c_hu4(uint8_t* dst);

// 16x16 prediction modes
void c_dc16(uint8_t* dst);
void c_tm16(uint8_t* dst);
void c_ve16(uint8_t* dst);
void c_he16(uint8_t* dst);
void c_dc16_no_top(uint8_t* dst);
void c_dc16_no_left(uint8_t* dst);
void c_dc16_no_top_left(uint8_t* dst);

// 8x8 chroma prediction modes
void c_dc8uv(uint8_t* dst);
void c_tm8uv(uint8_t* dst);
void c_ve8uv(uint8_t* dst);
void c_he8uv(uint8_t* dst);
void c_dc8uv_no_top(uint8_t* dst);
void c_dc8uv_no_left(uint8_t* dst);
void c_dc8uv_no_top_left(uint8_t* dst);

#endif
