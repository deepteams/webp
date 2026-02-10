#ifndef WRAPPER_TRANSFORMS_H
#define WRAPPER_TRANSFORMS_H

#include <stdint.h>

// Decoder transforms (from dec.c)
void c_transform_one(const int16_t* in, uint8_t* dst);
void c_transform_dc(const int16_t* in, uint8_t* dst);
void c_transform_ac3(const int16_t* in, uint8_t* dst);
void c_transform_wht(const int16_t* in, int16_t* out);

// Encoder transforms (from enc.c)
void c_ftransform(const uint8_t* src, const uint8_t* ref, int16_t* out);
void c_ftransform_wht(const int16_t* in, int16_t* out);
void c_itransform(const uint8_t* ref, const int16_t* in, uint8_t* dst, int do_two);

#endif // WRAPPER_TRANSFORMS_H
