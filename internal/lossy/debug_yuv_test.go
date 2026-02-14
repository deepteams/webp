package lossy

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
)

func TestDebugUVPipeline(t *testing.T) {
	// Create a 16x16 solid red image.
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}

	cfg := DefaultConfig(75)
	q := qualityToQIndex(75)
	fmt.Printf("Quality 75 → q index = %d\n", q)
	fmt.Printf("Y1: DC=%d AC=%d\n", KDcTable[q], KAcTable[q])
	fmt.Printf("Y2: DC=%d AC=%d\n", int(KDcTable[q])*2, (int(KAcTable[q])*101581)>>16)
	fmt.Printf("UV: DC=%d AC=%d\n", KDcTable[clampInt(q, 0, 117)], KAcTable[q])

	enc := NewEncoder(img, cfg)
	fmt.Printf("\nEncoder seg[0]: Y1.DCQuant=%d Y1.Quant=%d Y2.DCQuant=%d Y2.Quant=%d UV.DCQuant=%d UV.Quant=%d\n",
		enc.dqm[0].Y1.DCQuant, enc.dqm[0].Y1.Quant,
		enc.dqm[0].Y2.DCQuant, enc.dqm[0].Y2.Quant,
		enc.dqm[0].UV.DCQuant, enc.dqm[0].UV.Quant)

	// Full encode.
	bs, err := enc.EncodeFrame()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("Bitstream size: %d bytes\n", len(bs))

	// Save to tmp file for dwebp verification.
	riff := AssembleRIFF(bs)
	tmpFile := "/tmp/test_red16.webp"
	os.WriteFile(tmpFile, riff, 0644)

	// Try dwebp to decode (skip if not installed).
	if _, lookErr := exec.LookPath("dwebp"); lookErr != nil {
		t.Log("dwebp not found, skipping dwebp verification")
	} else {
		dwebpOut := "/tmp/test_red16_dwebp.pam"
		cmd := exec.Command("dwebp", tmpFile, "-pam", "-o", dwebpOut)
		out, dwebpErr := cmd.CombinedOutput()
		if dwebpErr != nil {
			fmt.Printf("dwebp error: %v\nOutput: %s\n", dwebpErr, string(out))
		} else {
			fmt.Printf("dwebp decoded successfully to %s\n", dwebpOut)
			pam, _ := os.ReadFile(dwebpOut)
			if len(pam) > 0 {
				fmt.Printf("dwebp PAM first 200 bytes: %q\n", string(pam[:min(200, len(pam))]))
			}
		}
	}

	// Our decoder.
	_, w, h, yP, _, uP, vP, _, err := DecodeFrame(bs)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	fmt.Printf("\nOur decoder: %dx%d\n", w, h)
	fmt.Printf("Y[0]=%d U[0]=%d V[0]=%d\n", yP[0], uP[0], vP[0])
	r := dsp.YUVToR(int(yP[0]), int(vP[0]))
	g := dsp.YUVToG(int(yP[0]), int(uP[0]), int(vP[0]))
	b := dsp.YUVToB(int(yP[0]), int(uP[0]))
	fmt.Printf("RGB: R=%d G=%d B=%d\n", r, g, b)

	// Also trace encoder WHT coefficients.
	enc2 := NewEncoder(img, cfg)
	enc2.analysis()
	enc2.InitIterator()
	it := &enc2.mbIterator
	info := &enc2.mbInfo[0]
	seg := &enc2.dqm[info.Segment]
	it.Import(enc2)
	it.FillPredictionContext(enc2)
	enc2.pickBestMode(it, info, seg)
	enc2.encodeResiduals(it, info, seg)

	fmt.Printf("\nEncoder WHT quantized: [")
	for i := 0; i < 16; i++ {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%d", info.Coeffs[384+i])
	}
	fmt.Printf("]\n")

	// Check Y AC blocks.
	fmt.Printf("Y AC blocks (first 4):\n")
	for b := 0; b < 4; b++ {
		off := b * 16
		fmt.Printf("  Block %d DC=%d AC[1]=%d\n", b, info.Coeffs[off], info.Coeffs[off+1])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
