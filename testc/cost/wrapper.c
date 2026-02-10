#include "wrapper.h"
#include "src/dsp/cost.c"

void init_cost_tables(void) {
    VP8EncDspCostInit();
}

uint16_t c_entropy_cost(int i) { return VP8EntropyCost[i]; }
uint16_t c_level_fixed_cost(int i) { return VP8LevelFixedCosts[i]; }
uint8_t c_enc_bands(int i) { return VP8EncBands[i]; }
