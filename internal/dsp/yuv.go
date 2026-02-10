package dsp

import "math"

// BT.601 YUV <-> RGB conversion using fixed-point arithmetic.
// All coefficients match libwebp yuv.h exactly.

// YUV -> RGB fixed-point multipliers (from yuv.h).
const (
	yuvFix  = 16   // fixed-point precision
	YUVFix  = yuvFix // exported for dithering callers
	yuvHalf = 1 << (yuvFix - 1)

	yuvFix2 = 6                  // additional precision for intermediate values
	yuvMask = (256 << yuvFix2) - 1

	kYScale = 19077 // 1.164 * (1 << 16)
	kRCr    = 26149 // 1.596 * (1 << 14)
	kGCb    = 6419  // 0.391 * (1 << 14)
	kGCr    = 13320 // 0.813 * (1 << 14)
	kBCb    = 33050 // 2.018 * (1 << 14)

	// Bias constants from the C reference (libwebp/src/dsp/yuv.h lines 80-90).
	// These are hardcoded values that absorb the (Y-16) and (U/V-128) offsets
	// into the fixed-point formula. They must match the C values exactly.
	//   R = MultHi(y, 19077) + MultHi(v, 26149) - 14234
	//   G = MultHi(y, 19077) - MultHi(u, 6419) - MultHi(v, 13320) + 8708
	//   B = MultHi(y, 19077) + MultHi(u, 33050) - 17685
	kRBias = 14234
	kGBias = 8708
	kBBias = 17685
)

// multHi computes (v * coeff) >> 8.
func multHi(v, coeff int) int {
	return (v * coeff) >> 8
}

// VP8kClip stores clipped values in [0..255] range, mapping input range
// [0..yuvMask] after shift by yuvFix2.
var vp8kClip [yuvMask + 1]uint8
var vp8kClip4Bits [yuvMask + 1]uint8

func initYUVTables() {
	for i := 0; i <= yuvMask; i++ {
		v := i >> yuvFix2
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		vp8kClip[i] = uint8(v)
		vp8kClip4Bits[i] = uint8((v >> 4) & 0x0f)
	}
}

func clip(v, maxVal int) uint8 {
	if v < 0 {
		return 0
	}
	if v > maxVal {
		return uint8(maxVal)
	}
	return uint8(v)
}

// YUVToR converts (y, v) to the R component.
func YUVToR(y, v int) uint8 {
	val := multHi(y, kYScale) + multHi(v, kRCr) - kRBias
	if val < 0 {
		return 0
	}
	if val > yuvMask {
		return 255
	}
	return vp8kClip[val]
}

// YUVToG converts (y, u, v) to the G component.
func YUVToG(y, u, v int) uint8 {
	val := multHi(y, kYScale) - multHi(u, kGCb) - multHi(v, kGCr) + kGBias
	if val < 0 {
		return 0
	}
	if val > yuvMask {
		return 255
	}
	return vp8kClip[val]
}

// YUVToB converts (y, u) to the B component.
func YUVToB(y, u int) uint8 {
	val := multHi(y, kYScale) + multHi(u, kBCb) - kBBias
	if val < 0 {
		return 0
	}
	if val > yuvMask {
		return 255
	}
	return vp8kClip[val]
}

// YUVToRGB converts YUV (in [16..235] / [16..240] full range) to RGB.
func YUVToRGB(y, u, v int, rgb []byte) {
	rgb[0] = YUVToR(y, v)
	rgb[1] = YUVToG(y, u, v)
	rgb[2] = YUVToB(y, u)
}

// YUVToBGR converts YUV to BGR (reversed channel order).
func YUVToBGR(y, u, v int, bgr []byte) {
	bgr[0] = YUVToB(y, u)
	bgr[1] = YUVToG(y, u, v)
	bgr[2] = YUVToR(y, v)
}

// RGB -> YUV conversion coefficients (from enc.c).
const (
	kRGBToY0 = 16839 // 0.2568 * (1 << 16)
	kRGBToY1 = 33059 // 0.5041 * (1 << 16)
	kRGBToY2 = 6420  // 0.0979 * (1 << 16)
	kRGBToU0 = -9719
	kRGBToU1 = -19081
	kRGBToU2 = 28800
	kRGBToV0 = 28800
	kRGBToV1 = -24116
	kRGBToV2 = -4684
)

// VP8ClipUV clips the intermediate UV value to [0..255].
// Matches C libwebp yuv.h: VP8ClipUV uses >> (YUV_FIX + 2) = >> 18.
// The extra +2 accounts for 4x accumulated pixel values (sum of 2x2 block).
// Callers must pass sum-of-4-pixels values (not averaged) and
// rounding = YUV_HALF << 2 = 1 << 17.
func VP8ClipUV(uv, rounding int) uint8 {
	uv = (uv + rounding + (128 << (yuvFix + 2))) >> (yuvFix + 2)
	if uv&^0xff == 0 {
		return uint8(uv)
	}
	if uv < 0 {
		return 0
	}
	return 255
}

// RGBToY converts an RGB triple to the Y component.
// Uses fixed rounding (YUV_HALF). For dithered conversion, use RGBToYRounding.
func RGBToY(r, g, b int) uint8 {
	return uint8((kRGBToY0*r + kRGBToY1*g + kRGBToY2*b + yuvHalf + (16 << 16)) >> 16)
}

// RGBToYRounding converts an RGB triple to the Y component with a custom
// rounding value. This is used for dithered RGB->YUV conversion where the
// rounding comes from VP8RandomBits(rg, YUV_FIX=16).
// Matches C VP8RGBToY(r, g, b, rounding).
func RGBToYRounding(r, g, b, rounding int) uint8 {
	return uint8((kRGBToY0*r + kRGBToY1*g + kRGBToY2*b + rounding + (16 << yuvFix)) >> yuvFix)
}

// RGBToU converts an RGB triple to the U component.
func RGBToU(r, g, b, rounding int) uint8 {
	return VP8ClipUV(kRGBToU0*r+kRGBToU1*g+kRGBToU2*b, rounding)
}

// RGBToV converts an RGB triple to the V component.
func RGBToV(r, g, b, rounding int) uint8 {
	return VP8ClipUV(kRGBToV0*r+kRGBToV1*g+kRGBToV2*b, rounding)
}

// --- Gamma correction for accurate chroma subsampling ---
// Reference: libwebp/src/dsp/yuv.c (SharpYuvInitGammaTables, etc.)

const (
	kGamma         = 0.80
	kGammaFix      = 12                          // fixed-point precision for linear values
	kGammaScale    = (1 << kGammaFix) - 1         // 4095
	kGammaTabFix   = 7                            // fixed-point fractional bits precision
	kGammaTabScale = 1 << kGammaTabFix            // 128
	kGammaTabSize  = 1 << (kGammaFix - kGammaTabFix) // 32, matches C GAMMA_TAB_SIZE
)

var (
	kLinearToGammaTab [kGammaTabSize + 2]uint32 // [0..GAMMA_TAB_SIZE+1] = [0..33]
	kGammaToLinearTab [256]uint32
	gammaTablesInited bool
)

// InitGammaTables initializes the gamma correction lookup tables.
// Safe to call multiple times; only initializes once.
func InitGammaTables() {
	if gammaTablesInited {
		return
	}
	gammaTablesInited = true

	// kGammaToLinearTab: maps gamma-space [0..255] to linear fixed-point.
	// C reference (yuv.c:243-244): pow(norm * v, kGamma) * kGammaScale
	// where kGamma = 0.80, norm = 1/255.
	for i := 0; i < 256; i++ {
		v := float64(i) / 255.0
		lin := gammaPow(v, kGamma) * float64(kGammaScale)
		kGammaToLinearTab[i] = uint32(lin + 0.5)
	}

	// kLinearToGammaTab: maps linear fixed-point [0..kGammaTabSize] to gamma [0..255].
	// C reference (yuv.c:246-247): 255 * pow(scale * v, 1/kGamma)
	// where scale = kGammaTabScale / kGammaScale.
	scale := float64(kGammaTabScale) / float64(kGammaScale)
	for i := 0; i <= kGammaTabSize; i++ {
		v := scale * float64(i)
		g := gammaPow(v, 1.0/kGamma) * 255.0
		kLinearToGammaTab[i] = uint32(g + 0.5)
	}
	kLinearToGammaTab[kGammaTabSize+1] = 255
}

// gammaPow computes base^exp for gamma correction.
func gammaPow(base, exp float64) float64 {
	if base <= 0 {
		return 0
	}
	return math.Pow(base, exp)
}

// GammaToLinear converts a gamma-space [0..255] value to linear fixed-point.
func GammaToLinear(v uint8) uint32 {
	return kGammaToLinearTab[v]
}

// kGammaTabRounder is the rounding value for descaling after interpolation.
// Matches C: (1 << GAMMA_TAB_FIX >> 1) = 64.
const kGammaTabRounder = kGammaTabScale >> 1

// LinearToGamma converts a linear fixed-point value back to gamma space [0..255].
// The shift parameter allows operating on sums: shift=0 for sum-of-4, shift=1 for sum-of-2.
// Matches C libwebp Interpolate() + LinearToGamma() (yuv.c:257-272).
func LinearToGamma(baseValue uint32, shift int) int {
	v := int(baseValue) << shift
	// Interpolate: matches C Interpolate() (yuv.c:257-265)
	tabPos := v >> (kGammaTabFix + 2)
	if tabPos >= kGammaTabSize {
		tabPos = kGammaTabSize - 1
	}
	x := v & ((kGammaTabScale << 2) - 1)
	v0 := int(kLinearToGammaTab[tabPos])
	v1 := int(kLinearToGammaTab[tabPos+1])
	y := v1*x + v0*((kGammaTabScale<<2)-x)
	return (y + kGammaTabRounder) >> kGammaTabFix
}

// GammaAverageRGB computes the gamma-correct average of 4 RGB pixel values
// for chroma subsampling. Converts to linear, sums, converts back.
// Matches C SUM4 macro (libwebp/src/dsp/yuv.c:296-300): sum of 4 linear
// values passed to LinearToGamma with shift=0.
func GammaAverageRGB(r0, g0, b0, r1, g1, b1, r2, g2, b2, r3, g3, b3 int) (int, int, int) {
	InitGammaTables()
	lr := GammaToLinear(uint8(r0)) + GammaToLinear(uint8(r1)) +
		GammaToLinear(uint8(r2)) + GammaToLinear(uint8(r3))
	lg := GammaToLinear(uint8(g0)) + GammaToLinear(uint8(g1)) +
		GammaToLinear(uint8(g2)) + GammaToLinear(uint8(g3))
	lb := GammaToLinear(uint8(b0)) + GammaToLinear(uint8(b1)) +
		GammaToLinear(uint8(b2)) + GammaToLinear(uint8(b3))
	return LinearToGamma(lr, 0), LinearToGamma(lg, 0), LinearToGamma(lb, 0)
}

// --- Packed ARGB -> YUV batch conversion pipeline ---
// Reference: libwebp/src/dsp/yuv.c (ConvertARGBToY_C, WebPConvertARGBToUV_C)
// Packed ARGB format: each uint32 is 0xAARRGGBB (alpha in MSB).

// ConvertARGBToY batch-converts a row of packed ARGB pixels to Y plane values.
// Matches C ConvertARGBToY_C (yuv.c:137-145).
func ConvertARGBToY(argb []uint32, y []byte, width int) {
	for i := 0; i < width; i++ {
		p := argb[i]
		r := int((p >> 16) & 0xff)
		g := int((p >> 8) & 0xff)
		b := int(p & 0xff)
		y[i] = RGBToY(r, g, b)
	}
}

// ConvertARGBToUV batch-converts a row of packed ARGB pixels to U/V plane values.
// This handles a single row for chroma subsampling: pairs of pixels are combined
// to produce one U and one V sample. The values are scaled to match the sum-of-4
// convention expected by VP8ClipUV (shift one less to double the contribution of
// each pixel, since we only have 2 pixels per row instead of 4 in a 2x2 block).
//
// When doStore is true, the U/V values are written directly. When false, they are
// averaged with existing values (for combining two rows of a 2x2 block).
//
// Matches C WebPConvertARGBToUV_C (yuv.c:147-187).
func ConvertARGBToUV(argb []uint32, u, v []byte, srcWidth int, doStore bool) {
	uvWidth := srcWidth >> 1
	rounding := yuvHalf << 2

	for i := 0; i < uvWidth; i++ {
		v0 := argb[2*i]
		v1 := argb[2*i+1]
		// Scale by 2: shift one less to produce sum-of-4 equivalent from 2 pixels.
		r := int((v0>>15)&0x1fe) + int((v1>>15)&0x1fe)
		g := int((v0>>7)&0x1fe) + int((v1>>7)&0x1fe)
		b := int((v0<<1)&0x1fe) + int((v1<<1)&0x1fe)
		tmpU := RGBToU(r, g, b, rounding)
		tmpV := RGBToV(r, g, b, rounding)
		if doStore {
			u[i] = tmpU
			v[i] = tmpV
		} else {
			// Approximated average-of-four (combining two rows).
			u[i] = uint8((int(u[i]) + int(tmpU) + 1) >> 1)
			v[i] = uint8((int(v[i]) + int(tmpV) + 1) >> 1)
		}
	}
	// Handle odd last pixel: scale by 4 to match sum-of-4 convention.
	if srcWidth&1 != 0 {
		v0 := argb[2*uvWidth]
		r := int((v0 >> 14) & 0x3fc)
		g := int((v0 >> 6) & 0x3fc)
		b := int((v0 << 2) & 0x3fc)
		tmpU := RGBToU(r, g, b, rounding)
		tmpV := RGBToV(r, g, b, rounding)
		if doStore {
			u[uvWidth] = tmpU
			v[uvWidth] = tmpV
		} else {
			u[uvWidth] = uint8((int(u[uvWidth]) + int(tmpU) + 1) >> 1)
			v[uvWidth] = uint8((int(v[uvWidth]) + int(tmpV) + 1) >> 1)
		}
	}
}

// --- Alpha-weighted chroma conversion ---
// Reference: libwebp/src/dsp/yuv.c (LinearToGammaWeighted, kInvAlpha table,
// DIVIDE_BY_ALPHA, WebPAccumulateRGBA)

// kAlphaFix is the fixed-point precision for the inverse-alpha table.
const kAlphaFix = 19

// kInvAlpha is a precomputed table of (1 << kAlphaFix) / a for a in [0..4*255].
// Used for fast division by alpha sums. The formula (v * kInvAlpha[a]) >> kAlphaFix
// is equivalent to v / a in most (99.6%) cases.
// Matches C kInvAlpha[4*0xff+1] (yuv.c:310-421).
var kInvAlpha = [4*0xff + 1]uint32{
	0, // alpha = 0
	524288, 262144, 174762, 131072, 104857, 87381, 74898, 65536, 58254, 52428,
	47662, 43690, 40329, 37449, 34952, 32768, 30840, 29127, 27594, 26214,
	24966, 23831, 22795, 21845, 20971, 20164, 19418, 18724, 18078, 17476,
	16912, 16384, 15887, 15420, 14979, 14563, 14169, 13797, 13443, 13107,
	12787, 12483, 12192, 11915, 11650, 11397, 11155, 10922, 10699, 10485,
	10280, 10082, 9892, 9709, 9532, 9362, 9198, 9039, 8886, 8738,
	8594, 8456, 8322, 8192, 8065, 7943, 7825, 7710, 7598, 7489,
	7384, 7281, 7182, 7084, 6990, 6898, 6808, 6721, 6636, 6553,
	6472, 6393, 6316, 6241, 6168, 6096, 6026, 5957, 5890, 5825,
	5761, 5698, 5637, 5577, 5518, 5461, 5405, 5349, 5295, 5242,
	5190, 5140, 5090, 5041, 4993, 4946, 4899, 4854, 4809, 4766,
	4723, 4681, 4639, 4599, 4559, 4519, 4481, 4443, 4405, 4369,
	4332, 4297, 4262, 4228, 4194, 4161, 4128, 4096, 4064, 4032,
	4002, 3971, 3942, 3912, 3883, 3855, 3826, 3799, 3771, 3744,
	3718, 3692, 3666, 3640, 3615, 3591, 3566, 3542, 3518, 3495,
	3472, 3449, 3426, 3404, 3382, 3360, 3339, 3318, 3297, 3276,
	3256, 3236, 3216, 3196, 3177, 3158, 3139, 3120, 3102, 3084,
	3066, 3048, 3030, 3013, 2995, 2978, 2962, 2945, 2928, 2912,
	2896, 2880, 2864, 2849, 2833, 2818, 2803, 2788, 2774, 2759,
	2744, 2730, 2716, 2702, 2688, 2674, 2661, 2647, 2634, 2621,
	2608, 2595, 2582, 2570, 2557, 2545, 2532, 2520, 2508, 2496,
	2484, 2473, 2461, 2449, 2438, 2427, 2416, 2404, 2394, 2383,
	2372, 2361, 2351, 2340, 2330, 2319, 2309, 2299, 2289, 2279,
	2269, 2259, 2250, 2240, 2231, 2221, 2212, 2202, 2193, 2184,
	2175, 2166, 2157, 2148, 2139, 2131, 2122, 2114, 2105, 2097,
	2088, 2080, 2072, 2064, 2056, 2048, 2040, 2032, 2024, 2016,
	2008, 2001, 1993, 1985, 1978, 1971, 1963, 1956, 1949, 1941,
	1934, 1927, 1920, 1913, 1906, 1899, 1892, 1885, 1879, 1872,
	1865, 1859, 1852, 1846, 1839, 1833, 1826, 1820, 1814, 1807,
	1801, 1795, 1789, 1783, 1777, 1771, 1765, 1759, 1753, 1747,
	1741, 1736, 1730, 1724, 1718, 1713, 1707, 1702, 1696, 1691,
	1685, 1680, 1675, 1669, 1664, 1659, 1653, 1648, 1643, 1638,
	1633, 1628, 1623, 1618, 1613, 1608, 1603, 1598, 1593, 1588,
	1583, 1579, 1574, 1569, 1565, 1560, 1555, 1551, 1546, 1542,
	1537, 1533, 1528, 1524, 1519, 1515, 1510, 1506, 1502, 1497,
	1493, 1489, 1485, 1481, 1476, 1472, 1468, 1464, 1460, 1456,
	1452, 1448, 1444, 1440, 1436, 1432, 1428, 1424, 1420, 1416,
	1413, 1409, 1405, 1401, 1398, 1394, 1390, 1387, 1383, 1379,
	1376, 1372, 1368, 1365, 1361, 1358, 1354, 1351, 1347, 1344,
	1340, 1337, 1334, 1330, 1327, 1323, 1320, 1317, 1314, 1310,
	1307, 1304, 1300, 1297, 1294, 1291, 1288, 1285, 1281, 1278,
	1275, 1272, 1269, 1266, 1263, 1260, 1257, 1254, 1251, 1248,
	1245, 1242, 1239, 1236, 1233, 1230, 1227, 1224, 1222, 1219,
	1216, 1213, 1210, 1208, 1205, 1202, 1199, 1197, 1194, 1191,
	1188, 1186, 1183, 1180, 1178, 1175, 1172, 1170, 1167, 1165,
	1162, 1159, 1157, 1154, 1152, 1149, 1147, 1144, 1142, 1139,
	1137, 1134, 1132, 1129, 1127, 1125, 1122, 1120, 1117, 1115,
	1113, 1110, 1108, 1106, 1103, 1101, 1099, 1096, 1094, 1092,
	1089, 1087, 1085, 1083, 1081, 1078, 1076, 1074, 1072, 1069,
	1067, 1065, 1063, 1061, 1059, 1057, 1054, 1052, 1050, 1048,
	1046, 1044, 1042, 1040, 1038, 1036, 1034, 1032, 1030, 1028,
	1026, 1024, 1022, 1020, 1018, 1016, 1014, 1012, 1010, 1008,
	1006, 1004, 1002, 1000, 998, 996, 994, 992, 991, 989,
	987, 985, 983, 981, 979, 978, 976, 974, 972, 970,
	969, 967, 965, 963, 961, 960, 958, 956, 954, 953,
	951, 949, 948, 946, 944, 942, 941, 939, 937, 936,
	934, 932, 931, 929, 927, 926, 924, 923, 921, 919,
	918, 916, 914, 913, 911, 910, 908, 907, 905, 903,
	902, 900, 899, 897, 896, 894, 893, 891, 890, 888,
	887, 885, 884, 882, 881, 879, 878, 876, 875, 873,
	872, 870, 869, 868, 866, 865, 863, 862, 860, 859,
	858, 856, 855, 853, 852, 851, 849, 848, 846, 845,
	844, 842, 841, 840, 838, 837, 836, 834, 833, 832,
	830, 829, 828, 826, 825, 824, 823, 821, 820, 819,
	817, 816, 815, 814, 812, 811, 810, 809, 807, 806,
	805, 804, 802, 801, 800, 799, 798, 796, 795, 794,
	793, 791, 790, 789, 788, 787, 786, 784, 783, 782,
	781, 780, 779, 777, 776, 775, 774, 773, 772, 771,
	769, 768, 767, 766, 765, 764, 763, 762, 760, 759,
	758, 757, 756, 755, 754, 753, 752, 751, 750, 748,
	747, 746, 745, 744, 743, 742, 741, 740, 739, 738,
	737, 736, 735, 734, 733, 732, 731, 730, 729, 728,
	727, 726, 725, 724, 723, 722, 721, 720, 719, 718,
	717, 716, 715, 714, 713, 712, 711, 710, 709, 708,
	707, 706, 705, 704, 703, 702, 701, 700, 699, 699,
	698, 697, 696, 695, 694, 693, 692, 691, 690, 689,
	688, 688, 687, 686, 685, 684, 683, 682, 681, 680,
	680, 679, 678, 677, 676, 675, 674, 673, 673, 672,
	671, 670, 669, 668, 667, 667, 666, 665, 664, 663,
	662, 661, 661, 660, 659, 658, 657, 657, 656, 655,
	654, 653, 652, 652, 651, 650, 649, 648, 648, 647,
	646, 645, 644, 644, 643, 642, 641, 640, 640, 639,
	638, 637, 637, 636, 635, 634, 633, 633, 632, 631,
	630, 630, 629, 628, 627, 627, 626, 625, 624, 624,
	623, 622, 621, 621, 620, 619, 618, 618, 617, 616,
	616, 615, 614, 613, 613, 612, 611, 611, 610, 609,
	608, 608, 607, 606, 606, 605, 604, 604, 603, 602,
	601, 601, 600, 599, 599, 598, 597, 597, 596, 595,
	595, 594, 593, 593, 592, 591, 591, 590, 589, 589,
	588, 587, 587, 586, 585, 585, 584, 583, 583, 582,
	581, 581, 580, 579, 579, 578, 578, 577, 576, 576,
	575, 574, 574, 573, 572, 572, 571, 571, 570, 569,
	569, 568, 568, 567, 566, 566, 565, 564, 564, 563,
	563, 562, 561, 561, 560, 560, 559, 558, 558, 557,
	557, 556, 555, 555, 554, 554, 553, 553, 552, 551,
	551, 550, 550, 549, 548, 548, 547, 547, 546, 546,
	545, 544, 544, 543, 543, 542, 542, 541, 541, 540,
	539, 539, 538, 538, 537, 537, 536, 536, 535, 534,
	534, 533, 533, 532, 532, 531, 531, 530, 530, 529,
	529, 528, 527, 527, 526, 526, 525, 525, 524, 524,
	523, 523, 522, 522, 521, 521, 520, 520, 519, 519,
	518, 518, 517, 517, 516, 516, 515, 515, 514, 514,
}

// divideByAlpha computes sum/a using the inverse-alpha table.
// The result is pre-multiplied by 4 (matching the C DIVIDE_BY_ALPHA macro)
// since LinearToGamma expects values premultiplied by 4.
// Matches C: (sum * kInvAlpha[a]) >> (kAlphaFix - 2) (yuv.c:425).
func divideByAlpha(sum, a uint32) uint32 {
	return (sum * kInvAlpha[a]) >> (kAlphaFix - 2)
}

// LinearToGammaWeighted computes the alpha-weighted gamma-correct average
// of a 2x2 block of a single color channel for chroma subsampling.
// When pixels have varying alpha, the chroma contribution of each pixel
// is weighted by its alpha to prevent color bleeding at transparency edges.
//
// The four pixel values are at positions: [0], [step], [stride], [stride+step].
// totalA is the sum of the four alpha values (must be > 0 and <= 4*255).
//
// Matches C LinearToGammaWeighted (yuv.c:433-447).
func LinearToGammaWeighted(src [4]uint8, alpha [4]uint8, totalA uint32) int {
	sum := uint32(alpha[0])*GammaToLinear(src[0]) +
		uint32(alpha[1])*GammaToLinear(src[1]) +
		uint32(alpha[2])*GammaToLinear(src[2]) +
		uint32(alpha[3])*GammaToLinear(src[3])
	return LinearToGamma(divideByAlpha(sum, totalA), 0)
}

// AccumulateRGBA computes gamma-corrected, alpha-weighted 2x2 averages
// of RGBA pixel data for chroma subsampling. For each 2x2 block of pixels,
// it produces one set of (R, G, B, A) values in the dst slice (as uint16).
//
// When alpha is fully opaque (4*0xff) or fully transparent (0), standard
// gamma averaging is used (SUM4). Otherwise, alpha-weighted averaging is
// used to prevent color bleeding at transparency edges.
//
// r, g, b, a are planar channel data where pixels at offsets [j], [j+step],
// [j+stride], [j+stride+step] form a 2x2 block.
//
// Matches C WebPAccumulateRGBA (yuv.c:449-488).
func AccumulateRGBA(r, g, b, a []uint8, stride int, dst []uint16, width int) {
	InitGammaTables()
	j := 0
	dIdx := 0
	for i := 0; i < (width >> 1); i++ {
		// Sum alpha for the 2x2 block (step=4 for RGBA interleaved, but here
		// we work with planar data, so step between horizontal neighbors is 1).
		totalA := uint32(a[j]) + uint32(a[j+1]) + uint32(a[j+stride]) + uint32(a[j+stride+1])
		var rv, gv, bv int
		if totalA == 4*0xff || totalA == 0 {
			// Fully opaque or transparent: standard gamma average (SUM4).
			rv = LinearToGamma(
				GammaToLinear(r[j])+GammaToLinear(r[j+1])+
					GammaToLinear(r[j+stride])+GammaToLinear(r[j+stride+1]), 0)
			gv = LinearToGamma(
				GammaToLinear(g[j])+GammaToLinear(g[j+1])+
					GammaToLinear(g[j+stride])+GammaToLinear(g[j+stride+1]), 0)
			bv = LinearToGamma(
				GammaToLinear(b[j])+GammaToLinear(b[j+1])+
					GammaToLinear(b[j+stride])+GammaToLinear(b[j+stride+1]), 0)
		} else {
			// Semi-transparent: alpha-weighted average.
			srcR := [4]uint8{r[j], r[j+1], r[j+stride], r[j+stride+1]}
			srcG := [4]uint8{g[j], g[j+1], g[j+stride], g[j+stride+1]}
			srcB := [4]uint8{b[j], b[j+1], b[j+stride], b[j+stride+1]}
			alphas := [4]uint8{a[j], a[j+1], a[j+stride], a[j+stride+1]}
			rv = LinearToGammaWeighted(srcR, alphas, totalA)
			gv = LinearToGammaWeighted(srcG, alphas, totalA)
			bv = LinearToGammaWeighted(srcB, alphas, totalA)
		}
		dst[dIdx] = uint16(rv)
		dst[dIdx+1] = uint16(gv)
		dst[dIdx+2] = uint16(bv)
		dst[dIdx+3] = uint16(totalA)
		j += 2
		dIdx += 4
	}
	// Handle odd last column: use SUM2 (2 pixels vertically).
	if width&1 != 0 {
		totalA := 2 * (uint32(a[j]) + uint32(a[j+stride]))
		var rv, gv, bv int
		if totalA == 4*0xff || totalA == 0 {
			rv = LinearToGamma(GammaToLinear(r[j])+GammaToLinear(r[j+stride]), 1)
			gv = LinearToGamma(GammaToLinear(g[j])+GammaToLinear(g[j+stride]), 1)
			bv = LinearToGamma(GammaToLinear(b[j])+GammaToLinear(b[j+stride]), 1)
		} else {
			// For odd column, step=0 (same column), so positions are [j] and [j+stride].
			// We double the values to make them sum-of-4 equivalent.
			srcR := [4]uint8{r[j], r[j], r[j+stride], r[j+stride]}
			srcG := [4]uint8{g[j], g[j], g[j+stride], g[j+stride]}
			srcB := [4]uint8{b[j], b[j], b[j+stride], b[j+stride]}
			alphas := [4]uint8{a[j], a[j], a[j+stride], a[j+stride]}
			rv = LinearToGammaWeighted(srcR, alphas, totalA)
			gv = LinearToGammaWeighted(srcG, alphas, totalA)
			bv = LinearToGammaWeighted(srcB, alphas, totalA)
		}
		dst[dIdx] = uint16(rv)
		dst[dIdx+1] = uint16(gv)
		dst[dIdx+2] = uint16(bv)
		dst[dIdx+3] = uint16(totalA)
	}
}

// ConvertRGBA32ToUV converts accumulated 2x2 averaged RGBA values (as uint16)
// to U/V plane values. The input is in the format produced by AccumulateRGBA:
// each group of 4 uint16 values is (R, G, B, A).
// Matches C WebPConvertRGBA32ToUV_C (yuv.c:207-216).
func ConvertRGBA32ToUV(rgb []uint16, u, v []byte, width int) {
	rounding := yuvHalf << 2
	for i := 0; i < width; i++ {
		r := int(rgb[i*4])
		g := int(rgb[i*4+1])
		b := int(rgb[i*4+2])
		u[i] = RGBToU(r, g, b, rounding)
		v[i] = RGBToV(r, g, b, rounding)
	}
}

// ConvertRGBA32ToUVDithered is like ConvertRGBA32ToUV but uses dithered
// rounding from the VP8Random generator instead of fixed rounding.
// The rounding value for U/V uses YUV_FIX+2 = 18 bits, matching C
// ConvertRowsToUV which calls VP8RandomBits(rg, YUV_FIX + 2).
func ConvertRGBA32ToUVDithered(rgb []uint16, u, v []byte, width int, rg *VP8Random) {
	for i := 0; i < width; i++ {
		r := int(rgb[i*4])
		g := int(rgb[i*4+1])
		b := int(rgb[i*4+2])
		u[i] = RGBToU(r, g, b, RandomBits(rg, yuvFix+2))
		v[i] = RGBToV(r, g, b, RandomBits(rg, yuvFix+2))
	}
}
