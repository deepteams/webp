// wrapper.c - Standalone implementations of VP8L lossless DSP functions
// extracted from libwebp src/dsp/lossless.c and src/dsp/lossless_enc.c.
// These are direct copies of the _C variants to avoid the deep header chain.

#include "wrapper.h"
#include <stdlib.h>

// --- Helpers (from lossless.c) ---

static inline uint32_t Average2(uint32_t a0, uint32_t a1) {
    return (((a0 ^ a1) & 0xfefefefeu) >> 1) + (a0 & a1);
}

static inline uint32_t Average3(uint32_t a0, uint32_t a1, uint32_t a2) {
    return Average2(Average2(a0, a2), a1);
}

static inline uint32_t Average4(uint32_t a0, uint32_t a1, uint32_t a2,
                                uint32_t a3) {
    return Average2(Average2(a0, a1), Average2(a2, a3));
}

static inline uint32_t Clip255(uint32_t a) {
    if (a < 256) return a;
    return ~a >> 24;
}

static inline int AddSubtractComponentFull(int a, int b, int c) {
    return Clip255((uint32_t)(a + b - c));
}

static inline uint32_t ClampedAddSubtractFull(uint32_t c0, uint32_t c1,
                                              uint32_t c2) {
    const int a = AddSubtractComponentFull(c0 >> 24, c1 >> 24, c2 >> 24);
    const int r = AddSubtractComponentFull((c0 >> 16) & 0xff, (c1 >> 16) & 0xff,
                                           (c2 >> 16) & 0xff);
    const int g = AddSubtractComponentFull((c0 >> 8) & 0xff, (c1 >> 8) & 0xff,
                                           (c2 >> 8) & 0xff);
    const int b = AddSubtractComponentFull(c0 & 0xff, c1 & 0xff, c2 & 0xff);
    return ((uint32_t)a << 24) | (r << 16) | (g << 8) | b;
}

static inline int AddSubtractComponentHalf(int a, int b) {
    return Clip255((uint32_t)(a + (a - b) / 2));
}

static inline uint32_t ClampedAddSubtractHalf(uint32_t c0, uint32_t c1,
                                              uint32_t c2) {
    const uint32_t ave = Average2(c0, c1);
    const int a = AddSubtractComponentHalf(ave >> 24, c2 >> 24);
    const int r = AddSubtractComponentHalf((ave >> 16) & 0xff, (c2 >> 16) & 0xff);
    const int g = AddSubtractComponentHalf((ave >> 8) & 0xff, (c2 >> 8) & 0xff);
    const int b = AddSubtractComponentHalf((ave >> 0) & 0xff, (c2 >> 0) & 0xff);
    return ((uint32_t)a << 24) | (r << 16) | (g << 8) | b;
}

static inline int Sub3(int a, int b, int c) {
    const int pb = b - c;
    const int pa = a - c;
    return abs(pb) - abs(pa);
}

static inline uint32_t Select(uint32_t a, uint32_t b, uint32_t c) {
    const int pa_minus_pb =
        Sub3((a >> 24), (b >> 24), (c >> 24)) +
        Sub3((a >> 16) & 0xff, (b >> 16) & 0xff, (c >> 16) & 0xff) +
        Sub3((a >> 8) & 0xff, (b >> 8) & 0xff, (c >> 8) & 0xff) +
        Sub3((a) & 0xff, (b) & 0xff, (c) & 0xff);
    return (pa_minus_pb <= 0) ? a : b;
}

#define ARGB_BLACK 0xff000000u

// --- Predictors ---
// C convention: top[0] = T, top[-1] = TL, top[1] = TR

static uint32_t Pred0(const uint32_t* left, const uint32_t* top) {
    (void)left; (void)top;
    return ARGB_BLACK;
}
static uint32_t Pred1(const uint32_t* left, const uint32_t* top) {
    (void)top;
    return *left;
}
static uint32_t Pred2(const uint32_t* left, const uint32_t* top) {
    (void)left;
    return top[0];
}
static uint32_t Pred3(const uint32_t* left, const uint32_t* top) {
    (void)left;
    return top[1];
}
static uint32_t Pred4(const uint32_t* left, const uint32_t* top) {
    (void)left;
    return top[-1];
}
static uint32_t Pred5(const uint32_t* left, const uint32_t* top) {
    return Average3(*left, top[0], top[1]);
}
static uint32_t Pred6(const uint32_t* left, const uint32_t* top) {
    return Average2(*left, top[-1]);
}
static uint32_t Pred7(const uint32_t* left, const uint32_t* top) {
    return Average2(*left, top[0]);
}
static uint32_t Pred8(const uint32_t* left, const uint32_t* top) {
    (void)left;
    return Average2(top[-1], top[0]);
}
static uint32_t Pred9(const uint32_t* left, const uint32_t* top) {
    (void)left;
    return Average2(top[0], top[1]);
}
static uint32_t Pred10(const uint32_t* left, const uint32_t* top) {
    return Average4(*left, top[-1], top[0], top[1]);
}
static uint32_t Pred11(const uint32_t* left, const uint32_t* top) {
    return Select(top[0], *left, top[-1]);
}
static uint32_t Pred12(const uint32_t* left, const uint32_t* top) {
    return ClampedAddSubtractFull(*left, top[0], top[-1]);
}
static uint32_t Pred13(const uint32_t* left, const uint32_t* top) {
    return ClampedAddSubtractHalf(*left, top[0], top[-1]);
}

typedef uint32_t (*PredFunc)(const uint32_t*, const uint32_t*);
static const PredFunc kPredictors[14] = {
    Pred0, Pred1, Pred2, Pred3, Pred4, Pred5, Pred6,
    Pred7, Pred8, Pred9, Pred10, Pred11, Pred12, Pred13
};

uint32_t c_predictor(int mode, const uint32_t* left, const uint32_t* top) {
    if (mode < 0 || mode > 13) return 0;
    return kPredictors[mode](left, top);
}

// --- Color transforms ---

void c_add_green(const uint32_t* src, int num_pixels, uint32_t* dst) {
    int i;
    for (i = 0; i < num_pixels; ++i) {
        const uint32_t argb = src[i];
        const uint32_t green = ((argb >> 8) & 0xff);
        uint32_t red_blue = (argb & 0x00ff00ffu);
        red_blue += (green << 16) | green;
        red_blue &= 0x00ff00ffu;
        dst[i] = (argb & 0xff00ff00u) | red_blue;
    }
}

void c_subtract_green(uint32_t* argb_data, int num_pixels) {
    int i;
    for (i = 0; i < num_pixels; ++i) {
        const int argb = (int)argb_data[i];
        const int green = (argb >> 8) & 0xff;
        const uint32_t new_r = (((argb >> 16) & 0xff) - green) & 0xff;
        const uint32_t new_b = (((argb >> 0) & 0xff) - green) & 0xff;
        argb_data[i] = ((uint32_t)argb & 0xff00ff00u) | (new_r << 16) | new_b;
    }
}

static inline int ColorTransformDelta(int8_t color_pred, int8_t color) {
    return ((int)color_pred * color) >> 5;
}

void c_transform_color(const CMultipliers* m, uint32_t* data, int num_pixels) {
    int i;
    for (i = 0; i < num_pixels; ++i) {
        const uint32_t argb = data[i];
        const int8_t green = (int8_t)(argb >> 8);
        const int8_t red = (int8_t)(argb >> 16);
        int new_red = red & 0xff;
        int new_blue = argb & 0xff;
        new_red -= ColorTransformDelta((int8_t)m->green_to_red, green);
        new_red &= 0xff;
        new_blue -= ColorTransformDelta((int8_t)m->green_to_blue, green);
        new_blue -= ColorTransformDelta((int8_t)m->red_to_blue, red);
        new_blue &= 0xff;
        data[i] = (argb & 0xff00ff00u) | ((uint32_t)new_red << 16) | (uint32_t)new_blue;
    }
}

void c_transform_color_inverse(const CMultipliers* m, const uint32_t* src,
                                int num_pixels, uint32_t* dst) {
    int i;
    for (i = 0; i < num_pixels; ++i) {
        const uint32_t argb = src[i];
        const int8_t green = (int8_t)(argb >> 8);
        const uint32_t red = argb >> 16;
        int new_red = red & 0xff;
        int new_blue = argb & 0xff;
        new_red += ColorTransformDelta((int8_t)m->green_to_red, green);
        new_red &= 0xff;
        new_blue += ColorTransformDelta((int8_t)m->green_to_blue, green);
        new_blue += ColorTransformDelta((int8_t)m->red_to_blue, (int8_t)new_red);
        new_blue &= 0xff;
        dst[i] = (argb & 0xff00ff00u) | ((uint32_t)new_red << 16) | (uint32_t)new_blue;
    }
}
