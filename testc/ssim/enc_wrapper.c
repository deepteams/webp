#include "wrapper.h"
#include "src/dsp/enc.c"

void init_enc_dsp(void) {
    VP8EncDspInit();
}

int c_sse_4x4(const uint8_t* a, const uint8_t* b) {
    return SSE4x4_C(a, b);
}

int c_sse_16x16(const uint8_t* a, const uint8_t* b) {
    return SSE16x16_C(a, b);
}

int c_tdisto_4x4(const uint8_t* a, const uint8_t* b, const uint16_t* w) {
    return Disto4x4_C(a, b, w);
}
