#ifndef FILTER_WRAPPER_H
#define FILTER_WRAPPER_H
#include <stdint.h>

void init_filter(void);

void c_simple_vfilter16(uint8_t* p, int stride, int thresh);
void c_simple_hfilter16(uint8_t* p, int stride, int thresh);
void c_simple_vfilter16i(uint8_t* p, int stride, int thresh);
void c_simple_hfilter16i(uint8_t* p, int stride, int thresh);

void c_vfilter16(uint8_t* p, int stride, int thresh, int ithresh, int hev_thresh);
void c_hfilter16(uint8_t* p, int stride, int thresh, int ithresh, int hev_thresh);

void c_vfilter8(uint8_t* u, uint8_t* v, int stride, int thresh, int ithresh, int hev_thresh);
void c_hfilter8(uint8_t* u, uint8_t* v, int stride, int thresh, int ithresh, int hev_thresh);

#endif
