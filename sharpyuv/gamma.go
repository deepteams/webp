package sharpyuv

import (
	"math"
	"sync"
)

// TransferFunc identifies a transfer function (OETF/EOTF) as defined in H.273.
type TransferFunc int

const (
	TransferBT709       TransferFunc = 1
	TransferBT470M      TransferFunc = 4
	TransferBT470BG     TransferFunc = 5
	TransferBT601       TransferFunc = 6
	TransferSMPTE240    TransferFunc = 7
	TransferLinear      TransferFunc = 8
	TransferLog100      TransferFunc = 9
	TransferLog100Sqrt  TransferFunc = 10
	TransferIEC61966    TransferFunc = 11
	TransferBT1361      TransferFunc = 12
	TransferSRGB        TransferFunc = 13
	TransferBT2020_10   TransferFunc = 14
	TransferBT2020_12   TransferFunc = 15
	TransferPQ          TransferFunc = 16 // SMPTE 2084
	TransferSMPTE428    TransferFunc = 17
	TransferHLG         TransferFunc = 18
)

// Gamma-related constants.
const (
	gammaToLinearTabBits = 10
	gammaToLinearTabSize = 1 << gammaToLinearTabBits
	linearToGammaTabBits = 9
	linearToGammaTabSize = 1 << linearToGammaTabBits
	gammaToLinearBits    = 16
)

var (
	gammaToLinearTab [gammaToLinearTabSize + 2]uint32
	linearToGammaTab [linearToGammaTabSize + 2]uint32
	gammaTablesOnce  sync.Once
)

const gammaF = 1.0 / 0.45

// initGammaTables precomputes the sRGB gamma/linear lookup tables.
func initGammaTables() {
	gammaTablesOnce.Do(func() {
		const a = 0.09929682680944
		const thresh = 0.018053968510807
		const finalScale = float64(uint32(1) << gammaToLinearBits)

		// Gamma to linear table.
		{
			norm := 1.0 / float64(gammaToLinearTabSize)
			aRec := 1.0 / (1.0 + a)
			for v := 0; v <= gammaToLinearTabSize; v++ {
				g := norm * float64(v)
				var value float64
				if g <= thresh*4.5 {
					value = g / 4.5
				} else {
					value = math.Pow(aRec*(g+a), gammaF)
				}
				gammaToLinearTab[v] = uint32(value*finalScale + 0.5)
			}
			gammaToLinearTab[gammaToLinearTabSize+1] = gammaToLinearTab[gammaToLinearTabSize]
		}

		// Linear to gamma table.
		{
			scale := 1.0 / float64(linearToGammaTabSize)
			for v := 0; v <= linearToGammaTabSize; v++ {
				g := scale * float64(v)
				var value float64
				if g <= thresh {
					value = 4.5 * g
				} else {
					value = (1.0+a)*math.Pow(g, 1.0/gammaF) - a
				}
				linearToGammaTab[v] = uint32(finalScale*value + 0.5)
			}
			linearToGammaTab[linearToGammaTabSize+1] = linearToGammaTab[linearToGammaTabSize]
		}
	})
}

func shiftVal(v, shift int) int {
	if shift >= 0 {
		return v << uint(shift)
	}
	return v >> uint(-shift)
}

func fixedPointInterpolation(v int, tab []uint32, tabPosShiftRight, tabValueShift int) uint32 {
	tabPos := shiftVal(v, -tabPosShiftRight)
	x := uint32(v - shiftVal(tabPos, tabPosShiftRight)) // fractional part
	v0 := uint32(shiftVal(int(tab[tabPos]), tabValueShift))
	v1 := uint32(shiftVal(int(tab[tabPos+1]), tabValueShift))
	v2 := (v1 - v0) * x
	half := 0
	if tabPosShiftRight > 0 {
		half = 1 << uint(tabPosShiftRight-1)
	}
	return v0 + (v2+uint32(half))>>uint(tabPosShiftRight)
}

func toLinearSrgb(v uint16, bitDepth int) uint32 {
	shift := gammaToLinearTabBits - bitDepth
	if shift > 0 {
		return gammaToLinearTab[int(v)<<uint(shift)]
	}
	return fixedPointInterpolation(int(v), gammaToLinearTab[:], -shift, 0)
}

func fromLinearSrgb(value uint32, bitDepth int) uint16 {
	return uint16(fixedPointInterpolation(
		int(value), linearToGammaTab[:],
		gammaToLinearBits-linearToGammaTabBits,
		bitDepth-gammaToLinearBits,
	))
}

// --- Transfer function implementations ---

func clampF(x, low, high float32) float32 {
	if x < low {
		return low
	}
	if x > high {
		return high
	}
	return x
}

func powf(base, exp float32) float32 {
	return float32(math.Pow(float64(base), float64(exp)))
}

func log10f(x float32) float32 {
	return float32(math.Log10(float64(x)))
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func roundf(x float32) float32 {
	if x < 0 {
		return float32(math.Ceil(float64(x - 0.5)))
	}
	return float32(math.Floor(float64(x + 0.5)))
}

// BT.709 / BT.601 / BT.2020 transfer
func toLinear709(gamma float32) float32 {
	if gamma < 0 {
		return 0
	} else if gamma < 4.5*0.018053968510807 {
		return gamma / 4.5
	} else if gamma < 1 {
		return powf((gamma+0.09929682680944)/1.09929682680944, 1.0/0.45)
	}
	return 1
}

func fromLinear709(linear float32) float32 {
	if linear < 0 {
		return 0
	} else if linear < 0.018053968510807 {
		return linear * 4.5
	} else if linear < 1 {
		return 1.09929682680944*powf(linear, 0.45) - 0.09929682680944
	}
	return 1
}

// BT.470M (gamma 2.2)
func toLinear470M(gamma float32) float32 {
	return powf(clampF(gamma, 0, 1), 2.2)
}

func fromLinear470M(linear float32) float32 {
	return powf(clampF(linear, 0, 1), 1.0/2.2)
}

// BT.470BG (gamma 2.8)
func toLinear470BG(gamma float32) float32 {
	return powf(clampF(gamma, 0, 1), 2.8)
}

func fromLinear470BG(linear float32) float32 {
	return powf(clampF(linear, 0, 1), 1.0/2.8)
}

// SMPTE 240
func toLinearSmpte240(gamma float32) float32 {
	if gamma < 0 {
		return 0
	} else if gamma < 4.0*0.022821585529445 {
		return gamma / 4.0
	} else if gamma < 1 {
		return powf((gamma+0.111572195921731)/1.111572195921731, 1.0/0.45)
	}
	return 1
}

func fromLinearSmpte240(linear float32) float32 {
	if linear < 0 {
		return 0
	} else if linear < 0.022821585529445 {
		return linear * 4.0
	} else if linear < 1 {
		return 1.111572195921731*powf(linear, 0.45) - 0.111572195921731
	}
	return 1
}

// Log 100
func toLinearLog100(gamma float32) float32 {
	const midInterval = 0.01 / 2.0
	if gamma <= 0 {
		return midInterval
	}
	return powf(10.0, 2.0*(minF(gamma, 1.0)-1.0))
}

func fromLinearLog100(linear float32) float32 {
	if linear < 0.01 {
		return 0
	}
	return 1.0 + log10f(minF(linear, 1.0))/2.0
}

// Log 100 * Sqrt(10)
func toLinearLog100Sqrt10(gamma float32) float32 {
	const midInterval = 0.00316227766 / 2.0
	if gamma <= 0 {
		return midInterval
	}
	return powf(10.0, 2.5*(minF(gamma, 1.0)-1.0))
}

func fromLinearLog100Sqrt10(linear float32) float32 {
	if linear < 0.00316227766 {
		return 0
	}
	return 1.0 + log10f(minF(linear, 1.0))/2.5
}

// IEC 61966
func toLinearIEC61966(gamma float32) float32 {
	if gamma <= -4.5*0.018053968510807 {
		return powf((-gamma+0.09929682680944)/-1.09929682680944, 1.0/0.45)
	} else if gamma < 4.5*0.018053968510807 {
		return gamma / 4.5
	}
	return powf((gamma+0.09929682680944)/1.09929682680944, 1.0/0.45)
}

func fromLinearIEC61966(linear float32) float32 {
	if linear <= -0.018053968510807 {
		return -1.09929682680944*powf(-linear, 0.45) + 0.09929682680944
	} else if linear < 0.018053968510807 {
		return linear * 4.5
	}
	return 1.09929682680944*powf(linear, 0.45) - 0.09929682680944
}

// BT.1361
func toLinearBT1361(gamma float32) float32 {
	if gamma < -0.25 {
		return -0.25
	} else if gamma < 0 {
		return powf((gamma-0.02482420670236)/-0.27482420670236, 1.0/0.45) / -4.0
	} else if gamma < 4.5*0.018053968510807 {
		return gamma / 4.5
	} else if gamma < 1 {
		return powf((gamma+0.09929682680944)/1.09929682680944, 1.0/0.45)
	}
	return 1
}

func fromLinearBT1361(linear float32) float32 {
	if linear < -0.25 {
		return -0.25
	} else if linear < 0 {
		return -0.27482420670236*powf(-4.0*linear, 0.45) + 0.02482420670236
	} else if linear < 0.018053968510807 {
		return linear * 4.5
	} else if linear < 1 {
		return 1.09929682680944*powf(linear, 0.45) - 0.09929682680944
	}
	return 1
}

// PQ (Perceptual Quantizer, SMPTE 2084)
func toLinearPQ(gamma float32) float32 {
	if gamma > 0 {
		powGamma := powf(gamma, 32.0/2523.0)
		num := maxF(powGamma-107.0/128.0, 0.0)
		den := maxF(2413.0/128.0-2392.0/128.0*powGamma, math.SmallestNonzeroFloat32)
		return powf(num/den, 4096.0/653.0)
	}
	return 0
}

func fromLinearPQ(linear float32) float32 {
	if linear > 0 {
		powLinear := powf(linear, 653.0/4096.0)
		num := 107.0/128.0 + 2413.0/128.0*powLinear
		den := 1.0 + 2392.0/128.0*powLinear
		return powf(num/den, 2523.0/32.0)
	}
	return 0
}

// SMPTE 428
func toLinearSmpte428(gamma float32) float32 {
	return powf(maxF(gamma, 0), 2.6) / 0.91655527974030934
}

func fromLinearSmpte428(linear float32) float32 {
	return powf(0.91655527974030934*maxF(linear, 0), 1.0/2.6)
}

// HLG (Hybrid Log-Gamma)
func toLinearHLG(gamma float32) float32 {
	if gamma < 0 {
		return 0
	} else if gamma <= 0.5 {
		return powf((gamma*gamma)*(1.0/3.0), 1.2)
	}
	return powf((float32(math.Exp(float64((gamma-0.55991073)/0.17883277)))+0.28466892)/12.0, 1.2)
}

func fromLinearHLG(linear float32) float32 {
	linear = powf(linear, 1.0/1.2)
	if linear < 0 {
		return 0
	} else if linear <= 1.0/12.0 {
		return float32(math.Sqrt(float64(3.0 * linear)))
	}
	return 0.17883277*float32(math.Log(float64(12.0*linear-0.28466892))) + 0.55991073
}

// GammaToLinear converts a gamma-encoded value (in the range of bitDepth bits)
// to a 16-bit linear value using the specified transfer function.
func GammaToLinear(v uint16, bitDepth int, tf TransferFunc) uint32 {
	initGammaTables()

	if tf == TransferSRGB {
		return toLinearSrgb(v, bitDepth)
	}
	if tf == TransferLinear {
		return uint32(v)
	}

	vFloat := float32(v) / float32(int(1)<<uint(bitDepth)-1)
	var linear float32

	switch tf {
	case TransferBT709, TransferBT601, TransferBT2020_10, TransferBT2020_12:
		linear = toLinear709(vFloat)
	case TransferBT470M:
		linear = toLinear470M(vFloat)
	case TransferBT470BG:
		linear = toLinear470BG(vFloat)
	case TransferSMPTE240:
		linear = toLinearSmpte240(vFloat)
	case TransferLog100:
		linear = toLinearLog100(vFloat)
	case TransferLog100Sqrt:
		linear = toLinearLog100Sqrt10(vFloat)
	case TransferIEC61966:
		linear = toLinearIEC61966(vFloat)
	case TransferBT1361:
		linear = toLinearBT1361(vFloat)
	case TransferPQ:
		linear = toLinearPQ(vFloat)
	case TransferSMPTE428:
		linear = toLinearSmpte428(vFloat)
	case TransferHLG:
		linear = toLinearHLG(vFloat)
	default:
		linear = 0
	}

	return uint32(roundf(linear * float32((1<<16)-1)))
}

// LinearToGamma converts a 16-bit linear value to a gamma-encoded value
// in the range of bitDepth bits using the specified transfer function.
func LinearToGamma(v uint32, bitDepth int, tf TransferFunc) uint16 {
	initGammaTables()

	if tf == TransferSRGB {
		return fromLinearSrgb(v, bitDepth)
	}
	if tf == TransferLinear {
		return uint16(v)
	}

	vFloat := float32(v) / float32((1<<16)-1)
	var gamma float32

	switch tf {
	case TransferBT709, TransferBT601, TransferBT2020_10, TransferBT2020_12:
		gamma = fromLinear709(vFloat)
	case TransferBT470M:
		gamma = fromLinear470M(vFloat)
	case TransferBT470BG:
		gamma = fromLinear470BG(vFloat)
	case TransferSMPTE240:
		gamma = fromLinearSmpte240(vFloat)
	case TransferLog100:
		gamma = fromLinearLog100(vFloat)
	case TransferLog100Sqrt:
		gamma = fromLinearLog100Sqrt10(vFloat)
	case TransferIEC61966:
		gamma = fromLinearIEC61966(vFloat)
	case TransferBT1361:
		gamma = fromLinearBT1361(vFloat)
	case TransferPQ:
		gamma = fromLinearPQ(vFloat)
	case TransferSMPTE428:
		gamma = fromLinearSmpte428(vFloat)
	case TransferHLG:
		gamma = fromLinearHLG(vFloat)
	default:
		gamma = 0
	}

	return uint16(roundf(gamma * float32(int(1)<<uint(bitDepth)-1)))
}
