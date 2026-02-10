package lossy

import (
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"os"
	"os/exec"
	"testing"
)

func TestEncodeNoiseSizes(t *testing.T) {
	sizes := [][2]int{{32, 32}, {48, 32}, {64, 64}, {128, 128}}

	for _, segs := range []int{1, 4} {
		for _, sz := range sizes {
			w, h := sz[0], sz[1]
			name := fmt.Sprintf("%dx%d_s%d", w, h, segs)
			t.Run(name, func(t *testing.T) {
				img := image.NewNRGBA(image.Rect(0, 0, w, h))
				rng := rand.New(rand.NewSource(42))
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						img.SetNRGBA(x, y, color.NRGBA{
							R: uint8(rng.Intn(256)),
							G: uint8(rng.Intn(256)),
							B: uint8(rng.Intn(256)),
							A: 255,
						})
					}
				}

				cfg := DefaultConfig(75)
				cfg.Segments = segs
				cfg.Method = 0
				cfg.Pass = 1
				enc := NewEncoder(img, cfg)
				bs, err := enc.EncodeFrame()
				if err != nil {
					t.Fatalf("Encode error: %v", err)
				}
				riff := AssembleRIFF(bs)
				fname := fmt.Sprintf("/tmp/noise_%s.webp", name)
				os.WriteFile(fname, riff, 0644)
				fmt.Printf("Encoded %s: %d bytes\n", name, len(riff))

				cmd := exec.Command("dwebp", fname, "-pam", "-o", fname+".pam")
				out, dErr := cmd.CombinedOutput()
				if dErr != nil {
					t.Errorf("dwebp failed: %v\nOutput: %s", dErr, string(out))
				} else {
					fmt.Printf("dwebp %s: OK\n", name)
				}
			})
		}
	}
}
