#include "wrapper.h"
#include "src/dsp/dec_clip_tables.c"
#include "src/dsp/dec.c"

void init_predict(void) {
    VP8DspInit();
}

// 4x4 prediction modes
void c_dc4(uint8_t* dst) { DC4_C(dst); }
void c_tm4(uint8_t* dst) { TM4_C(dst); }
void c_ve4(uint8_t* dst) { VE4_C(dst); }
void c_he4(uint8_t* dst) { HE4_C(dst); }
void c_rd4(uint8_t* dst) { RD4_C(dst); }
void c_vr4(uint8_t* dst) { VR4_C(dst); }
void c_ld4(uint8_t* dst) { LD4_C(dst); }
void c_vl4(uint8_t* dst) { VL4_C(dst); }
void c_hd4(uint8_t* dst) { HD4_C(dst); }
void c_hu4(uint8_t* dst) { HU4_C(dst); }

// 16x16 prediction modes
void c_dc16(uint8_t* dst) { DC16_C(dst); }
void c_tm16(uint8_t* dst) { TM16_C(dst); }
void c_ve16(uint8_t* dst) { VE16_C(dst); }
void c_he16(uint8_t* dst) { HE16_C(dst); }
void c_dc16_no_top(uint8_t* dst) { DC16NoTop_C(dst); }
void c_dc16_no_left(uint8_t* dst) { DC16NoLeft_C(dst); }
void c_dc16_no_top_left(uint8_t* dst) { DC16NoTopLeft_C(dst); }

// 8x8 chroma prediction modes
void c_dc8uv(uint8_t* dst) { DC8uv_C(dst); }
void c_tm8uv(uint8_t* dst) { TM8uv_C(dst); }
void c_ve8uv(uint8_t* dst) { VE8uv_C(dst); }
void c_he8uv(uint8_t* dst) { HE8uv_C(dst); }
void c_dc8uv_no_top(uint8_t* dst) { DC8uvNoTop_C(dst); }
void c_dc8uv_no_left(uint8_t* dst) { DC8uvNoLeft_C(dst); }
void c_dc8uv_no_top_left(uint8_t* dst) { DC8uvNoTopLeft_C(dst); }
