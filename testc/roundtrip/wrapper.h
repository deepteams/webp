#ifndef ROUNDTRIP_WRAPPER_H
#define ROUNDTRIP_WRAPPER_H

#include <stdint.h>
#include <stddef.h>

// Encode RGBA to WebP lossy. Returns 1 on success, 0 on error.
int c_encode_lossy(const uint8_t* rgba, int width, int height, int stride,
                   float quality, uint8_t** output, size_t* output_size);

// Encode RGBA to WebP lossless. Returns 1 on success, 0 on error.
int c_encode_lossless(const uint8_t* rgba, int width, int height, int stride,
                      uint8_t** output, size_t* output_size);

// Decode WebP to RGBA. Returns 1 on success, 0 on error.
int c_decode_rgba(const uint8_t* data, size_t data_size,
                  int* width, int* height, uint8_t** output);

// Validate a WebP bitstream. Returns 1 if valid, 0 otherwise.
int c_validate_webp(const uint8_t* data, size_t data_size,
                    int* width, int* height);

// Free memory allocated by C encoder/decoder.
void c_free(uint8_t* ptr);

#endif // ROUNDTRIP_WRAPPER_H
