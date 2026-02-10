// all_libwebp.c - Builds the full libwebp encoder and decoder.
//
// Instead of one mega-include, we split into separate compilation units
// to avoid the massive number of static symbol, enum, and const conflicts.
// This file is intentionally empty - see dec_unit.c, enc_unit.c, dsp_unit.c,
// utils_unit.c, and sharpyuv_unit.c for the actual compilation units.
//
// This file only contains the wrapper functions that call the libwebp API.

#include "wrapper.h"
#include "src/webp/encode.h"
#include "src/webp/decode.h"
#include "src/webp/types.h"

int c_encode_lossy(const uint8_t* rgba, int width, int height, int stride,
                   float quality, uint8_t** output, size_t* output_size) {
    size_t sz = WebPEncodeRGBA(rgba, width, height, stride, quality, output);
    if (sz == 0) return 0;
    *output_size = sz;
    return 1;
}

int c_encode_lossless(const uint8_t* rgba, int width, int height, int stride,
                      uint8_t** output, size_t* output_size) {
    size_t sz = WebPEncodeLosslessRGBA(rgba, width, height, stride, output);
    if (sz == 0) return 0;
    *output_size = sz;
    return 1;
}

int c_decode_rgba(const uint8_t* data, size_t data_size,
                  int* width, int* height, uint8_t** output) {
    *output = WebPDecodeRGBA(data, data_size, width, height);
    return (*output != NULL) ? 1 : 0;
}

void c_free(uint8_t* ptr) {
    WebPFree(ptr);
}

int c_validate_webp(const uint8_t* data, size_t data_size,
                    int* width, int* height) {
    return WebPGetInfo(data, data_size, width, height);
}
