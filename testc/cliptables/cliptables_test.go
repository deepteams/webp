//go:build testc

package cliptables_test

import (
	"testing"

	"github.com/deepteams/webp/internal/dsp"
	"github.com/deepteams/webp/testc/cliptables"
)

func init() {
	cliptables.Init()
}

func TestKsclip1(t *testing.T) {
	for v := -893; v <= 892; v++ {
		got := dsp.Ksclip1(v)
		want := cliptables.CKsclip1(v)
		if got != want {
			t.Fatalf("Ksclip1(%d): Go=%d, C=%d", v, got, want)
		}
	}
}

func TestKsclip2(t *testing.T) {
	for v := -112; v <= 112; v++ {
		got := dsp.Ksclip2(v)
		want := cliptables.CKsclip2(v)
		if got != want {
			t.Fatalf("Ksclip2(%d): Go=%d, C=%d", v, got, want)
		}
	}
}

func TestKclip1(t *testing.T) {
	for v := -255; v <= 511; v++ {
		got := dsp.Kclip1(v)
		want := cliptables.CKclip1(v)
		if got != want {
			t.Fatalf("Kclip1(%d): Go=%d, C=%d", v, got, want)
		}
	}
}

func TestKabs0(t *testing.T) {
	for v := -255; v <= 255; v++ {
		got := dsp.Kabs0(v)
		want := cliptables.CKabs0(v)
		if got != want {
			t.Fatalf("Kabs0(%d): Go=%d, C=%d", v, got, want)
		}
	}
}

func TestClip8b(t *testing.T) {
	for v := -1000; v <= 1000; v++ {
		got := dsp.Clip8b(v)
		want := cliptables.CClip8b(v)
		if got != want {
			t.Fatalf("Clip8b(%d): Go=%d, C=%d", v, got, want)
		}
	}
}
