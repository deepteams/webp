#include "wrapper.h"
#include "src/dsp/ssim.c"

void init_ssim_dsp(void) {
    VP8SSIMDspInit();
}

double c_ssim_from_stats(uint32_t w, uint32_t xm, uint32_t ym,
                         uint32_t xxm, uint32_t xym, uint32_t yym) {
    VP8DistoStats stats = {w, xm, ym, xxm, xym, yym};
    return VP8SSIMFromStats(&stats);
}
