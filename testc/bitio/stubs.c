// stubs.c - Provide symbols needed by bit_writer_utils.c and bit_reader_utils.c
// without pulling in the full libwebp build.

#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>

#include "src/webp/types.h"
#include "src/dsp/cpu.h"

// VP8GetCPUInfo is used by WEBP_DSP_INIT_FUNC - provide a NULL stub.
VP8CPUInfo VP8GetCPUInfo = NULL;

// Minimal implementations of WebPSafeMalloc/WebPSafeCalloc/WebPSafeFree
// so we don't need to pull in the full utils.c (which depends on encode.h etc.)
void* WebPSafeMalloc(uint64_t nmemb, size_t size) {
    uint64_t total = nmemb * size;
    if (nmemb != 0 && total / nmemb != size) return NULL;
    if (total == 0) return NULL;
    return malloc((size_t)total);
}

void* WebPSafeCalloc(uint64_t nmemb, size_t size) {
    uint64_t total = nmemb * size;
    if (nmemb != 0 && total / nmemb != size) return NULL;
    if (total == 0) return NULL;
    return calloc((size_t)nmemb, size);
}

void WebPSafeFree(void* const ptr) {
    free(ptr);
}
