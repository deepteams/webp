//go:build !amd64

package dsp

// hasAVX2 is always false on non-amd64 platforms.
var hasAVX2 = false

// HasAVX2 returns false on non-amd64 platforms.
func HasAVX2() bool {
	return false
}
