//go:build amd64

package dsp

// hasAVX2 is set at init time by probing CPUID for AVX2 support.
// This file is cpuid_amd64.go (alphabetically before dsp_amd64.go),
// so hasAVX2 is ready before the dispatch init() runs.
var hasAVX2 bool

func init() {
	hasAVX2 = cpuidAVX2Check()
}

// HasAVX2 returns true if the CPU supports AVX2 and the OS has enabled
// YMM state saving. Exported for use by other internal packages (e.g. lossy).
func HasAVX2() bool {
	return hasAVX2
}

//go:noescape
func cpuidAVX2Check() bool
