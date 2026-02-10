// enc_unit.c - Encoder compilation unit.
// All src/enc/*.c files compiled together in one unit.
// Static symbol conflicts are resolved with #define/#undef.

#include "src/enc/alpha_enc.c"

// analysis_enc.c defines clip and MAX_ALPHA
#include "src/enc/analysis_enc.c"

#include "src/enc/backward_references_cost_enc.c"
#include "src/enc/backward_references_enc.c"
#include "src/enc/config_enc.c"
#include "src/enc/cost_enc.c"
#include "src/enc/filter_enc.c"

// frame_enc.c defines GetPSNR
#include "src/enc/frame_enc.c"

#include "src/enc/histogram_enc.c"
#include "src/enc/iterator_enc.c"

// near_lossless_enc.c defines NearLossless - rename to avoid conflict with predictor_enc.c
#define NearLossless near_lossless_enc_NearLossless
#include "src/enc/near_lossless_enc.c"
#undef NearLossless

#include "src/enc/picture_csp_enc.c"
#include "src/enc/picture_enc.c"

// picture_psnr_enc.c redefines GetPSNR (from frame_enc.c)
#define GetPSNR picture_psnr_GetPSNR
#include "src/enc/picture_psnr_enc.c"
#undef GetPSNR

#include "src/enc/picture_rescale_enc.c"
#include "src/enc/picture_tools_enc.c"

// predictor_enc.c defines NearLossless (conflicts with near_lossless_enc.c)
#define NearLossless predictor_NearLossless
#include "src/enc/predictor_enc.c"
#undef NearLossless

// quant_enc.c redefines clip (from analysis_enc.c) and MAX_ALPHA
#define clip quant_enc_clip
#undef MAX_ALPHA
#include "src/enc/quant_enc.c"
#undef clip

#include "src/enc/syntax_enc.c"
#include "src/enc/token_enc.c"
#include "src/enc/tree_enc.c"
#include "src/enc/vp8l_enc.c"

// webp_enc.c redefines GetPSNR (from frame_enc.c)
#define GetPSNR webp_enc_GetPSNR
#include "src/enc/webp_enc.c"
#undef GetPSNR
