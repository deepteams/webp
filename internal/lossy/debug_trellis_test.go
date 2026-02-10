package lossy

import (
	"fmt"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
)

func TestDebugTrellisRegression(t *testing.T) {
	img := gradientImage(64, 64)

	for _, m := range []int{3, 4} {
		cfg := DefaultConfig(75)
		cfg.Method = m
		enc := NewEncoder(img, cfg)
		data, err := enc.EncodeFrame()
		if err != nil {
			t.Fatalf("Method %d: %v", m, err)
		}

		i16 := 0
		i4 := 0
		skip := 0
		totalNzY := 0
		totalNzUV := 0
		for _, info := range enc.mbInfo {
			if info.MBType == 0 {
				i16++
			} else {
				i4++
			}
			if info.Skip {
				skip++
			}
			for bit := 0; bit < 25; bit++ {
				if info.NonZeroY&(1<<uint(bit)) != 0 {
					totalNzY++
				}
			}
			for bit := 0; bit < 8; bit++ {
				if info.NonZeroUV&(1<<uint(bit)) != 0 {
					totalNzUV++
				}
			}
		}
		fmt.Printf("Method %d: I16=%d I4=%d Skip=%d NzY=%d NzUV=%d Size=%d\n",
			m, i16, i4, skip, totalNzY, totalNzUV, len(data))
	}
}

func TestDebugTrellisBlock(t *testing.T) {
	// Test what trellis does to a single block compared to QuantizeCoeffs.
	img := gradientImage(16, 16)
	cfg := DefaultConfig(75)
	cfg.Method = 4
	enc := NewEncoder(img, cfg)
	seg := &enc.dqm[0]

	// Initialize iterator and import.
	enc.InitIterator()
	it := &enc.mbIterator
	it.Import(enc)
	it.FillPredictionContext(enc)

	// Generate I16 DC prediction.
	srcY := enc.yuvIn[YOff:]
	predY := enc.yuvOut[YOff:]

	dsp.PredLuma16[DCPred](enc.yuvOut, YOff)

	// Compare for first block.
	var coeffs [16]int16
	dsp.FTransform(srcY[0:], predY[0:], coeffs[:])
	coeffs[0] = 0 // skip DC for I16

	var qNormal [16]int16
	nzNormal := QuantizeCoeffs(coeffs[:], qNormal[:], &seg.Y1, 1)

	var qTrellis [16]int16
	copy(qTrellis[:], coeffs[:])
	nzTrellis := TrellisQuantizeBlock(coeffs[:], qTrellis[:], &seg.Y1, 1, 0, 0, &enc.proba, seg.TLambdaI4)

	fmt.Printf("Segment: Quant=%d DCQuant=%d\n", seg.Y1.Quant, seg.Y1.DCQuant)
	fmt.Printf("Lambda: LambdaI4=%d LambdaI16=%d TLambdaI4=%d TLambdaI16=%d TLambdaUV=%d\n",
		seg.LambdaI4, seg.LambdaI16, seg.TLambdaI4, seg.TLambdaI16, seg.TLambdaUV)
	fmt.Printf("Normal: nz=%d coeffs=%v\n", nzNormal, qNormal)
	fmt.Printf("Trellis: nz=%d coeffs=%v\n", nzTrellis, qTrellis)

	// Count nonzero in each.
	nzCountNormal := 0
	nzCountTrellis := 0
	for i := 0; i < 16; i++ {
		if qNormal[i] != 0 {
			nzCountNormal++
		}
		if qTrellis[i] != 0 {
			nzCountTrellis++
		}
	}
	fmt.Printf("Normal nonzero count: %d, Trellis nonzero count: %d\n", nzCountNormal, nzCountTrellis)
}

func TestDebugMethodComparison(t *testing.T) {
	img := solidImage(64, 64, color.NRGBA{R: 200, G: 100, B: 50, A: 255})

	for _, m := range []int{0, 1, 3, 4} {
		cfg := DefaultConfig(75)
		cfg.Method = m
		enc := NewEncoder(img, cfg)
		data, err := enc.EncodeFrame()
		if err != nil {
			t.Fatalf("Method %d: %v", m, err)
		}

		i16 := 0
		i4 := 0
		skip := 0
		for _, info := range enc.mbInfo {
			if info.MBType == 0 {
				i16++
			} else {
				i4++
			}
			if info.Skip {
				skip++
			}
		}
		fmt.Printf("Solid - Method %d: I16=%d I4=%d Skip=%d Size=%d\n",
			m, i16, i4, skip, len(data))
	}
}

func TestDebugFullImageStats(t *testing.T) {
	f, err := os.Open("../../image_test/test.png")
	if err != nil {
		t.Skip("no test image")
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range []int{1, 3, 4} {
		cfg := DefaultConfig(75)
		cfg.Method = m
		enc := NewEncoder(img, cfg)
		data, _ := enc.EncodeFrame()

		i16 := 0
		i4 := 0
		skip := 0
		for _, info := range enc.mbInfo {
			if info.MBType == 0 {
				i16++
			} else {
				i4++
			}
			if info.Skip {
				skip++
			}
		}
		totalMB := len(enc.mbInfo)
		fmt.Printf("Method %d: I16=%d(%d%%) I4=%d(%d%%) Skip=%d(%d%%) Size=%d\n",
			m, i16, i16*100/totalMB, i4, i4*100/totalMB, skip, skip*100/totalMB, len(data))

		// Print segment stats.
		segCounts := [4]int{}
		for _, info := range enc.mbInfo {
			segCounts[info.Segment]++
		}
		for s := 0; s < 4; s++ {
			if segCounts[s] > 0 {
				fmt.Printf("  Seg %d: %d MBs, Q=%d\n", s, segCounts[s], enc.SegmentQuant(s))
			}
		}
	}
}

func TestDebugSizeBreakdown(t *testing.T) {
	f, err := os.Open("../../image_test/test.png")
	if err != nil {
		t.Skip("no test image")
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range []int{1, 3, 4} {
		cfg := DefaultConfig(75)
		cfg.Method = m
		enc := NewEncoder(img, cfg)
		data, _ := enc.EncodeFrame()
		stats := enc.Stats()
		modesSize := stats.HeaderSize - 10 - stats.ProbaSize()
		fmt.Printf("Method %d: Total=%d Part0=%d (proba=%d modes=%d) Tokens=%d\n",
			m, len(data), stats.HeaderSize, stats.ProbaSize(), modesSize, stats.Residuals)

		// Count non-zero coefficients and token count.
		totalNzCoeffs := 0
		totalTokens := 0
		for _, info := range enc.mbInfo {
			if info.Skip {
				continue
			}
			if info.MBType == 0 {
				// I16: 16 AC blocks + 1 DC block
				// DC block at 384-399
				for n := 0; n < 16; n++ {
					if info.Coeffs[384+n] != 0 {
						totalNzCoeffs++
					}
				}
				// 16 AC blocks
				for b := 0; b < 16; b++ {
					for n := 1; n < 16; n++ { // skip DC at 0
						if info.Coeffs[b*16+n] != 0 {
							totalNzCoeffs++
						}
					}
				}
				totalTokens += 17 // 16 AC + 1 DC
			} else {
				// I4: 16 full blocks
				for b := 0; b < 16; b++ {
					for n := 0; n < 16; n++ {
						if info.Coeffs[b*16+n] != 0 {
							totalNzCoeffs++
						}
					}
				}
				totalTokens += 16
			}
			// UV: 8 blocks
			for b := 16; b < 24; b++ {
				for n := 0; n < 16; n++ {
					if info.Coeffs[b*16+n] != 0 {
						totalNzCoeffs++
					}
				}
			}
			totalTokens += 8
		}
		notSkip := 0
		for _, info := range enc.mbInfo {
			if !info.Skip {
				notSkip++
			}
		}
		fmt.Printf("  NonSkip=%d NzCoeffs=%d TokenBlocks=%d AvgNzPerBlock=%.1f\n",
			notSkip, totalNzCoeffs, totalTokens, float64(totalNzCoeffs)/float64(totalTokens))
	}
}

func TestDebugI4CostTable(t *testing.T) {
	// Validate our computed VP8FixedCostsI4 against libwebp's VP8FixedCostsI4.
	// First row from libwebp: VP8FixedCostsI4[0][0] = {40, 1151, 1723, 1874, 2103, 2019, 1628, 1777, 2226, 2137}
	expected := [NumBModes]uint16{40, 1151, 1723, 1874, 2103, 2019, 1628, 1777, 2226, 2137}
	for mode := 0; mode < NumBModes; mode++ {
		got := VP8FixedCostsI4[0][0][mode]
		if got != expected[mode] {
			t.Errorf("VP8FixedCostsI4[0][0][%d] = %d, want %d", mode, got, expected[mode])
		}
	}

	// Second row: VP8FixedCostsI4[0][1] = {192, 469, 1296, 1308, 1849, 1794, 1781, 1703, 1713, 1522}
	expected2 := [NumBModes]uint16{192, 469, 1296, 1308, 1849, 1794, 1781, 1703, 1713, 1522}
	for mode := 0; mode < NumBModes; mode++ {
		got := VP8FixedCostsI4[0][1][mode]
		if got != expected2[mode] {
			t.Errorf("VP8FixedCostsI4[0][1][%d] = %d, want %d", mode, got, expected2[mode])
		}
	}

	// Last row: VP8FixedCostsI4[9][9] = {305, 1167, 1358, 899, 1587, 1587, 987, 1988, 1332, 501}
	expected3 := [NumBModes]uint16{305, 1167, 1358, 899, 1587, 1587, 987, 1988, 1332, 501}
	for mode := 0; mode < NumBModes; mode++ {
		got := VP8FixedCostsI4[9][9][mode]
		if got != expected3[mode] {
			t.Errorf("VP8FixedCostsI4[9][9][%d] = %d, want %d", mode, got, expected3[mode])
		}
	}

	fmt.Printf("VP8FixedCostsI4[0][0]: %v\n", VP8FixedCostsI4[0][0])
}

func TestDebugI4vsI16RDScores(t *testing.T) {
	img := gradientImage(64, 64)
	cfg := DefaultConfig(75)
	cfg.Method = 4
	enc := NewEncoder(img, cfg)

	enc.analysis()
	enc.InitIterator()
	it := &enc.mbIterator
	seg := &enc.dqm[enc.mbInfo[0].Segment]

	// Process first MB.
	it.Import(enc)
	it.FillPredictionContext(enc)

	// I16 RD.
	bestMode16, _, rate16, disto16 := enc.PickBestI16ModeRD(it, seg)
	score16mode := RDScore(disto16, rate16, seg.LambdaMode)
	score16own := RDScore(disto16, rate16, seg.LambdaI16)

	fmt.Printf("I16: mode=%d rate=%d disto=%d score_lambdaMode=%d score_lambdaI16=%d\n",
		bestMode16, rate16, disto16, score16mode, score16own)

	// I4 RD.
	var modes4 [16]uint8
	info := &enc.mbInfo[0]
	score4 := enc.tryI4ModesRD(it, info, seg, &modes4, ^uint64(0))

	fmt.Printf("I4: totalScore_lambdaMode=%d\n", score4)
	fmt.Printf("LambdaMode=%d LambdaI4=%d LambdaI16=%d LambdaUV=%d\n",
		seg.LambdaMode, seg.LambdaI4, seg.LambdaI16, seg.LambdaUV)

	fmt.Printf("Decision: ")
	if score4 < score16mode {
		fmt.Printf("I4 WINS (by %d)\n", score16mode-score4)
	} else {
		fmt.Printf("I16 WINS (by %d)\n", score4-score16mode)
	}
}
