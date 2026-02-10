#include "wrapper.h"
#include "src/dsp/dec_clip_tables.c"

static uint8_t clip_8b_local(int v) {
    return (v & ~0xff) == 0 ? (uint8_t)v : (v < 0) ? 0u : 255u;
}

void init_clip_tables(void) {
    VP8InitClipTables();
}

int8_t c_ksclip1(int v) { return (int8_t)VP8ksclip1[v]; }
int8_t c_ksclip2(int v) { return (int8_t)VP8ksclip2[v]; }
uint8_t c_kclip1(int v) { return VP8kclip1[v]; }
uint8_t c_kabs0(int v) { return VP8kabs0[v]; }
uint8_t c_clip_8b(int v) { return clip_8b_local(v); }
