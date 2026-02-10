//go:build testc

package cost_test

import (
	"testing"

	"github.com/deepteams/webp/internal/dsp"
	"github.com/deepteams/webp/testc/cost"
)

func init() {
	cost.Init()
}

func TestVP8EntropyCost(t *testing.T) {
	for i := 0; i < 256; i++ {
		got := dsp.VP8EntropyCost[i]
		want := cost.CEntropyCost(i)
		if got != want {
			t.Fatalf("VP8EntropyCost[%d]: Go=%d, C=%d", i, got, want)
		}
	}
}

func TestVP8LevelFixedCosts(t *testing.T) {
	for i := 0; i < 2048; i++ {
		got := dsp.VP8LevelFixedCosts[i]
		want := cost.CLevelFixedCost(i)
		if got != want {
			t.Fatalf("VP8LevelFixedCosts[%d]: Go=%d, C=%d", i, got, want)
		}
	}
}

func TestVP8EncBands(t *testing.T) {
	for i := 0; i < 17; i++ {
		got := dsp.VP8EncBands[i]
		want := int(cost.CEncBands(i))
		if got != want {
			t.Fatalf("VP8EncBands[%d]: Go=%d, C=%d", i, got, want)
		}
	}
}

func TestVP8BitCost(t *testing.T) {
	for bit := 0; bit <= 1; bit++ {
		for prob := 0; prob <= 255; prob++ {
			got := dsp.VP8BitCost(bit, uint8(prob))
			// Manual computation from entropy cost table, same as C VP8BitCost.
			var want int
			if bit == 0 {
				want = int(cost.CEntropyCost(prob))
			} else {
				want = int(cost.CEntropyCost(255 - prob))
			}
			if got != want {
				t.Fatalf("VP8BitCost(%d, %d): Go=%d, C=%d", bit, prob, got, want)
			}
		}
	}
}

func TestVP8LevelCost(t *testing.T) {
	for level := 0; level < 2048; level++ {
		got := dsp.VP8LevelCost(dsp.VP8LevelFixedCosts[:], level)
		want := int(cost.CLevelFixedCost(level))
		if got != want {
			t.Fatalf("VP8LevelCost(%d): Go=%d, C=%d", level, got, want)
		}
	}
}
