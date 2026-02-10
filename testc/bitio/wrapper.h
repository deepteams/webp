#ifndef BITIO_WRAPPER_H
#define BITIO_WRAPPER_H

#include <stdint.h>
#include <stddef.h>

// Boolean (arithmetic) coding wrappers
int c_bool_write_sequence(const int* bits, const int* probs, int count,
                          uint8_t* out_buf, int* out_size);
void c_bool_read_sequence(const uint8_t* data, int size,
                          const int* probs, int count, int* out_bits);

// Lossless bit-packing wrappers
int c_lossless_write_sequence(const uint32_t* values, const int* nbits,
                              int count, uint8_t* out_buf, int* out_size);
void c_lossless_read_sequence(const uint8_t* data, int size,
                              const int* nbits, int count,
                              uint32_t* out_values);

#endif
