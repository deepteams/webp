#include "wrapper.h"
#include "src/dsp/dec_clip_tables.c"
#include "src/dsp/dec.c"

void init_filter(void) {
    VP8DspInit();
}

void c_simple_vfilter16(uint8_t* p, int stride, int thresh) {
    SimpleVFilter16_C(p, stride, thresh);
}

void c_simple_hfilter16(uint8_t* p, int stride, int thresh) {
    SimpleHFilter16_C(p, stride, thresh);
}

void c_simple_vfilter16i(uint8_t* p, int stride, int thresh) {
    SimpleVFilter16i_C(p, stride, thresh);
}

void c_simple_hfilter16i(uint8_t* p, int stride, int thresh) {
    SimpleHFilter16i_C(p, stride, thresh);
}

void c_vfilter16(uint8_t* p, int stride, int thresh, int ithresh, int hev_thresh) {
    VFilter16_C(p, stride, thresh, ithresh, hev_thresh);
}

void c_hfilter16(uint8_t* p, int stride, int thresh, int ithresh, int hev_thresh) {
    HFilter16_C(p, stride, thresh, ithresh, hev_thresh);
}

void c_vfilter8(uint8_t* u, uint8_t* v, int stride, int thresh, int ithresh, int hev_thresh) {
    VFilter8_C(u, v, stride, thresh, ithresh, hev_thresh);
}

void c_hfilter8(uint8_t* u, uint8_t* v, int stride, int thresh, int ithresh, int hev_thresh) {
    HFilter8_C(u, v, stride, thresh, ithresh, hev_thresh);
}
