#ifndef CLIPTABLES_WRAPPER_H
#define CLIPTABLES_WRAPPER_H
#include <stdint.h>

int8_t c_ksclip1(int v);
int8_t c_ksclip2(int v);
uint8_t c_kclip1(int v);
uint8_t c_kabs0(int v);
uint8_t c_clip_8b(int v);

void init_clip_tables(void);
#endif
