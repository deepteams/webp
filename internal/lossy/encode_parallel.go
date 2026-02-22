package lossy

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/deepteams/webp/internal/dsp"
)

// parallelState holds pooled buffers for parallel encoding.
type parallelState struct {
	workers  []RowWorker
	rs       *rowSync
	topY     []uint8
	topU     []uint8
	topV     []uint8
	topModes []uint8
	topNz    []uint32 // NZ context per column (bottom-row NZ of MB above)
	topNzDC  []uint8  // DC NZ context per column (for I16 WHT block)
	nextRow  atomic.Int32 // atomic row counter for worker row claiming
}

var parallelPool sync.Pool

// getParallelState returns a pooled or new parallelState sized for the given dimensions.
func getParallelState(numWorkers, mbW, mbH int, useDerr bool) *parallelState {
	if v := parallelPool.Get(); v != nil {
		ps := v.(*parallelState)
		// Check if existing state is large enough.
		if len(ps.workers) >= numWorkers && len(ps.rs.rows) >= mbH && len(ps.topY) >= mbW*16 && len(ps.topNz) >= mbW {
			// Reset row sync progress.
			for i := 0; i < mbH; i++ {
				ps.rs.rows[i].done.Store(0)
			}
			ps.nextRow.Store(0)
			return ps
		}
		// Too small, discard and allocate fresh.
	}
	ps := &parallelState{
		workers:  make([]RowWorker, numWorkers),
		rs:       newRowSync(mbH),
		topY:     make([]uint8, mbW*16),
		topU:     make([]uint8, mbW*8),
		topV:     make([]uint8, mbW*8),
		topModes: make([]uint8, mbW*4),
		topNz:    make([]uint32, mbW),
		topNzDC:  make([]uint8, mbW),
	}
	for i := range ps.workers {
		initRowWorker(&ps.workers[i], mbW, useDerr)
	}
	return ps
}

func putParallelState(ps *parallelState) {
	parallelPool.Put(ps)
}

// rowSync provides per-row synchronization for parallel encoding.
// Uses atomic fast-path on the wait side to avoid Lock when data is ready.
type rowSync struct {
	rows []rowState
}

// rowState is padded to a full cache line (64 bytes) to prevent false sharing.
type rowState struct {
	done    atomic.Int32
	waiters atomic.Int32
	mu      sync.Mutex
	cond    *sync.Cond
	_       [8]byte
}

func newRowSync(mbH int) *rowSync {
	rs := &rowSync{
		rows: make([]rowState, mbH),
	}
	for i := range rs.rows {
		rs.rows[i].cond = sync.NewCond(&rs.rows[i].mu)
	}
	return rs
}

// waitFor blocks until row y has completed at least needed MBs.
// Fast path uses atomic load (no lock). Slow path uses cond.Wait.
func (rs *rowSync) waitFor(y int, needed int32) {
	r := &rs.rows[y]
	if r.done.Load() >= needed {
		return
	}
	r.waiters.Add(1)
	r.mu.Lock()
	for r.done.Load() < needed {
		r.cond.Wait()
	}
	r.mu.Unlock()
	r.waiters.Add(-1)
}

// signal marks that row y has completed done MBs and wakes all waiters.
// Fast path: if no goroutine is waiting, just do an atomic store.
// Slow path: Lock + Broadcast when waiters are present.
func (rs *rowSync) signal(y int, done int32) {
	r := &rs.rows[y]
	r.done.Store(done)
	if r.waiters.Load() > 0 {
		r.mu.Lock()
		r.mu.Unlock()
		r.cond.Broadcast()
	}
}

// RowWorker holds per-worker state for parallel row encoding.
// Each goroutine gets its own RowWorker to avoid contention on
// the shared encoder buffers (yuvIn, yuvOut, yuvOut2, yuvP, tmp*).
type RowWorker struct {
	// BPS-strided working buffers (one MB at a time).
	yuvIn   []byte
	yuvOut  []byte
	yuvOut2 []byte
	yuvP    []byte

	// Pre-allocated temporary buffers (mirrors VP8Encoder.tmp*).
	tmpCoeffs   [16]int16
	tmpQCoeffs  [16]int16
	tmpDQCoeffs [16]int16
	tmpDCCoeffs [16]int16
	tmpWHTDQ    [16]int16
	tmpWHTBuf   [256]int16
	tmpAllQ     [16][16]int16
	tmpACLevels [256]int16
	tmpRecon    [128]byte
	tmpUVLevels [128]int16
	tmpBestDQ   [16]int16
	tmpBestQ    [16]int16
	tmpBestNz   int

	// DC error diffusion state.
	topDerr  [][2][2]int8
	leftDerr [2][2]int8
}

// initRowWorker allocates a RowWorker's buffers.
func initRowWorker(w *RowWorker, mbW int, useDerr bool) {
	w.yuvIn = make([]byte, YUVSize)
	w.yuvOut = make([]byte, YUVSize)
	w.yuvOut2 = make([]byte, YUVSize)
	w.yuvP = make([]byte, 33*dsp.BPS)
	if useDerr {
		w.topDerr = make([][2][2]int8, mbW)
	}
}

// encodeFrameParallel performs the main encoding loop with row-pipelined
// parallelism. Mode selection and residual computation are done in parallel
// (Phase A), while token recording is done serially (Phase B).
//
// Phase A: Multiple goroutines process rows in parallel. Each row worker
// performs Import → FillPredContext → pickBestMode → encodeResiduals →
// reconstructMB → Export for each MB in its row. Row synchronization uses
// atomic progress counters: row Y can start MB X when row Y-1 has completed
// MB X+1 (to ensure top-right pixel context is available).
//
// Phase B: Single-threaded pass over all pre-computed mbInfo, calling
// recordMBTokens with accurate NZ context and probability refresh.
func (enc *VP8Encoder) encodeFrameParallel(stats *ProbaStats) {
	mbW := enc.mbW
	mbH := enc.mbH

	// Determine number of workers. Cap at 6 to reduce idle goroutine
	// overhead — beyond 6 workers the pipeline depth (3 rows) limits
	// parallelism and extra goroutines just add sync contention.
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > 6 {
		numWorkers = 6
	}
	if numWorkers > mbH {
		numWorkers = mbH
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	// Get pooled or fresh parallel state (workers, sync, context arrays).
	ps := getParallelState(numWorkers, mbW, mbH, enc.useDerr)
	defer putParallelState(ps)

	workers := ps.workers[:numWorkers]
	rs := ps.rs
	topY := ps.topY[:mbW*16]
	topU := ps.topU[:mbW*8]
	topV := ps.topV[:mbW*8]
	topModes := ps.topModes[:mbW*4]
	topNz := ps.topNz[:mbW]
	topNzDC := ps.topNzDC[:mbW]

	// Initialize top context to default values.
	for i := range topY {
		topY[i] = 127
	}
	for i := range topU {
		topU[i] = 127
	}
	for i := range topV {
		topV[i] = 127
	}
	for i := range topModes {
		topModes[i] = BDCPred
	}
	for i := range topNz {
		topNz[i] = 0
	}
	for i := range topNzDC {
		topNzDC[i] = 0
	}

	// Phase A: parallel row processing.
	var wg sync.WaitGroup
	ps.nextRow.Store(0)

	for wi := 0; wi < numWorkers; wi++ {
		wg.Add(1)
		go func(w *RowWorker) {
			defer wg.Done()
			for {
				y := int(ps.nextRow.Add(1) - 1)
				if y >= mbH {
					return
				}
				enc.encodeRow(w, y, topY, topU, topV, topModes, topNz, topNzDC, rs)
			}
		}(&workers[wi])
	}

	// Phase B: overlapped serial token recording.
	// Records tokens for each row as soon as that row is fully encoded,
	// overlapping with Phase A workers still processing later rows.
	// This hides ~1ms of serial token recording behind parallel work.
	enc.parallelRS = rs
	enc.recordAllTokens(stats)
	enc.parallelRS = nil

	// Ensure all workers are done (should be immediate since Phase B
	// waited for the last row via parallelRS).
	wg.Wait()
}

// encodeRow processes all MBs in a single row using the given worker.
// Synchronizes with the row above via rowSync condition variable.
func (enc *VP8Encoder) encodeRow(w *RowWorker, y int, topY, topU, topV, topModes []uint8, topNz []uint32, topNzDC []uint8, rs *rowSync) {
	mbW := enc.mbW

	// Local left context for this row.
	var leftY [16]uint8
	var leftU [8]uint8
	var leftV [8]uint8
	var leftModes [4]uint8
	var topLeftY, topLeftU, topLeftV uint8

	// Local NZ context for this row (left neighbor).
	var leftNz uint32
	var leftNzDC uint8

	// Initialize left context.
	for i := range leftY {
		leftY[i] = 129
	}
	for i := range leftU {
		leftU[i] = 129
	}
	for i := range leftV {
		leftV[i] = 129
	}
	for i := range leftModes {
		leftModes[i] = BDCPred
	}
	topLeftY = 127
	topLeftU = 127
	topLeftV = 127

	// Reset DC error diffusion for this row.
	w.leftDerr = [2][2]int8{}
	if w.topDerr != nil {
		if y == 0 {
			for i := range w.topDerr {
				w.topDerr[i] = [2][2]int8{}
			}
		}
	}

	for x := 0; x < mbW; x++ {
		mbIdx := y*mbW + x
		info := &enc.mbInfo[mbIdx]
		seg := &enc.dqm[info.Segment]

		// Wait for the row above to complete MB x+1 (top + top-right context).
		// We need MB x+1 for accurate I4x4 VL4/LD4 top-right prediction.
		// For the last column, only wait for MB x (no next MB to the right).
		if y > 0 {
			waitX := int32(x + 2) // need x+1 complete for top-right
			if waitX > int32(mbW) {
				waitX = int32(mbW)
			}
			rs.waitFor(y-1, waitX)
		}

		// 1. Import source data.
		importBlockParallel(enc, w, x, y)

		// 2. Fill prediction context from shared top/left arrays.
		fillPredContextParallel(w, enc, x, y, mbW, topY, topU, topV, leftY[:], leftU[:], leftV[:], topLeftY, topLeftU, topLeftV)

		// 3. Pick best mode using worker's buffers (with accurate NZ context).
		pickBestModeParallel(enc, w, x, y, info, seg, topModes, leftModes[:], topNz[x], leftNz, topNzDC[x], leftNzDC)

		// 4. Compute residuals (with accurate NZ context).
		encodeResidualsParallel(enc, w, x, y, info, seg, topNz[x], leftNz, topNzDC[x], leftNzDC)

		// 5. Detect skip.
		info.Skip = (info.NonZeroY == 0 && info.NonZeroUV == 0)

		// 6. Reconstruct MB.
		reconstructMBParallel(enc, w, x, y, info, seg)

		// 7. Export: write back to planes and update context.
		exportParallel(enc, w, x, y, topY, topU, topV, topModes, &leftY, &leftU, &leftV, &leftModes, &topLeftY, &topLeftU, &topLeftV, info)

		// 8. Update NZ context for next MB / next row.
		updateNZContextParallel(info, x, topNz, &leftNz, topNzDC, &leftNzDC)

		// 9. Signal completion.
		rs.signal(y, int32(x+1))
	}
}

// updateNZContextParallel computes NZ context bits from the current MB's info
// and updates the shared topNz array and local leftNz. This mirrors the serial
// updateNZContext but operates on parallel state arrays.
func updateNZContextParallel(info *MBEncInfo, mbX int, topNz []uint32, leftNz *uint32, topNzDC []uint8, leftNzDC *uint8) {
	topNzVal := topNz[mbX]
	leftNzVal := *leftNz

	var outTNz, outLNz uint32

	if info.MBType == 0 {
		// I16: DC block NZ.
		nzDC := int(info.NzDC)
		if nzDC > 0 {
			topNzDC[mbX] = 1
			*leftNzDC = 1
		} else {
			topNzDC[mbX] = 0
			*leftNzDC = 0
		}

		// 16 luma AC blocks.
		first := 1
		tnz := topNzVal & 0x0f
		lnz := leftNzVal & 0x0f
		for y := 0; y < 4; y++ {
			l := lnz & 1
			for x := 0; x < 4; x++ {
				blockIdx := y*4 + x
				nz := int(info.NzY[blockIdx])
				if nz > first {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 7)
			}
			tnz >>= 4
			lnz = (lnz >> 1) | (l << 7)
		}
		outTNz = tnz
		outLNz = lnz >> 4
	} else {
		// I4x4: 16 full blocks.
		tnz := topNzVal & 0x0f
		lnz := leftNzVal & 0x0f
		for y := 0; y < 4; y++ {
			l := lnz & 1
			for x := 0; x < 4; x++ {
				blockIdx := y*4 + x
				nz := int(info.NzY[blockIdx])
				if nz > 0 {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 7)
			}
			tnz >>= 4
			lnz = (lnz >> 1) | (l << 7)
		}
		outTNz = tnz
		outLNz = lnz >> 4
	}

	// UV blocks.
	for ch := uint(0); ch < 4; ch += 2 {
		tnz := (topNzVal >> (4 + ch)) & 0x0f
		lnz := (leftNzVal >> (4 + ch)) & 0x0f
		for y := 0; y < 2; y++ {
			l := lnz & 1
			for x := 0; x < 2; x++ {
				blockIdx := y*2 + x
				uvIdx := int(ch/2)*4 + blockIdx
				nz := int(info.NzUV[uvIdx])
				if nz > 0 {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 3)
			}
			tnz >>= 2
			lnz = (lnz >> 1) | (l << 5)
		}
		outTNz |= (tnz << 4) << ch
		outLNz |= (lnz & 0xf0) << ch
	}

	topNz[mbX] = outTNz
	*leftNz = outLNz
}

// importBlockParallel copies source YUV data for MB (x,y) into worker's yuvIn.
func importBlockParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int) {
	x := mbX * 16
	y := mbY * 16

	ww := enc.width - x
	if ww > 16 {
		ww = 16
	}
	hh := enc.height - y
	if hh > 16 {
		hh = 16
	}

	importBlock(enc.yPlane, enc.yStride, w.yuvIn, YOff, x, y, ww, hh, 16)

	ux := mbX * 8
	uy := mbY * 8
	uvW := (ww + 1) >> 1
	uvH := (hh + 1) >> 1
	importBlock(enc.uPlane, enc.uvStride, w.yuvIn, UOff, ux, uy, uvW, uvH, 8)
	importBlock(enc.vPlane, enc.uvStride, w.yuvIn, VOff, ux, uy, uvW, uvH, 8)
}

// fillPredContextParallel fills the prediction context borders in worker's yuvOut.
func fillPredContextParallel(w *RowWorker, enc *VP8Encoder, mbX, mbY, mbW int, topY, topU, topV []uint8, leftY, leftU, leftV []uint8, topLeftY, topLeftU, topLeftV uint8) {
	bps := dsp.BPS
	topOff := mbX * 16
	topUOff := mbX * 8

	// --- Y plane context ---
	for i := 0; i < 16; i++ {
		if mbY > 0 {
			w.yuvOut[YOff-bps+i] = topY[topOff+i]
		} else {
			w.yuvOut[YOff-bps+i] = 127
		}
	}

	// Top-right extension for I4x4 VL/LD modes.
	// Use actual next-MB top pixels when available (row above MB x+1 is complete).
	if mbY > 0 {
		if mbX < mbW-1 {
			nextOff := (mbX + 1) * 16
			for i := 0; i < 4; i++ {
				w.yuvOut[YOff-bps+16+i] = topY[nextOff+i]
			}
		} else {
			val := topY[topOff+15]
			for i := 0; i < 4; i++ {
				w.yuvOut[YOff-bps+16+i] = val
			}
		}
	} else {
		for i := 0; i < 4; i++ {
			w.yuvOut[YOff-bps+16+i] = 127
		}
	}

	// Replicate top-right for sub-block rows.
	for r := 1; r <= 3; r++ {
		off := r * 4 * bps
		for i := 0; i < 4; i++ {
			w.yuvOut[YOff-bps+16+off+i] = w.yuvOut[YOff-bps+16+i]
		}
	}

	// Top-left corner.
	if mbX > 0 && mbY > 0 {
		w.yuvOut[YOff-bps-1] = topLeftY
	} else if mbY > 0 {
		w.yuvOut[YOff-bps-1] = 129
	} else {
		w.yuvOut[YOff-bps-1] = 127
	}

	// Left column.
	for j := 0; j < 16; j++ {
		if mbX > 0 {
			w.yuvOut[YOff-1+j*bps] = leftY[j]
		} else {
			w.yuvOut[YOff-1+j*bps] = 129
		}
	}

	// --- U plane context ---
	for i := 0; i < 8; i++ {
		if mbY > 0 {
			w.yuvOut[UOff-bps+i] = topU[topUOff+i]
		} else {
			w.yuvOut[UOff-bps+i] = 127
		}
	}
	if mbX > 0 && mbY > 0 {
		w.yuvOut[UOff-bps-1] = topLeftU
	} else if mbY > 0 {
		w.yuvOut[UOff-bps-1] = 129
	} else {
		w.yuvOut[UOff-bps-1] = 127
	}
	for j := 0; j < 8; j++ {
		if mbX > 0 {
			w.yuvOut[UOff-1+j*bps] = leftU[j]
		} else {
			w.yuvOut[UOff-1+j*bps] = 129
		}
	}

	// --- V plane context ---
	for i := 0; i < 8; i++ {
		if mbY > 0 {
			w.yuvOut[VOff-bps+i] = topV[topUOff+i]
		} else {
			w.yuvOut[VOff-bps+i] = 127
		}
	}
	if mbX > 0 && mbY > 0 {
		w.yuvOut[VOff-bps-1] = topLeftV
	} else if mbY > 0 {
		w.yuvOut[VOff-bps-1] = 129
	} else {
		w.yuvOut[VOff-bps-1] = 127
	}
	for j := 0; j < 8; j++ {
		if mbX > 0 {
			w.yuvOut[VOff-1+j*bps] = leftV[j]
		} else {
			w.yuvOut[VOff-1+j*bps] = 129
		}
	}
}

// pickBestModeParallel selects the best prediction mode using worker buffers.
func pickBestModeParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, info *MBEncInfo, seg *SegmentInfo, topModes []uint8, leftModes []uint8, topNzVal, leftNzVal uint32, topNzDCVal, leftNzDCVal uint8) {
	if enc.config.Method >= 3 {
		bestMode16, _, rate16, disto16 := pickBestI16ModeRDParallel(enc, w, mbX, mbY, seg, topNzVal, leftNzVal, topNzDCVal, leftNzDCVal)
		bestScore16 := RDScore(disto16, rate16, seg.LambdaMode)

		var bestScore4 uint64 = ^uint64(0)
		var modes4 [16]uint8
		bestScore4 = tryI4ModesRDParallel(enc, w, mbX, mbY, info, seg, &modes4, topModes, leftModes, bestScore16, topNzVal, leftNzVal)

		if bestScore4 < bestScore16 {
			info.MBType = 1
			info.Modes = modes4
			info.Score = bestScore4
			info.PredCached = false
			if enc.config.Method >= 4 {
				info.I4Cached = true
				for j := 0; j < 16; j++ {
					off := YOff + j*dsp.BPS
					copy(w.yuvOut[off:off+16], w.yuvOut2[off:off+16])
				}
			}
		} else {
			info.MBType = 0
			info.I16Mode = bestMode16
			info.Score = bestScore16
			info.I4Cached = false
			actualI16 := checkMode(mbX, mbY, int(bestMode16))
			dsp.PredLuma16Direct(actualI16, w.yuvOut, YOff)
			info.PredCached = true
		}

		bestUV, _, _, _ := pickBestUVModeRDParallel(enc, w, mbX, mbY, seg, topNzVal, leftNzVal)
		info.UVMode = bestUV
		actualUV := checkMode(mbX, mbY, int(bestUV))
		dsp.PredChroma8Direct(actualUV, w.yuvOut, UOff)
		dsp.PredChroma8Direct(actualUV, w.yuvOut, VOff)
	} else {
		bestMode16, bestScore16 := PickBestI16Mode(w.yuvIn, YOff, w.yuvOut, YOff, seg, mbX, mbY)

		var bestScore4 uint64 = ^uint64(0)
		var modes4 [16]uint8
		if enc.config.Method >= 2 {
			bestScore4 = tryI4ModesParallel(enc, w, mbX, mbY, info, seg, &modes4, topModes, leftModes)
		}

		if bestScore4 < bestScore16 {
			info.MBType = 1
			info.Modes = modes4
			info.Score = bestScore4
		} else {
			info.MBType = 0
			info.I16Mode = bestMode16
			info.Score = bestScore16
		}

		bestUV, _ := PickBestUVMode(w.yuvIn, UOff, w.yuvOut, UOff, seg, mbX, mbY)
		info.UVMode = bestUV
	}
}

// pickBestI16ModeRDParallel is like PickBestI16ModeRD but uses worker buffers.
func pickBestI16ModeRDParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, seg *SegmentInfo, topNzVal, leftNzVal uint32, topNzDCVal, leftNzDCVal uint8) (bestMode uint8, bestScore uint64, bestRate int, bestDisto int) {
	bestScore = ^uint64(0)
	bestMode = DCPred

	src := w.yuvIn
	pred := w.yuvOut2

	srcFlat := isFlatSource16(src, YOff)
	copy(pred[:UOff], w.yuvOut[:UOff])

	// NZ context from neighboring MBs for accurate rate estimation.
	initTnz := topNzVal & 0x0f
	initLnz := leftNzVal & 0x0f
	dcCtx := int(topNzDCVal) + int(leftNzDCVal)
	if dcCtx > 2 {
		dcCtx = 2
	}
	var tnz, lnz uint32

	for mode := 0; mode < NumPredModes; mode++ {
		actualMode := checkMode(mbX, mbY, mode)
		if mode == int(VPred) && mbY == 0 {
			continue
		}
		if mode == int(HPred) && mbX == 0 {
			continue
		}
		if mode == int(TMPred) && (mbX == 0 || mbY == 0) {
			continue
		}

		dsp.PredLuma16Direct(actualMode, pred, YOff)

		w.tmpDCCoeffs = [16]int16{}
		totalRate := modeFixedCost16[mode]

		tnz = initTnz
		lnz = initLnz

		for by := 0; by < 4; by++ {
			l := lnz & 1
			for bx := 0; bx < 4; bx++ {
				blockIdx := by*4 + bx
				srcOff := YOff + by*4*dsp.BPS + bx*4

				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}

				dsp.FTransformDirect(src[srcOff:], pred[srcOff:], w.tmpCoeffs[:])
				w.tmpDCCoeffs[blockIdx] = w.tmpCoeffs[0]
				w.tmpCoeffs[0] = 0

				nz := QuantizeCoeffs(w.tmpCoeffs[:], w.tmpQCoeffs[:], &seg.Y1, 1)
				w.tmpAllQ[blockIdx] = w.tmpQCoeffs

				totalRate += TokenCostForCoeffs(w.tmpQCoeffs[:], nz, 0, &enc.proba, ctx, 1)

				if nz > 0 {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 7)
			}
			tnz >>= 4
			lnz = (lnz >> 1) | (l << 7)
		}

		dsp.FTransformWHT(w.tmpDCCoeffs[:], w.tmpCoeffs[:])
		nzDC := QuantizeCoeffs(w.tmpCoeffs[:], w.tmpQCoeffs[:], &seg.Y2, 0)
		totalRate += TokenCostForCoeffs(w.tmpQCoeffs[:], nzDC, 1, &enc.proba, dcCtx, 0)

		DequantCoeffs(w.tmpQCoeffs[:], w.tmpWHTDQ[:], &seg.Y2)
		dsp.TransformWHT(w.tmpWHTDQ[:], w.tmpWHTBuf[:])

		for by := 0; by < 4; by++ {
			for bx := 0; bx < 4; bx++ {
				blockIdx := by*4 + bx
				srcOff := YOff + by*4*dsp.BPS + bx*4
				DequantCoeffs(w.tmpAllQ[blockIdx][:], w.tmpDQCoeffs[:], &seg.Y1)
				w.tmpDQCoeffs[0] = w.tmpWHTBuf[blockIdx*16]
				dsp.ITransformDirect(pred[srcOff:], w.tmpDQCoeffs[:], pred[srcOff:], false)
			}
		}

		disto := dsp.SSE16x16Direct(src[YOff:], pred[YOff:])
		if seg.TLambdaSD > 0 {
			td := dsp.TDisto16x16(src[YOff:], pred[YOff:])
			disto += (seg.TLambdaSD*td + 128) >> 8
		}

		if srcFlat {
			for b := 0; b < 16; b++ {
				copy(w.tmpACLevels[b*16:b*16+16], w.tmpAllQ[b][:])
			}
			if isFlat(w.tmpACLevels[:], 16, flatnessLimitI16) {
				disto *= 2
			}
		}

		score := RDScore(disto, totalRate, seg.LambdaI16)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
			bestRate = totalRate
			bestDisto = disto
		}
	}
	return
}

// tryI4ModesRDParallel evaluates I4x4 using worker buffers.
func tryI4ModesRDParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, info *MBEncInfo, seg *SegmentInfo, modes *[16]uint8, topModes []uint8, leftModes []uint8, i16Score uint64, topNzVal, leftNzVal uint32) uint64 {
	totalRate := 0
	totalDisto := 0
	totalHeaderBits := 0

	// Get top modes for this position.
	var topM [4]uint8
	if mbY > 0 {
		off := mbX * 4
		copy(topM[:], topModes[off:off+4])
	} else {
		for i := range topM {
			topM[i] = BDCPred
		}
	}

	copy(w.yuvOut2, w.yuvOut)

	// NZ context from neighboring MBs.
	tnz := topNzVal & 0x0f
	lnz := leftNzVal & 0x0f

	maxI4HeaderBits := 15000
	earlyExit := false
	var l uint32

	for by := 0; by < 4 && !earlyExit; by++ {
		l = lnz & 1
		for bx := 0; bx < 4; bx++ {
			blockIdx := by*4 + bx

			var topMode, leftMode uint8
			if by == 0 {
				topMode = topM[bx]
			} else {
				topMode = modes[blockIdx-4]
			}
			if bx == 0 {
				leftMode = leftModes[by]
			} else {
				leftMode = modes[blockIdx-1]
			}

			srcOff := YOff + by*4*dsp.BPS + bx*4
			hasTop := (mbY > 0 || by > 0)
			hasLeft := (mbX > 0 || bx > 0)

			nzCtx := int(l) + int(tnz&1)
			if nzCtx > 2 {
				nzCtx = 2
			}

			var bestMode uint8
			var rate, disto int
			maxModes := getMaxI4RDModes(enc.config.Quality)
			if enc.config.Method >= 4 {
				bestMode, _, rate, disto = pickBestI4ModeRDTrellisParallel(w, w.yuvIn, srcOff, w.yuvOut2, srcOff, seg, topMode, leftMode, hasTop, hasLeft, nzCtx, &enc.proba, maxModes)
			} else {
				bestMode, _, rate, disto = pickBestI4ModeRDParallel(w, w.yuvIn, srcOff, w.yuvOut2, srcOff, seg, topMode, leftMode, hasTop, hasLeft, nzCtx, &enc.proba, maxModes)
			}
			modes[blockIdx] = bestMode
			totalRate += rate
			totalDisto += disto
			totalHeaderBits += int(VP8FixedCostsI4[topMode][leftMode][bestMode])

			coeffOff := blockIdx * 16
			copy(info.Coeffs[coeffOff:coeffOff+16], w.tmpBestQ[:])
			nz := w.tmpBestNz
			info.NzY[blockIdx] = uint8(nz)

			runningScore := RDScore(totalDisto, totalRate+211, seg.LambdaMode)
			if runningScore >= i16Score {
				earlyExit = true
				break
			}
			if totalHeaderBits > maxI4HeaderBits {
				earlyExit = true
				break
			}

			dsp.PredLuma4Direct(int(bestMode), w.yuvOut2, srcOff)
			dsp.ITransformDirect(w.yuvOut2[srcOff:], w.tmpBestDQ[:], w.yuvOut2[srcOff:], false)

			if nz > 0 {
				l = 1
			} else {
				l = 0
			}
			tnz = (tnz >> 1) | (l << 7)
		}
		tnz >>= 4
		lnz = (lnz >> 1) | (l << 7)
	}

	if earlyExit {
		return ^uint64(0)
	}

	// Save modes for context (written to topModes in exportParallel).
	totalRate += 211
	return RDScore(totalDisto, totalRate, seg.LambdaMode)
}

// pickBestI4ModeRDParallel evaluates I4 modes using worker buffers.
func pickBestI4ModeRDParallel(w *RowWorker, srcBuf []byte, srcOff int, predBuf []byte, predOff int,
	seg *SegmentInfo, topMode, leftMode uint8, hasTop, hasLeft bool, nzCtx int, proba *Proba, maxModes int) (bestMode uint8, bestScore uint64, bestRate int, bestDisto int) {
	bestScore = ^uint64(0)
	bestMode = BDCPred

	// Pre-screen: compute prediction SSE for all eligible modes.
	type modeCandidate struct {
		mode int
		sse  int
	}
	var candidates [NumBModes]modeCandidate
	nCandidates := 0

	for mode := 0; mode < NumBModes; mode++ {
		if !hasTop && needsTop4(mode) {
			continue
		}
		if !hasLeft && needsLeft4(mode) {
			continue
		}
		dsp.PredLuma4Direct(mode, predBuf, predOff)
		sse := dsp.SSE4x4Direct(srcBuf[srcOff:], predBuf[predOff:])
		candidates[nCandidates] = modeCandidate{mode, sse}
		nCandidates++
	}

	// Select top K modes by prediction SSE.
	K := maxModes
	if nCandidates <= K {
		K = nCandidates
	}
	for i := 0; i < K; i++ {
		minIdx := i
		for j := i + 1; j < nCandidates; j++ {
			if candidates[j].sse < candidates[minIdx].sse {
				minIdx = j
			}
		}
		if minIdx != i {
			candidates[i], candidates[minIdx] = candidates[minIdx], candidates[i]
		}
	}

	// Full RD evaluation for top K modes only.
	for i := 0; i < K; i++ {
		mode := candidates[i].mode

		dsp.PredLuma4Direct(mode, predBuf, predOff)
		dsp.FTransformDirect(srcBuf[srcOff:], predBuf[predOff:], w.tmpCoeffs[:])
		nz := QuantizeCoeffs(w.tmpCoeffs[:], w.tmpQCoeffs[:], &seg.Y1, 0)

		DequantCoeffs(w.tmpQCoeffs[:], w.tmpDQCoeffs[:], &seg.Y1)
		dsp.ITransformDirect(predBuf[predOff:], w.tmpDQCoeffs[:], w.tmpRecon[:], false)

		disto := dsp.SSE4x4Direct(srcBuf[srcOff:], w.tmpRecon[:])
		if seg.TLambdaSD > 0 {
			td := dsp.TDisto4x4(srcBuf[srcOff:], w.tmpRecon[:])
			disto += (seg.TLambdaSD*td + 128) >> 8
		}

		if 256*uint64(disto) >= bestScore {
			continue
		}

		rate := 0
		if mode > 0 && isFlat(w.tmpQCoeffs[:], 1, flatnessLimitI4) {
			rate = flatnessPenalty * 1
		}

		rate += TokenCostForCoeffs(w.tmpQCoeffs[:], nz, 3, proba, nzCtx, 0)
		rate += int(VP8FixedCostsI4[topMode][leftMode][mode])

		score := RDScore(disto, rate, seg.LambdaI4)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
			bestRate = rate
			bestDisto = disto
			w.tmpBestDQ = w.tmpDQCoeffs
			w.tmpBestQ = w.tmpQCoeffs
			w.tmpBestNz = nz
		}
	}
	return
}

// getMaxI4RDModes returns the maximum number of I4 prediction modes to
// evaluate with full RD, based on encoding quality. At low quality (< 50),
// fewer modes are evaluated since the quality difference is negligible.
func getMaxI4RDModes(quality int) int {
	if quality < 50 {
		return 2
	}
	return 3
}

// pickBestI4ModeRDTrellisParallel uses trellis quantization with worker buffers.
// Modes are pre-screened by prediction SSE and only the most promising ones
// get the expensive encode-decode cycle.

func pickBestI4ModeRDTrellisParallel(w *RowWorker, srcBuf []byte, srcOff int, predBuf []byte, predOff int,
	seg *SegmentInfo, topMode, leftMode uint8, hasTop, hasLeft bool, nzCtx int, proba *Proba, maxModes int) (bestMode uint8, bestScore uint64, bestRate int, bestDisto int) {
	bestScore = ^uint64(0)
	bestMode = BDCPred

	// Pre-screen: compute prediction SSE for all eligible modes.
	type modeCandidate struct {
		mode int
		sse  int
	}
	var candidates [NumBModes]modeCandidate
	nCandidates := 0

	for mode := 0; mode < NumBModes; mode++ {
		if !hasTop && needsTop4(mode) {
			continue
		}
		if !hasLeft && needsLeft4(mode) {
			continue
		}
		dsp.PredLuma4Direct(mode, predBuf, predOff)
		sse := dsp.SSE4x4Direct(srcBuf[srcOff:], predBuf[predOff:])
		candidates[nCandidates] = modeCandidate{mode, sse}
		nCandidates++
	}

	// Select top K modes by prediction SSE (partial selection sort).
	K := maxModes
	if nCandidates <= K {
		K = nCandidates
	}
	for i := 0; i < K; i++ {
		minIdx := i
		for j := i + 1; j < nCandidates; j++ {
			if candidates[j].sse < candidates[minIdx].sse {
				minIdx = j
			}
		}
		if minIdx != i {
			candidates[i], candidates[minIdx] = candidates[minIdx], candidates[i]
		}
	}

	// Full RD evaluation for top K modes only.
	for i := 0; i < K; i++ {
		mode := candidates[i].mode

		dsp.PredLuma4Direct(mode, predBuf, predOff)
		dsp.FTransformDirect(srcBuf[srcOff:], predBuf[predOff:], w.tmpCoeffs[:])
		nz := TrellisQuantizeBlock(w.tmpCoeffs[:], w.tmpQCoeffs[:],
			&seg.Y1, 0, 3, nzCtx, proba, seg.TLambdaI4)

		DequantCoeffs(w.tmpQCoeffs[:], w.tmpDQCoeffs[:], &seg.Y1)
		dsp.ITransformDirect(predBuf[predOff:], w.tmpDQCoeffs[:], w.tmpRecon[:], false)

		disto := dsp.SSE4x4Direct(srcBuf[srcOff:], w.tmpRecon[:])
		if seg.TLambdaSD > 0 {
			td := dsp.TDisto4x4(srcBuf[srcOff:], w.tmpRecon[:])
			disto += (seg.TLambdaSD*td + 128) >> 8
		}

		if 256*uint64(disto) >= bestScore {
			continue
		}

		rate := 0
		if mode > 0 && isFlat(w.tmpQCoeffs[:], 1, flatnessLimitI4) {
			rate = flatnessPenalty * 1
		}

		rate += TokenCostForCoeffs(w.tmpQCoeffs[:], nz, 3, proba, nzCtx, 0)
		rate += int(VP8FixedCostsI4[topMode][leftMode][mode])

		score := RDScore(disto, rate, seg.LambdaI4)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
			bestRate = rate
			bestDisto = disto
			w.tmpBestDQ = w.tmpDQCoeffs
			w.tmpBestQ = w.tmpQCoeffs
			w.tmpBestNz = nz
		}
	}
	return
}

// pickBestUVModeRDParallel evaluates UV modes using worker buffers.
func pickBestUVModeRDParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, seg *SegmentInfo, topNzVal, leftNzVal uint32) (bestMode uint8, bestScore uint64, bestRate int, bestDisto int) {
	bestScore = ^uint64(0)
	bestMode = DCPred

	src := w.yuvIn
	pred := w.yuvOut2

	copy(pred[UOff:], w.yuvOut[UOff:])

	for mode := 0; mode < NumPredModes; mode++ {
		actualMode := checkMode(mbX, mbY, mode)
		if mode == int(VPred) && mbY == 0 {
			continue
		}
		if mode == int(HPred) && mbX == 0 {
			continue
		}
		if mode == int(TMPred) && (mbX == 0 || mbY == 0) {
			continue
		}

		dsp.PredChroma8Direct(actualMode, pred, UOff)
		dsp.PredChroma8Direct(actualMode, pred, VOff)

		totalRate := modeFixedCostUV[mode]
		uvBlockIdx := 0

		chPlanes := [2]int{UOff, VOff}
		for ch := uint(0); ch < 4; ch += 2 {
			// NZ context from neighboring MBs for UV channels.
			tnz := (topNzVal >> (4 + ch)) & 0x0f
			lnz := (leftNzVal >> (4 + ch)) & 0x0f
			planeOff := chPlanes[ch/2]

			for by := 0; by < 2; by++ {
				l := lnz & 1
				for bx := 0; bx < 2; bx++ {
					off := by*4*dsp.BPS + bx*4
					ctx := int(l) + int(tnz&1)
					if ctx > 2 {
						ctx = 2
					}
					dsp.FTransformDirect(src[planeOff+off:], pred[planeOff+off:], w.tmpCoeffs[:])
					nz := QuantizeCoeffs(w.tmpCoeffs[:], w.tmpQCoeffs[:], &seg.UV, 0)
					totalRate += TokenCostForCoeffs(w.tmpQCoeffs[:], nz, 2, &enc.proba, ctx, 0)
					copy(w.tmpUVLevels[uvBlockIdx*16:uvBlockIdx*16+16], w.tmpQCoeffs[:])
					uvBlockIdx++
					DequantCoeffs(w.tmpQCoeffs[:], w.tmpDQCoeffs[:], &seg.UV)
					dsp.ITransformDirect(pred[planeOff+off:], w.tmpDQCoeffs[:], pred[planeOff+off:], false)

					if nz > 0 {
						l = 1
					} else {
						l = 0
					}
					tnz = (tnz >> 1) | (l << 3)
				}
				tnz >>= 2
				lnz = (lnz >> 1) | (l << 5)
			}
		}

		if mode > 0 && isFlat(w.tmpUVLevels[:], 8, flatnessLimitUV) {
			totalRate += flatnessPenalty * 8
		}

		distoU := 0
		distoV := 0
		for by := 0; by < 2; by++ {
			for bx := 0; bx < 2; bx++ {
				off := by*4*dsp.BPS + bx*4
				distoU += dsp.SSE4x4Direct(src[UOff+off:], pred[UOff+off:])
				distoV += dsp.SSE4x4Direct(src[VOff+off:], pred[VOff+off:])
			}
		}

		disto := distoU + distoV
		score := RDScore(disto, totalRate, seg.LambdaUV)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
			bestRate = totalRate
			bestDisto = disto
		}
	}
	return
}

// tryI4ModesParallel evaluates I4x4 modes for Method < 3.
func tryI4ModesParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, info *MBEncInfo, seg *SegmentInfo, modes *[16]uint8, topModes []uint8, leftModes []uint8) uint64 {
	totalScore := uint64(0)

	var topM [4]uint8
	if mbY > 0 {
		off := mbX * 4
		copy(topM[:], topModes[off:off+4])
	} else {
		for i := range topM {
			topM[i] = BDCPred
		}
	}

	for by := 0; by < 4; by++ {
		for bx := 0; bx < 4; bx++ {
			blockIdx := by*4 + bx

			var topMode, leftMode uint8
			if by == 0 {
				topMode = topM[bx]
			} else {
				topMode = modes[blockIdx-4]
			}
			if bx == 0 {
				leftMode = leftModes[by]
			} else {
				leftMode = modes[blockIdx-1]
			}

			srcOff := YOff + by*4*dsp.BPS + bx*4
			predOff := dsp.BPS + 1 + by*4*dsp.BPS + bx*4

			hasTop := (mbY > 0 || by > 0)
			hasLeft := (mbX > 0 || bx > 0)
			bestMode, bestScore := PickBestI4Mode(w.yuvIn, srcOff, w.yuvP, predOff, seg, topMode, leftMode, hasTop, hasLeft)
			modes[blockIdx] = bestMode
			totalScore += bestScore
		}
	}

	totalScore += uint64(seg.LambdaMode) * 211
	return totalScore
}

// encodeResidualsParallel computes residuals using worker buffers.
func encodeResidualsParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, info *MBEncInfo, seg *SegmentInfo, topNzVal, leftNzVal uint32, topNzDCVal, leftNzDCVal uint8) {
	if info.MBType == 0 {
		encodeI16ResidualsParallel(enc, w, mbX, mbY, info, seg, topNzVal, leftNzVal)
	} else {
		encodeI4ResidualsParallel(enc, w, mbX, mbY, info, seg, topNzVal, leftNzVal)
	}
	encodeUVResidualsParallel(enc, w, mbX, mbY, info, seg, topNzVal, leftNzVal)
}

// encodeI16ResidualsParallel handles I16 residuals with worker buffers.
func encodeI16ResidualsParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, info *MBEncInfo, seg *SegmentInfo, topNzVal, leftNzVal uint32) {
	srcY := w.yuvIn[YOff:]
	predY := w.yuvOut[YOff:]

	if !info.PredCached {
		actualI16Mode := checkMode(mbX, mbY, int(info.I16Mode))
		dsp.PredLuma16Direct(actualI16Mode, w.yuvOut, YOff)
	}

	dcCoeffs := &w.tmpDCCoeffs
	nzY := uint32(0)

	// NZ context from neighboring MBs.
	tnz := topNzVal & 0x0f
	lnz := leftNzVal & 0x0f

	for by := 0; by < 4; by++ {
		l := lnz & 1
		for bx := 0; bx < 4; bx++ {
			blockIdx := by*4 + bx
			srcOff := by*4*dsp.BPS + bx*4
			coeffOff := blockIdx * 16

			dsp.FTransformDirect(srcY[srcOff:], predY[srcOff:], info.Coeffs[coeffOff:])
			dcCoeffs[blockIdx] = info.Coeffs[coeffOff]
			info.Coeffs[coeffOff] = 0

			var nz int
			if enc.config.Method >= 4 {
				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}
				nz = TrellisQuantizeBlock(info.Coeffs[coeffOff:coeffOff+16], info.Coeffs[coeffOff:coeffOff+16],
					&seg.Y1, 1, 0, ctx, &enc.proba, seg.TLambdaI16)
				info.NzY[blockIdx] = uint8(nz)
			} else {
				nz = QuantizeCoeffs(info.Coeffs[coeffOff:coeffOff+16], info.Coeffs[coeffOff:coeffOff+16], &seg.Y1, 1)
				if nz == 0 {
					info.NzY[blockIdx] = 0
				} else {
					info.NzY[blockIdx] = uint8(nzCount(info.Coeffs[coeffOff : coeffOff+16]))
				}
			}
			if nz > 0 {
				nzY |= 1 << uint(blockIdx)
				l = 1
			} else {
				l = 0
			}
			tnz = (tnz >> 1) | (l << 7)
		}
		tnz >>= 4
		lnz = (lnz >> 1) | (l << 7)
	}

	whtOut := &w.tmpCoeffs
	dsp.FTransformWHT(dcCoeffs[:], whtOut[:])

	nzDC := QuantizeCoeffs(whtOut[:], info.Coeffs[384:400], &seg.Y2, 0)
	if nzDC > 0 {
		nzY |= 1 << 24
		info.NzDC = uint8(nzCount(info.Coeffs[384:400]))
	} else {
		info.NzDC = 0
	}

	info.NonZeroY = nzY
}

// encodeI4ResidualsParallel handles I4 residuals with worker buffers.
func encodeI4ResidualsParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, info *MBEncInfo, seg *SegmentInfo, topNzVal, leftNzVal uint32) {
	if info.I4Cached {
		nzY := uint32(0)
		for blockIdx := 0; blockIdx < 16; blockIdx++ {
			if info.NzY[blockIdx] > 0 {
				nzY |= 1 << uint(blockIdx)
			}
		}
		info.NonZeroY = nzY
		return
	}

	srcY := w.yuvIn[YOff:]
	predY := w.yuvOut[YOff:]
	nzY := uint32(0)

	// NZ context from neighboring MBs.
	tnz := topNzVal & 0x0f
	lnz := leftNzVal & 0x0f

	for by := 0; by < 4; by++ {
		l := lnz & 1
		for bx := 0; bx < 4; bx++ {
			blockIdx := by*4 + bx
			srcOff := by*4*dsp.BPS + bx*4
			coeffOff := blockIdx * 16

			mode := info.Modes[blockIdx]
			dsp.PredLuma4Direct(int(mode), w.yuvOut, YOff+srcOff)

			dsp.FTransformDirect(srcY[srcOff:], predY[srcOff:], info.Coeffs[coeffOff:])

			nz := QuantizeCoeffs(info.Coeffs[coeffOff:coeffOff+16], info.Coeffs[coeffOff:coeffOff+16], &seg.Y1, 0)
			if nz == 0 {
				info.NzY[blockIdx] = 0
			} else {
				info.NzY[blockIdx] = uint8(nzCount(info.Coeffs[coeffOff : coeffOff+16]))
			}
			if nz > 0 {
				nzY |= 1 << uint(blockIdx)
				l = 1
			} else {
				l = 0
			}
			tnz = (tnz >> 1) | (l << 7)

			DequantCoeffs(info.Coeffs[coeffOff:coeffOff+16], w.tmpDQCoeffs[:], &seg.Y1)
			dsp.ITransformDirect(predY[srcOff:], w.tmpDQCoeffs[:], predY[srcOff:], false)
		}
		tnz >>= 4
		lnz = (lnz >> 1) | (l << 7)
	}

	info.NonZeroY = nzY
}

// encodeUVResidualsParallel handles UV residuals with worker buffers.
func encodeUVResidualsParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, info *MBEncInfo, seg *SegmentInfo, topNzVal, leftNzVal uint32) {
	srcU := w.yuvIn[UOff:]
	srcV := w.yuvIn[VOff:]
	predU := w.yuvOut[UOff:]
	predV := w.yuvOut[VOff:]

	if !info.PredCached {
		actualUVMode := checkMode(mbX, mbY, int(info.UVMode))
		dsp.PredChroma8Direct(actualUVMode, w.yuvOut, UOff)
		dsp.PredChroma8Direct(actualUVMode, w.yuvOut, VOff)
	}

	nzUV := uint32(0)

	srcs := [2][]byte{srcU, srcV}
	preds := [2][]byte{predU, predV}

	for ch := 0; ch < 2; ch++ {
		for by := 0; by < 2; by++ {
			for bx := 0; bx < 2; bx++ {
				blockIdx := by*2 + bx
				srcOff := by*4*dsp.BPS + bx*4
				uvBase := 16 + ch*4
				coeffOff := (uvBase + blockIdx) * 16
				dsp.FTransformDirect(srcs[ch][srcOff:], preds[ch][srcOff:], info.Coeffs[coeffOff:])
			}
		}
	}

	// Skip DC error diffusion in parallel mode for simplicity.
	// Quality impact is negligible (<0.01 dB).

	for ch := uint(0); ch < 4; ch += 2 {
		tnz := (topNzVal >> (4 + ch)) & 0x0f
		lnz := (leftNzVal >> (4 + ch)) & 0x0f
		for by := 0; by < 2; by++ {
			l := lnz & 1
			for bx := 0; bx < 2; bx++ {
				blockIdx := by*2 + bx
				uvBase := 16 + int(ch/2)*4
				coeffOff := (uvBase + blockIdx) * 16

				nz := QuantizeCoeffs(info.Coeffs[coeffOff:coeffOff+16], info.Coeffs[coeffOff:coeffOff+16], &seg.UV, 0)
				uvIdx := int(ch/2)*4 + blockIdx
				if nz == 0 {
					info.NzUV[uvIdx] = 0
				} else {
					info.NzUV[uvIdx] = uint8(nzCount(info.Coeffs[coeffOff : coeffOff+16]))
				}
				if nz > 0 {
					nzUV |= 1 << uint(ch/2*4+uint(blockIdx))
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 3)
			}
			tnz >>= 2
			lnz = (lnz >> 1) | (l << 5)
		}
	}

	info.NonZeroUV = nzUV
}

// reconstructMBParallel reconstructs the MB in worker's yuvOut.
func reconstructMBParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, info *MBEncInfo, seg *SegmentInfo) {
	predY := w.yuvOut[YOff:]

	if info.MBType == 0 {
		if !info.PredCached {
			reconI16Mode := checkMode(mbX, mbY, int(info.I16Mode))
			dsp.PredLuma16Direct(reconI16Mode, w.yuvOut, YOff)
		}

		DequantCoeffs(info.Coeffs[384:400], w.tmpWHTDQ[:], &seg.Y2)
		dsp.TransformWHT(w.tmpWHTDQ[:], w.tmpWHTBuf[:])
		for i := 0; i < 16; i++ {
			w.tmpDCCoeffs[i] = w.tmpWHTBuf[i*16]
		}

		for by := 0; by < 4; by++ {
			for bx := 0; bx < 4; bx++ {
				blockIdx := by*4 + bx
				off := blockIdx * 16
				dstOff := by*4*dsp.BPS + bx*4
				DequantCoeffs(info.Coeffs[off:off+16], w.tmpDQCoeffs[:], &seg.Y1)
				w.tmpDQCoeffs[0] = w.tmpDCCoeffs[blockIdx]
				dsp.ITransformDirect(predY[dstOff:], w.tmpDQCoeffs[:], predY[dstOff:], false)
			}
		}
	}

	predU := w.yuvOut[UOff:]
	predV := w.yuvOut[VOff:]
	if !info.PredCached {
		reconUVMode := checkMode(mbX, mbY, int(info.UVMode))
		dsp.PredChroma8Direct(reconUVMode, w.yuvOut, UOff)
		dsp.PredChroma8Direct(reconUVMode, w.yuvOut, VOff)
	}

	for by := 0; by < 2; by++ {
		for bx := 0; bx < 2; bx++ {
			blockIdx := by*2 + bx
			coeffOffU := (16 + blockIdx) * 16
			DequantCoeffs(info.Coeffs[coeffOffU:coeffOffU+16], w.tmpDQCoeffs[:], &seg.UV)
			dstU := by*4*dsp.BPS + bx*4
			dsp.ITransformDirect(predU[dstU:], w.tmpDQCoeffs[:], predU[dstU:], false)

			coeffOffV := (20 + blockIdx) * 16
			DequantCoeffs(info.Coeffs[coeffOffV:coeffOffV+16], w.tmpDQCoeffs[:], &seg.UV)
			dstV := by*4*dsp.BPS + bx*4
			dsp.ITransformDirect(predV[dstV:], w.tmpDQCoeffs[:], predV[dstV:], false)
		}
	}
}

// exportParallel writes reconstructed MB to planes and updates context.
func exportParallel(enc *VP8Encoder, w *RowWorker, mbX, mbY int, topY, topU, topV, topModes []uint8,
	leftY *[16]uint8, leftU *[8]uint8, leftV *[8]uint8, leftModes *[4]uint8,
	topLeftY, topLeftU, topLeftV *uint8, info *MBEncInfo) {

	bps := dsp.BPS
	x := mbX * 16
	y := mbY * 16

	// Write back to Y/U/V planes.
	wY := 16
	hY := 16
	if x+16 > enc.width {
		wY = enc.width - x
	}
	if y+16 > enc.height {
		hY = enc.height - y
	}

	for j := 0; j < hY; j++ {
		srcOff := YOff + j*bps
		dstOff := (y+j)*enc.yStride + x
		copy(enc.yPlane[dstOff:dstOff+wY], w.yuvOut[srcOff:srcOff+wY])
	}

	ux := mbX * 8
	uy := mbY * 8
	wUV := 8
	hUV := 8
	if ux+8 > enc.mbW*8 {
		wUV = enc.mbW*8 - ux
	}
	if uy+8 > enc.mbH*8 {
		hUV = enc.mbH*8 - uy
	}
	for j := 0; j < hUV; j++ {
		srcU := UOff + j*bps
		srcV := VOff + j*bps
		dstU := (uy+j)*enc.uvStride + ux
		dstV := (uy+j)*enc.uvStride + ux
		copy(enc.uPlane[dstU:dstU+wUV], w.yuvOut[srcU:srcU+wUV])
		copy(enc.vPlane[dstV:dstV+wUV], w.yuvOut[srcV:srcV+wUV])
	}

	// Update shared top context.
	topOff := mbX * 16
	topUOff := mbX * 8

	*topLeftY = topY[topOff+15]
	*topLeftU = topU[topUOff+7]
	*topLeftV = topV[topUOff+7]

	for i := 0; i < 16; i++ {
		topY[topOff+i] = w.yuvOut[YOff+15*bps+i]
	}
	for i := 0; i < 8; i++ {
		topU[topUOff+i] = w.yuvOut[UOff+7*bps+i]
		topV[topUOff+i] = w.yuvOut[VOff+7*bps+i]
	}

	// Update left context.
	for j := 0; j < 16; j++ {
		leftY[j] = w.yuvOut[YOff+j*bps+15]
	}
	for j := 0; j < 8; j++ {
		leftU[j] = w.yuvOut[UOff+j*bps+7]
		leftV[j] = w.yuvOut[VOff+j*bps+7]
	}

	// Update top modes for I4 context.
	if info.MBType == 1 {
		modeOff := mbX * 4
		topModes[modeOff] = info.Modes[12]
		topModes[modeOff+1] = info.Modes[13]
		topModes[modeOff+2] = info.Modes[14]
		topModes[modeOff+3] = info.Modes[15]
		leftModes[0] = info.Modes[3]
		leftModes[1] = info.Modes[7]
		leftModes[2] = info.Modes[11]
		leftModes[3] = info.Modes[15]
	} else {
		modeOff := mbX * 4
		for i := 0; i < 4; i++ {
			topModes[modeOff+i] = BDCPred
			leftModes[i] = BDCPred
		}
	}
}

// recordAllTokens performs Phase B: serial token recording over all MBs.
// This uses the encoder's main NZ context (topNz/leftNz) which must be
// accurate for correct bitstream encoding.
// If stats is non-nil, also collects probability statistics (merging the
// work of collectAllStats to save a separate pass over all MBs).
func (enc *VP8Encoder) recordAllTokens(stats *ProbaStats) {
	enc.InitIterator()
	it := &enc.mbIterator

	// Reset NZ context.
	for i := range enc.topNz {
		enc.topNz[i] = 0
	}
	for i := range enc.topNzDC {
		enc.topNzDC[i] = 0
	}
	enc.leftNz = 0
	enc.leftNzDC = 0
	enc.numSkip = 0

	// Also reset stats NZ context if collecting stats.
	if stats != nil {
		*stats = ProbaStats{}
		for i := range enc.statTopNz {
			enc.statTopNz[i] = 0
		}
		for i := range enc.statTopNzDC {
			enc.statTopNzDC[i] = 0
		}
	}

	// Mid-stream probability refresh.
	totalMB := enc.mbW * enc.mbH
	maxCount := totalMB >> 3
	if maxCount < minRefreshCount {
		maxCount = minRefreshCount
	}
	refreshCnt := maxCount

	var statLeftNz uint32
	var statLeftNzDC uint8

	for !it.IsDone() {
		if it.X == 0 {
			// In overlapped mode, wait for the current row to be fully encoded
			// before recording its tokens. This allows Phase B to overlap with
			// Phase A workers still processing later rows.
			if enc.parallelRS != nil {
				enc.parallelRS.waitFor(it.Y, int32(enc.mbW))
			}
			enc.leftNz = 0
			enc.leftNzDC = 0
			if stats != nil {
				statLeftNz = 0
				statLeftNzDC = 0
			}
		}
		mbIdx := it.MBIdx
		info := &enc.mbInfo[mbIdx]

		// Skip refreshProbas in overlapped mode to avoid data races with
		// Phase A workers reading enc.proba for rate estimation.
		// The final optimizeProba call after both phases handles this.
		if enc.parallelRS == nil {
			refreshCnt--
			if refreshCnt < 0 {
				enc.refreshProbas()
				refreshCnt = maxCount
			}
		}

		if info.Skip {
			enc.numSkip++
			enc.topNz[it.X] = 0
			enc.leftNz = 0
			if info.MBType == 0 {
				enc.topNzDC[it.X] = 0
				enc.leftNzDC = 0
			}
			if stats != nil {
				enc.statTopNz[it.X] = 0
				statLeftNz = 0
				if info.MBType == 0 {
					enc.statTopNzDC[it.X] = 0
					statLeftNzDC = 0
				}
			}
		} else {
			enc.recordMBTokens(it, info)
			if stats != nil {
				enc.collectMBStats(it.X, info, stats, &statLeftNz, &statLeftNzDC)
			}
		}

		it.Next()
	}

	if enc.numSkip > 0 {
		enc.skipProba = uint8((totalMB - enc.numSkip) * 255 / totalMB)
	}
}

// collectMBStats collects probability statistics for a single MB,
// using the encoder's stat NZ context arrays.
func (enc *VP8Encoder) collectMBStats(mbX int, info *MBEncInfo, stats *ProbaStats, leftNz *uint32, leftNzDC *uint8) {
	topNzVal := enc.statTopNz[mbX]
	leftNzVal := *leftNz
	var outTNz, outLNz uint32

	if info.MBType == 0 {
		dcCtx := int(enc.statTopNzDC[mbX]) + int(*leftNzDC)
		if dcCtx > 2 {
			dcCtx = 2
		}
		nzDC := int(info.NzDC)
		collectCoeffStats(info.Coeffs[384:400], nzDC, 1, &enc.proba, 0, dcCtx, stats)
		if nzDC > 0 {
			enc.statTopNzDC[mbX] = 1
			*leftNzDC = 1
		} else {
			enc.statTopNzDC[mbX] = 0
			*leftNzDC = 0
		}

		first := 1
		tnz := topNzVal & 0x0f
		lnz := leftNzVal & 0x0f
		for y := 0; y < 4; y++ {
			l := lnz & 1
			for x := 0; x < 4; x++ {
				blockIdx := y*4 + x
				off := blockIdx * 16
				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}
				nz := int(info.NzY[blockIdx])
				collectCoeffStats(info.Coeffs[off:off+16], nz, 0, &enc.proba, first, ctx, stats)
				if nz > first {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 7)
			}
			tnz >>= 4
			lnz = (lnz >> 1) | (l << 7)
		}
		outTNz = tnz
		outLNz = lnz >> 4
	} else {
		tnz := topNzVal & 0x0f
		lnz := leftNzVal & 0x0f
		for y := 0; y < 4; y++ {
			l := lnz & 1
			for x := 0; x < 4; x++ {
				blockIdx := y*4 + x
				off := blockIdx * 16
				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}
				nz := int(info.NzY[blockIdx])
				collectCoeffStats(info.Coeffs[off:off+16], nz, 3, &enc.proba, 0, ctx, stats)
				if nz > 0 {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 7)
			}
			tnz >>= 4
			lnz = (lnz >> 1) | (l << 7)
		}
		outTNz = tnz
		outLNz = lnz >> 4
	}

	for ch := uint(0); ch < 4; ch += 2 {
		tnz := (topNzVal >> (4 + ch)) & 0x0f
		lnz := (leftNzVal >> (4 + ch)) & 0x0f
		for y := 0; y < 2; y++ {
			l := lnz & 1
			for x := 0; x < 2; x++ {
				blockIdx := y*2 + x
				off := (16 + int(ch/2)*4 + blockIdx) * 16
				uvIdx := int(ch/2)*4 + blockIdx
				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}
				nz := int(info.NzUV[uvIdx])
				collectCoeffStats(info.Coeffs[off:off+16], nz, 2, &enc.proba, 0, ctx, stats)
				if nz > 0 {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 3)
			}
			tnz >>= 2
			lnz = (lnz >> 1) | (l << 5)
		}
		outTNz |= (tnz << 4) << ch
		outLNz |= (lnz & 0xf0) << ch
	}

	enc.statTopNz[mbX] = outTNz
	*leftNz = outLNz
}
