#ifndef COST_WRAPPER_H
#define COST_WRAPPER_H
#include <stdint.h>

uint16_t c_entropy_cost(int i);
uint16_t c_level_fixed_cost(int i);
uint8_t c_enc_bands(int i);

void init_cost_tables(void);
#endif
