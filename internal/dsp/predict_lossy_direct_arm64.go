//go:build arm64

package dsp

// PredLuma16Direct calls the 16x16 prediction function for the given mode.
// On ARM64, modes 0-3 dispatch to NEON assembly for ~2-4x speedup over scalar Go.
// Modes 4-6 (boundary cases) remain pure Go since they are rarely called.
func PredLuma16Direct(mode int, dst []byte, off int) {
	switch mode {
	case 0:
		dc16asmNEON(dst, off)
	case 1:
		tm16asmNEON(dst, off)
	case 2:
		ve16asmNEON(dst, off)
	case 3:
		he16asmNEON(dst, off)
	case 4:
		dc16NoTop(dst, off)
	case 5:
		dc16NoLeft(dst, off)
	case 6:
		dc16NoTopLeft(dst, off)
	}
}

// PredChroma8Direct calls the 8x8 chroma prediction function for the given mode.
// On ARM64, modes 0-3 dispatch to NEON assembly.
func PredChroma8Direct(mode int, dst []byte, off int) {
	switch mode {
	case 0:
		dc8uvasmNEON(dst, off)
	case 1:
		tm8uvasmNEON(dst, off)
	case 2:
		ve8uvasmNEON(dst, off)
	case 3:
		he8uvasmNEON(dst, off)
	case 4:
		dc8uvNoTop(dst, off)
	case 5:
		dc8uvNoLeft(dst, off)
	case 6:
		dc8uvNoTopLeft(dst, off)
	}
}
