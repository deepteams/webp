package animation

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/deepteams/webp/mux"
)

// Animation holds all frames and parameters of an animated WebP image.
type Animation struct {
	// Frames holds the ordered animation frames.
	Frames []Frame

	// LoopCount is the number of times to loop the animation.
	// 0 means infinite looping.
	LoopCount int

	// BackgroundColor is the canvas background color.
	BackgroundColor color.NRGBA

	// CanvasWidth is the canvas width in pixels.
	CanvasWidth int

	// CanvasHeight is the canvas height in pixels.
	CanvasHeight int

	// ICC holds the ICC color profile data (may be nil).
	ICC []byte

	// EXIF holds the EXIF metadata (may be nil).
	EXIF []byte

	// XMP holds the XMP metadata (may be nil).
	XMP []byte
}

// FrameDecoderFunc decodes a raw bitstream frame into an NRGBA image.
// It will be set by the codec package once available.
var FrameDecoderFunc func(bitstreamData, alphaData []byte) (*image.NRGBA, error)

// FrameEncoderFunc encodes an image to a raw VP8/VP8L bitstream.
// lossless controls whether VP8L (true) or VP8 (false) is used.
// quality controls encoding quality (0-100).
// It will be set by the codec package once available.
var FrameEncoderFunc func(img image.Image, lossless bool, quality int) ([]byte, error)

// SimpleEncodeFunc encodes an image as a complete simple (non-animated) WebP
// file. It is used by the single-frame optimization to compare the size of
// an animated single-frame WebP against a simple WebP. Returns the full
// RIFF/WEBP byte sequence. It will be set by the codec package once available.
var SimpleEncodeFunc func(img image.Image, lossless bool, quality float32) ([]byte, error)

var (
	ErrNoFrames       = errors.New("animation: no frames")
	ErrCanvasSize     = errors.New("animation: invalid canvas dimensions")
	ErrFrameOutOfRect = errors.New("animation: frame exceeds canvas bounds")
	ErrNilImage       = errors.New("animation: frame image is nil")
	ErrNoDecoder      = errors.New("animation: no frame decoder available")
)

// maxDuration is the maximum frame duration in milliseconds (24-bit max,
// 0xFFFFFF). This matches the C libwebp MAX_DURATION constant.
const maxDuration = 0xFFFFFF // 16777215

// maxLoopCount is the maximum animation loop count (16-bit max, 0xFFFF).
// This matches the C libwebp MAX_LOOP_COUNT constant.
const maxLoopCount = 0xFFFF // 65535

// DecodeConfig holds options for Decode.
type DecodeConfig struct {
	// DecodePixels controls whether pixel data is decoded.
	// If false, Frame.BitstreamData is populated but Image is nil.
	DecodePixels bool
}

// Decode parses a WebP animation from r, extracting container structure and frames.
// Pixel data is stored as raw bitstream; actual VP8/VP8L decoding is deferred until
// FrameDecoderFunc is set and DecodeFrames/AnimDecoder is used.
func Decode(r io.Reader) (*Animation, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return DecodeBytes(data)
}

// DecodeBytes parses a WebP animation from raw bytes.
func DecodeBytes(data []byte) (*Animation, error) {
	dmx, err := mux.NewDemuxer(data)
	if err != nil {
		return nil, err
	}

	feat := dmx.GetFeatures()
	anim := &Animation{
		CanvasWidth:  feat.Width,
		CanvasHeight: feat.Height,
		LoopCount:    dmx.LoopCount(),
		BackgroundColor: argbToNRGBA(dmx.BackgroundColor()),
	}

	// Extract metadata.
	if icc, err := dmx.GetChunk(mux.FourCCICCP); err == nil {
		anim.ICC = icc
	}
	if exif, err := dmx.GetChunk(mux.FourCCEXIF); err == nil {
		anim.EXIF = exif
	}
	if xmp, err := dmx.GetChunk(mux.FourCCXMP); err == nil {
		anim.XMP = xmp
	}

	// Extract frames.
	n := dmx.NumFrames()
	anim.Frames = make([]Frame, n)
	for i := 0; i < n; i++ {
		fi, err := dmx.Frame(i)
		if err != nil {
			return nil, err
		}
		anim.Frames[i] = Frame{
			Duration:      time.Duration(fi.Duration) * time.Millisecond,
			OffsetX:       fi.OffsetX,
			OffsetY:       fi.OffsetY,
			Dispose:       DisposeMethod(fi.DisposeMode),
			Blend:         BlendMethod(fi.BlendMode),
			IsKeyframe:    fi.IsKeyframe,
			HasAlpha:      fi.HasAlpha,
			BitstreamData: fi.Data,
			AlphaData:     fi.AlphaData,
		}
	}

	return anim, nil
}

// TotalDuration returns the sum of all frame durations.
func (a *Animation) TotalDuration() time.Duration {
	var total time.Duration
	for i := range a.Frames {
		total += a.Frames[i].Duration
	}
	return total
}

// DecodeFrames decodes all frames using FrameDecoderFunc.
// Frames that already have a non-nil Image are skipped.
func (a *Animation) DecodeFrames() error {
	if FrameDecoderFunc == nil {
		return ErrNoDecoder
	}
	for i := range a.Frames {
		f := &a.Frames[i]
		if f.Image != nil {
			continue
		}
		if f.BitstreamData == nil {
			continue
		}
		img, err := FrameDecoderFunc(f.BitstreamData, f.AlphaData)
		if err != nil {
			return err
		}
		f.Image = img
	}
	return nil
}

// DecodeFramesParallel decodes all frames using FrameDecoderFunc in parallel.
// Each frame's VP8/VP8L bitstream is decoded independently on a separate
// goroutine. The number of concurrent decoders is limited to GOMAXPROCS.
// Frames that already have a non-nil Image are skipped.
// For small frame counts (<= 2), falls back to sequential DecodeFrames.
func (a *Animation) DecodeFramesParallel() error {
	if FrameDecoderFunc == nil {
		return ErrNoDecoder
	}

	// Collect indices of frames that need decoding.
	var toDecodeIdx []int
	for i := range a.Frames {
		if a.Frames[i].Image == nil && a.Frames[i].BitstreamData != nil {
			toDecodeIdx = append(toDecodeIdx, i)
		}
	}
	if len(toDecodeIdx) == 0 {
		return nil
	}
	if len(toDecodeIdx) <= 2 {
		return a.DecodeFrames()
	}

	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > len(toDecodeIdx) {
		numWorkers = len(toDecodeIdx)
	}

	type decodeResult struct {
		idx int
		img image.Image
		err error
	}

	work := make(chan int, len(toDecodeIdx))
	for _, idx := range toDecodeIdx {
		work <- idx
	}
	close(work)

	results := make(chan decodeResult, len(toDecodeIdx))
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				f := &a.Frames[idx]
				img, err := FrameDecoderFunc(f.BitstreamData, f.AlphaData)
				results <- decodeResult{idx: idx, img: img, err: err}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
			continue
		}
		if r.err == nil {
			a.Frames[r.idx].Image = r.img
		}
	}
	return firstErr
}

// argbToNRGBA converts an ARGB uint32 to color.NRGBA.
func argbToNRGBA(argb uint32) color.NRGBA {
	return color.NRGBA{
		A: uint8(argb >> 24),
		R: uint8(argb >> 16),
		G: uint8(argb >> 8),
		B: uint8(argb),
	}
}

// nrgbaToARGB converts color.NRGBA to an ARGB uint32.
func nrgbaToARGB(c color.NRGBA) uint32 {
	return uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

// --- AnimDecoder: canvas reconstruction ---

// AnimDecoder provides frame-by-frame canvas reconstruction for animated WebP.
// It uses a dual-buffer approach matching the C libwebp reference:
//   - currFrame: the current canvas being built
//   - prevFrameDisposed: the previous canvas after disposal has been applied
type AnimDecoder struct {
	anim              *Animation
	currFrame         *image.NRGBA
	prevFrameDisposed *image.NRGBA
	pos               int

	// State for keyframe detection.
	prevFrameWasKeyframe bool
	prevDispose          DisposeMethod
	prevBounds           image.Rectangle
}

// NewAnimDecoder creates an AnimDecoder from an Animation.
// The canvas is initialized to transparent (0,0,0,0), matching the C reference.
func NewAnimDecoder(anim *Animation) *AnimDecoder {
	bounds := image.Rect(0, 0, anim.CanvasWidth, anim.CanvasHeight)
	d := &AnimDecoder{
		anim:              anim,
		currFrame:         image.NewNRGBA(bounds),
		prevFrameDisposed: image.NewNRGBA(bounds),
	}
	// Both buffers start as zero-filled (transparent), matching C calloc behavior.
	return d
}

// HasNext reports whether more frames are available.
func (d *AnimDecoder) HasNext() bool {
	return d.pos < len(d.anim.Frames)
}

// isKeyFrame determines if the frame at the given index is a keyframe.
// This matches the C libwebp IsKeyFrame() logic, using the bitstream's
// has_alpha flag instead of scanning pixel data.
func (d *AnimDecoder) isKeyFrame(idx int) bool {
	f := &d.anim.Frames[idx]

	// First frame is always a keyframe.
	if idx == 0 {
		return true
	}

	canvasW := d.anim.CanvasWidth
	canvasH := d.anim.CanvasHeight

	// A full-canvas frame that has no alpha (per bitstream flag) or uses
	// no-blend is a keyframe. This uses the bitstream-level alpha flag
	// (from VP8L header or ALPH chunk presence) rather than scanning every
	// pixel, matching the C libwebp's IsKeyFrame() which checks
	// iter->has_alpha.
	isFullFrame := f.OffsetX == 0 && f.OffsetY == 0 &&
		frameWidth(f) == canvasW && frameHeight(f) == canvasH

	if isFullFrame {
		if !f.HasAlpha || f.Blend == BlendNone {
			return true
		}
	}

	// If previous frame was disposed to background and either:
	// - previous frame covered the full canvas, or
	// - previous frame was itself a keyframe
	// then this frame is a keyframe (canvas is fully transparent).
	if d.prevDispose == DisposeBackground {
		prevFull := d.prevBounds.Min.X == 0 && d.prevBounds.Min.Y == 0 &&
			d.prevBounds.Dx() == canvasW && d.prevBounds.Dy() == canvasH
		if prevFull || d.prevFrameWasKeyframe {
			return true
		}
	}

	return false
}

// NextFrame applies the next frame to the canvas and returns a snapshot.
// The caller receives a copy of the canvas; subsequent calls do not mutate it.
func (d *AnimDecoder) NextFrame() (*image.NRGBA, time.Duration, error) {
	if !d.HasNext() {
		return nil, 0, ErrNoFrames
	}
	f := &d.anim.Frames[d.pos]
	if f.Image == nil {
		return nil, 0, ErrNilImage
	}

	keyFrame := d.isKeyFrame(d.pos)

	// Initialize currFrame.
	if keyFrame {
		// Keyframe: start from a blank transparent canvas.
		clearCanvas(d.currFrame)
	} else {
		// Non-keyframe: start from the previous disposed canvas.
		copy(d.currFrame.Pix, d.prevFrameDisposed.Pix)
	}

	// Composite the frame onto currFrame.
	d.compositeFrame(f)

	// Snapshot the current canvas for the caller.
	snap := image.NewNRGBA(d.currFrame.Bounds())
	copy(snap.Pix, d.currFrame.Pix)

	// Prepare prevFrameDisposed for the next iteration:
	// 1. Copy currFrame to prevFrameDisposed
	// 2. Apply this frame's dispose method to prevFrameDisposed
	copy(d.prevFrameDisposed.Pix, d.currFrame.Pix)
	applyDispose(d.prevFrameDisposed, f)

	// Update keyframe detection state for next frame.
	d.prevFrameWasKeyframe = keyFrame
	d.prevDispose = f.Dispose
	d.prevBounds = f.Bounds()

	d.pos++
	return snap, f.Duration, nil
}

// Reset rewinds the decoder to the first frame and clears the canvas.
func (d *AnimDecoder) Reset() {
	d.pos = 0
	clearCanvas(d.currFrame)
	clearCanvas(d.prevFrameDisposed)
	d.prevFrameWasKeyframe = false
	d.prevDispose = DisposeNone
	d.prevBounds = image.Rectangle{}
}

// Canvas returns the current canvas state (not a copy).
func (d *AnimDecoder) Canvas() *image.NRGBA {
	return d.currFrame
}

// compositeFrame blends the frame onto the current canvas.
// Frame bounds are clamped to the canvas dimensions to prevent out-of-bounds access.
func (d *AnimDecoder) compositeFrame(f *Frame) {
	src := toNRGBA(f.Image)
	rect := f.Bounds()
	srcBounds := src.Bounds()

	// Clamp frame bounds to canvas dimensions to prevent out-of-bounds writes.
	canvasBounds := d.currFrame.Bounds()
	rect = rect.Intersect(canvasBounds)
	if rect.Empty() {
		return
	}

	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		sy := y - f.OffsetY
		if sy < 0 || sy >= srcBounds.Dy() {
			continue
		}
		for x := rect.Min.X; x < rect.Max.X; x++ {
			sx := x - f.OffsetX
			if sx < 0 || sx >= srcBounds.Dx() {
				continue
			}
			srcPx := src.NRGBAAt(sx, sy)

			if f.Blend == BlendNone {
				d.currFrame.SetNRGBA(x, y, srcPx)
			} else {
				dstPx := d.currFrame.NRGBAAt(x, y)
				d.currFrame.SetNRGBA(x, y, alphaBlendNRGBA(srcPx, dstPx))
			}
		}
	}
}

// clearCanvas fills the entire canvas with transparent (0,0,0,0).
func clearCanvas(canvas *image.NRGBA) {
	for i := range canvas.Pix {
		canvas.Pix[i] = 0
	}
}

// frameWidth returns the width of the frame's image, or 0 if nil.
func frameWidth(f *Frame) int {
	if f.Image == nil {
		return 0
	}
	return f.Image.Bounds().Dx()
}

// frameHeight returns the height of the frame's image, or 0 if nil.
func frameHeight(f *Frame) int {
	if f.Image == nil {
		return 0
	}
	return f.Image.Bounds().Dy()
}

// --- AnimEncoder: mux-based encoder ---

// EncodeOptions configures the AnimEncoder.
type EncodeOptions struct {
	LoopCount       int
	BackgroundColor color.NRGBA
	Quality         int  // 0-100 for lossy encoding.
	Lossless        bool

	// AllowMixed enables mixed codec mode. When true, each frame is encoded
	// as both lossy (VP8) and lossless (VP8L), and the smaller result is used.
	// This means different frames in the same animation may use different codecs.
	// This matches the C libwebp allow_mixed option in WebPAnimEncoderOptions.
	AllowMixed bool

	// Kmin is the minimum distance between keyframes. Frames closer than Kmin
	// to the previous keyframe are always encoded as sub-frames.
	// If Kmin <= 0, defaults are applied (see sanitizeKeyframeOptions).
	Kmin int

	// Kmax is the maximum distance between keyframes. When Kmax consecutive
	// frames have been encoded since the last keyframe, the next frame is
	// forced to be a keyframe. If Kmax <= 0, keyframe insertion is disabled
	// (only the first frame is a keyframe).
	Kmax int
}

// AnimEncoder writes an animated WebP file using mux.Muxer.
//
// Design note (C conformance -- DIFF-AN3 "prev_candidate_undecided"):
// The C libwebp encoder maintains a dual-candidate buffer where each frame can
// be stored as both a keyframe and a sub-frame simultaneously, with the final
// decision deferred until later frames are encoded. It tracks this deferral via
// a "prev_candidate_undecided" flag; when set, the previous frame's rectangle
// is unknown (depends on which variant wins), so dispose-to-background is
// disabled for the current frame.
//
// The Go encoder uses an immediate-decision architecture instead: each frame is
// encoded once and committed to the muxer right away, so prevFrameRect is
// always the definitive rectangle of the finalized previous frame. This means:
//   - There is no "undecided" state to track.
//   - Dispose-to-background can always be evaluated (the prev rect is known).
//   - Retroactive dispose updates (AN2) only change the dispose method, not
//     the frame rectangle, which is correct since the rect is already final.
//
// This is functionally equivalent to the C reference for the cases this encoder
// handles (immediate keyframe decisions based on kmin/kmax thresholds).
type AnimEncoder struct {
	w      io.Writer
	muxer  *mux.Muxer
	width  int
	height int
	opts   EncodeOptions
	closed bool

	// Optimization state (used when FrameEncoderFunc is set).
	prevCanvas         *image.NRGBA       // Previous canvas state for diff computation.
	frameCount         int                // Number of frames added so far.
	countSinceKeyframe int                // Frames since the last keyframe.
	prevFrameRect      image.Rectangle    // Bounding rect of previous frame (for dispose-bg). Always valid after a frame is committed.
	prevMuxIndex       int                // Index of previous frame in muxer (for retroactive dispose update).
}

// sanitizeKeyframeOptions adjusts kmin/kmax to valid ranges, matching the
// C libwebp SanitizeEncoderOptions logic.
func sanitizeKeyframeOptions(kmin, kmax *int) {
	if *kmax <= 0 {
		// Keyframes disabled: only the first frame is a keyframe.
		*kmax = int(^uint(0) >> 1) // MaxInt
		*kmin = *kmax - 1
		return
	}
	if *kmax == 1 {
		// All frames are keyframes.
		*kmin = 0
		*kmax = 0
		return
	}
	if *kmin >= *kmax {
		*kmin = *kmax - 1
	} else {
		kminLimit := *kmax/2 + 1
		if *kmin < kminLimit && kminLimit < *kmax {
			*kmin = kminLimit
		}
	}
	// Limit the max number of cached frames.
	const maxCachedFrames = 30
	if *kmax-*kmin > maxCachedFrames {
		*kmin = *kmax - maxCachedFrames
	}
}

// clampLoopCount clamps a loop count to [0, maxLoopCount].
func clampLoopCount(v int) int {
	if v < 0 {
		return 0
	}
	if v > maxLoopCount {
		return maxLoopCount
	}
	return v
}

// NewEncoder creates a new AnimEncoder.
func NewEncoder(w io.Writer, canvasWidth, canvasHeight int, opts *EncodeOptions) *AnimEncoder {
	m := mux.NewMuxer()
	enc := &AnimEncoder{
		w:      w,
		muxer:  m,
		width:  canvasWidth,
		height: canvasHeight,
	}
	if opts != nil {
		enc.opts = *opts
	}
	enc.opts.LoopCount = clampLoopCount(enc.opts.LoopCount)
	sanitizeKeyframeOptions(&enc.opts.Kmin, &enc.opts.Kmax)
	m.SetCanvasSize(canvasWidth, canvasHeight)
	m.SetLoopCount(enc.opts.LoopCount)
	m.SetBackgroundColor(nrgbaToARGB(enc.opts.BackgroundColor))
	return enc
}

// AddFrame adds an animation frame. If FrameEncoderFunc is set, any image.Image
// is accepted and will be encoded using the configured codec with sub-frame
// optimization. Otherwise, only *bitstreamFrame (from NewBitstreamFrame) is
// accepted and no optimization is applied.
func (e *AnimEncoder) AddFrame(img image.Image, duration time.Duration) error {
	if e.closed {
		return errors.New("animation: encoder is closed")
	}
	// Fast path for pre-encoded bitstream data (no optimization possible).
	if bf, ok := img.(*bitstreamFrame); ok {
		e.frameCount++
		return e.muxer.AddFrame(bf.data, &mux.FrameOptions{
			Duration: int(duration / time.Millisecond),
		})
	}
	// Use the registered encoder function with sub-frame optimization.
	if FrameEncoderFunc != nil {
		return e.addOptimizedFrame(img, duration)
	}
	return errors.New("animation: no frame encoder available; use AddRawFrame or register FrameEncoderFunc")
}

// encodeFrame encodes an image using the configured codec. When AllowMixed is
// true, the image is encoded as both lossy and lossless, and the smaller result
// is returned. This matches the C libwebp behavior where allow_mixed causes
// each frame to be tried with both codecs independently.
func (e *AnimEncoder) encodeFrame(img image.Image, lossless bool, quality int) ([]byte, error) {
	bs, err := FrameEncoderFunc(img, lossless, quality)
	if err != nil {
		return nil, err
	}
	if !e.opts.AllowMixed {
		return bs, nil
	}
	// Try the reversed codec (lossy if configured lossless, and vice versa).
	bsAlt, errAlt := FrameEncoderFunc(img, !lossless, quality)
	if errAlt != nil {
		// If the alternate codec fails, use the primary result.
		return bs, nil
	}
	if len(bsAlt) < len(bs) {
		return bsAlt, nil
	}
	return bs, nil
}

// addOptimizedFrame encodes a frame with sub-frame rectangle detection,
// dispose method selection, and keyframe policy.
func (e *AnimEncoder) addOptimizedFrame(img image.Image, duration time.Duration) error {
	currCanvas := toNRGBA(img)

	// Ensure canvas dimensions match. If the image is smaller than the canvas,
	// place it at (0,0) on a full-canvas NRGBA.
	if currCanvas.Bounds().Dx() != e.width || currCanvas.Bounds().Dy() != e.height {
		full := image.NewNRGBA(image.Rect(0, 0, e.width, e.height))
		copyImageRect(full, currCanvas, 0, 0)
		currCanvas = full
	}

	isFirstFrame := e.frameCount == 0
	durMS := int(duration / time.Millisecond)

	if isFirstFrame {
		// First frame is always a full-canvas keyframe.
		bs, err := e.encodeFrame(currCanvas, e.opts.Lossless, e.opts.Quality)
		if err != nil {
			return fmt.Errorf("animation: encoding frame: %w", err)
		}
		if err := e.muxer.AddFrame(bs, &mux.FrameOptions{
			Duration:    durMS,
			BlendMode:   mux.BlendMode(BlendNone),
			DisposeMode: mux.DisposeMode(DisposeNone),
		}); err != nil {
			return err
		}
		e.prevCanvas = cloneNRGBA(currCanvas)
		e.prevFrameRect = image.Rect(0, 0, e.width, e.height)
		e.prevMuxIndex = e.muxer.NumFrames() - 1
		e.frameCount++
		e.countSinceKeyframe = 0
		return nil
	}

	// Check if this frame is pixel-identical to the previous canvas. If so,
	// merge it by extending the previous frame's duration instead of encoding
	// a new frame. This matches the C libwebp frame_skipped / empty-rect logic.
	if isCanvasIdentical(e.prevCanvas, currCanvas) {
		return e.increasePreviousDuration(durMS)
	}

	e.countSinceKeyframe++

	// Determine if this frame must be a keyframe.
	forceKeyframe := e.countSinceKeyframe >= e.opts.Kmax

	// If forced keyframe or the changed area is very large (>90% of canvas),
	// encode as a full-canvas keyframe.
	if forceKeyframe {
		return e.encodeKeyframe(currCanvas, durMS)
	}

	// Try sub-frame encoding with both dispose methods and pick the best.
	return e.encodeSubFrame(currCanvas, durMS)
}

// encodeKeyframe encodes the current canvas as a full-canvas keyframe.
func (e *AnimEncoder) encodeKeyframe(currCanvas *image.NRGBA, durMS int) error {
	bs, err := e.encodeFrame(currCanvas, e.opts.Lossless, e.opts.Quality)
	if err != nil {
		return fmt.Errorf("animation: encoding keyframe: %w", err)
	}
	if err := e.muxer.AddFrame(bs, &mux.FrameOptions{
		Duration:    durMS,
		BlendMode:   mux.BlendMode(BlendNone),
		DisposeMode: mux.DisposeMode(DisposeNone),
	}); err != nil {
		return err
	}
	e.prevCanvas = cloneNRGBA(currCanvas)
	e.prevFrameRect = image.Rect(0, 0, e.width, e.height)
	e.prevMuxIndex = e.muxer.NumFrames() - 1
	e.frameCount++
	e.countSinceKeyframe = 0
	return nil
}

// qualityToMaxDiff converts an encoding quality (0-100) to a maximum per-channel
// pixel difference threshold, matching the C libwebp QualityToMaxDiff:
//
//	val = pow(quality / 100., 0.5)
//	max_diff = 31 * (1 - val) + 1 * val
func qualityToMaxDiff(quality int) int {
	val := math.Pow(float64(quality)/100.0, 0.5)
	maxDiff := 31.0*(1.0-val) + 1.0*val
	return int(maxDiff + 0.5)
}

// pixelsAreSimilar checks if two NRGBA pixels are within a maximum per-channel
// difference, matching the C libwebp PixelsAreSimilar. The comparison weights
// channel differences by the destination alpha:
//
//	abs(src_ch - dst_ch) * dst_a <= max_allowed_diff * 255
func pixelsAreSimilar(src, dst color.NRGBA, maxAllowedDiff int) bool {
	if src.A != dst.A {
		return false
	}
	dstA := int(dst.A)
	threshold := maxAllowedDiff * 255
	abs := func(a, b uint8) int {
		d := int(a) - int(b)
		if d < 0 {
			return -d
		}
		return d
	}
	return abs(src.R, dst.R)*dstA <= threshold &&
		abs(src.G, dst.G)*dstA <= threshold &&
		abs(src.B, dst.B)*dstA <= threshold
}

// isLosslessBlendingPossible checks whether alpha blending can correctly
// reconstruct the target pixels in rect. For lossless encoding, blending is
// only possible when every pixel in rect either has dst_alpha == 0xFF (so
// blending has no effect) or src_pixel == dst_pixel (so blending produces the
// same result regardless).
//
// This matches the C libwebp IsLosslessBlendingPossible:
//
//	for each pixel in rect:
//	  if dst_alpha != 0xff && src_pixel != dst_pixel: return false
//
// Parameters:
//   - src: the previous canvas (carry-over from previous frame)
//   - dst: the current target canvas
//   - rect: the sub-frame rectangle being encoded
func isLosslessBlendingPossible(src, dst *image.NRGBA, rect image.Rectangle) bool {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			srcPx := src.NRGBAAt(x, y)
			dstPx := dst.NRGBAAt(x, y)
			if dstPx.A != 0xFF && (srcPx != dstPx) {
				return false
			}
		}
	}
	return true
}

// isLossyBlendingPossible checks whether alpha blending can correctly
// reconstruct the target pixels in rect for lossy encoding. This is similar
// to isLosslessBlendingPossible but uses a quality-dependent similarity
// threshold instead of exact pixel equality.
//
// This matches the C libwebp IsLossyBlendingPossible:
//
//	for each pixel in rect:
//	  if dst_alpha != 0xff && !PixelsAreSimilar(src, dst, threshold): return false
//
// Parameters:
//   - src: the previous canvas (carry-over from previous frame)
//   - dst: the current target canvas
//   - rect: the sub-frame rectangle being encoded
//   - quality: encoding quality (0-100) used to determine the similarity threshold
func isLossyBlendingPossible(src, dst *image.NRGBA, rect image.Rectangle, quality int) bool {
	maxDiff := qualityToMaxDiff(quality)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			srcPx := src.NRGBAAt(x, y)
			dstPx := dst.NRGBAAt(x, y)
			if dstPx.A != 0xFF && !pixelsAreSimilar(srcPx, dstPx, maxDiff) {
				return false
			}
		}
	}
	return true
}

// encodeSubFrame detects the bounding rectangle of changed pixels between the
// previous canvas and the current canvas, encodes only that sub-rectangle, and
// emits it to the muxer with the appropriate offset. When the changed area
// exceeds 90% of the canvas, it falls back to a full-canvas keyframe if that
// produces a smaller bitstream.
//
// Matching the C libwebp reference, this method generates two candidate
// sub-frames -- one assuming the previous frame uses DISPOSE_NONE and one
// assuming DISPOSE_BACKGROUND -- then picks the candidate with the smaller
// encoded size. When DISPOSE_BACKGROUND wins, the previous frame's dispose
// method is retroactively updated in the muxer.
//
// Note: The C reference skips the dispose-BG candidate when
// prev_candidate_undecided is true (because the previous frame's rectangle is
// unknown). In the Go encoder, frames are committed immediately so
// prevFrameRect is always valid, and both candidates can always be evaluated.
// See AnimEncoder type comment for the full DIFF-AN3 rationale.
func (e *AnimEncoder) encodeSubFrame(currCanvas *image.NRGBA, durMS int) error {
	// --- Candidate 1: DISPOSE_NONE on previous frame ---
	// The previous canvas is unchanged; diff against it directly.
	rectNone := findChangedRect(e.prevCanvas, currCanvas)
	if rectNone.Empty() {
		// No pixel changed -- encode a minimal 1x1 frame.
		rectNone = image.Rect(0, 0, 1, 1)
	}
	rectNone = snapToEven(rectNone)
	rectNone = rectNone.Intersect(image.Rect(0, 0, e.width, e.height))

	// Check if blending is possible for the DISPOSE_NONE candidate.
	// Matching C libwebp: blend mode is BLEND if validation passes, NO_BLEND otherwise.
	blendNone := BlendNone
	if e.opts.Lossless {
		if isLosslessBlendingPossible(e.prevCanvas, currCanvas, rectNone) {
			blendNone = BlendAlpha
		}
	} else {
		if isLossyBlendingPossible(e.prevCanvas, currCanvas, rectNone, e.opts.Quality) {
			blendNone = BlendAlpha
		}
	}

	subImgNone := extractSubImage(currCanvas, rectNone)
	bsNone, err := e.encodeFrame(subImgNone, e.opts.Lossless, e.opts.Quality)
	if err != nil {
		return fmt.Errorf("animation: encoding sub-frame (dispose-none): %w", err)
	}

	// --- Candidate 2: DISPOSE_BACKGROUND on previous frame ---
	// Simulate what the canvas would look like if the previous frame's
	// rectangle were cleared to transparent after display.
	var bsBG []byte
	var rectBG image.Rectangle
	blendBG := BlendNone
	prevDisposedCanvas := cloneNRGBA(e.prevCanvas)
	fillRect(prevDisposedCanvas, e.prevFrameRect, color.NRGBA{})
	rectBG = findChangedRect(prevDisposedCanvas, currCanvas)
	if rectBG.Empty() {
		rectBG = image.Rect(0, 0, 1, 1)
	}
	rectBG = snapToEven(rectBG)
	rectBG = rectBG.Intersect(image.Rect(0, 0, e.width, e.height))

	// Check if blending is possible for the DISPOSE_BG candidate.
	if e.opts.Lossless {
		if isLosslessBlendingPossible(prevDisposedCanvas, currCanvas, rectBG) {
			blendBG = BlendAlpha
		}
	} else {
		if isLossyBlendingPossible(prevDisposedCanvas, currCanvas, rectBG, e.opts.Quality) {
			blendBG = BlendAlpha
		}
	}

	subImgBG := extractSubImage(currCanvas, rectBG)
	bsBG, err = e.encodeFrame(subImgBG, e.opts.Lossless, e.opts.Quality)
	if err != nil {
		// If encoding the BG candidate fails, fall through with DISPOSE_NONE.
		bsBG = nil
	}

	// --- Pick the best candidate ---
	useBG := bsBG != nil && len(bsBG) < len(bsNone)

	bestBS := bsNone
	bestRect := rectNone
	bestDispose := DisposeNone
	bestBlend := blendNone
	if useBG {
		bestBS = bsBG
		bestRect = rectBG
		bestDispose = DisposeBackground
		bestBlend = blendBG
	}

	// If the changed area is very large, a full-canvas keyframe may compress
	// better than the sub-frame. Try both and pick the smaller one.
	canvasArea := e.width * e.height
	changedArea := bestRect.Dx() * bestRect.Dy()
	if changedArea > canvasArea*9/10 {
		bsKey, errKey := e.encodeFrame(currCanvas, e.opts.Lossless, e.opts.Quality)
		if errKey == nil && len(bsKey) < len(bestBS) {
			return e.encodeKeyframe(currCanvas, durMS)
		}
	}

	// If DISPOSE_BACKGROUND was chosen, retroactively update the previous
	// frame's dispose method in the muxer.
	if bestDispose == DisposeBackground {
		e.muxer.SetFrameDisposeMode(e.prevMuxIndex, mux.DisposeBackground)
	}

	if err := e.muxer.AddFrame(bestBS, &mux.FrameOptions{
		Duration:    durMS,
		OffsetX:     bestRect.Min.X,
		OffsetY:     bestRect.Min.Y,
		BlendMode:   mux.BlendMode(bestBlend),
		DisposeMode: mux.DisposeMode(DisposeNone),
	}); err != nil {
		return err
	}

	e.prevCanvas = cloneNRGBA(currCanvas)
	e.prevFrameRect = bestRect
	e.prevMuxIndex = e.muxer.NumFrames() - 1
	e.frameCount++
	return nil
}

// isCanvasIdentical returns true if every pixel in a and b is identical.
// Both images must have the same dimensions.
func isCanvasIdentical(a, b *image.NRGBA) bool {
	if a == nil || b == nil {
		return false
	}
	return bytes.Equal(a.Pix, b.Pix)
}

// increasePreviousDuration extends the previous frame's duration by durMS
// milliseconds, merging the current (identical) frame into the previous one.
//
// If the combined duration would exceed maxDuration (0xFFFFFF, the 24-bit
// maximum for a WebP ANMF frame duration), the previous frame's duration is
// capped at maxDuration and a new 1x1 transparent frame is emitted with
// blend-on for the remaining duration. This matches the C libwebp
// IncreasePreviousDuration() overflow handling.
func (e *AnimEncoder) increasePreviousDuration(durMS int) error {
	prevDur := e.muxer.FrameDuration(e.prevMuxIndex)
	newDur := prevDur + durMS

	if newDur < maxDuration {
		// Common case: just extend the previous frame's duration.
		e.muxer.SetFrameDuration(e.prevMuxIndex, newDur)
		return nil
	}

	// Overflow: cap the previous frame at maxDuration and emit a 1x1
	// transparent filler frame for the remaining duration.
	e.muxer.SetFrameDuration(e.prevMuxIndex, maxDuration)
	remainder := newDur - maxDuration

	// Encode a 1x1 transparent pixel as the filler frame.
	fillerImg := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	bs, err := e.encodeFrame(fillerImg, e.opts.Lossless, e.opts.Quality)
	if err != nil {
		return fmt.Errorf("animation: encoding filler frame: %w", err)
	}

	if err := e.muxer.AddFrame(bs, &mux.FrameOptions{
		Duration:    remainder,
		OffsetX:     0,
		OffsetY:     0,
		BlendMode:   mux.BlendMode(BlendAlpha), // Blend on: transparent over existing = no change.
		DisposeMode: mux.DisposeMode(DisposeNone),
	}); err != nil {
		return err
	}

	e.prevMuxIndex = e.muxer.NumFrames() - 1
	e.frameCount++
	e.countSinceKeyframe++
	// prevCanvas and prevFrameRect remain unchanged since the canvas is identical.
	return nil
}

// findChangedRect computes the bounding rectangle of pixels that differ
// between prev and curr. Both images must have the same dimensions.
// Returns an empty rectangle if all pixels are identical.
//
// Uses bytes.Equal per row for SIMD-accelerated skip of identical rows,
// then narrows X boundaries progressively within changed rows.
func findChangedRect(prev, curr *image.NRGBA) image.Rectangle {
	w := prev.Bounds().Dx()
	h := prev.Bounds().Dy()
	if w == 0 || h == 0 {
		return image.Rectangle{}
	}
	stride := prev.Stride
	rowLen := w * 4

	// Find top boundary: first changed row.
	minY := h
	for y := 0; y < h; y++ {
		off := y * stride
		if !bytes.Equal(prev.Pix[off:off+rowLen], curr.Pix[off:off+rowLen]) {
			minY = y
			break
		}
	}
	if minY == h {
		return image.Rectangle{} // identical
	}

	// Find bottom boundary: last changed row.
	maxY := minY + 1
	for y := h - 1; y > minY; y-- {
		off := y * stride
		if !bytes.Equal(prev.Pix[off:off+rowLen], curr.Pix[off:off+rowLen]) {
			maxY = y + 1
			break
		}
	}

	// Find X boundaries within changed rows (progressive narrowing).
	minX := w
	maxX := 0
	for y := minY; y < maxY; y++ {
		rowOff := y * stride
		// Scan left, only up to current minX.
		for x := 0; x < minX; x++ {
			off := rowOff + x*4
			if prev.Pix[off] != curr.Pix[off] ||
				prev.Pix[off+1] != curr.Pix[off+1] ||
				prev.Pix[off+2] != curr.Pix[off+2] ||
				prev.Pix[off+3] != curr.Pix[off+3] {
				minX = x
				break
			}
		}
		// Scan right, only beyond current maxX.
		for x := w - 1; x >= maxX; x-- {
			off := rowOff + x*4
			if prev.Pix[off] != curr.Pix[off] ||
				prev.Pix[off+1] != curr.Pix[off+1] ||
				prev.Pix[off+2] != curr.Pix[off+2] ||
				prev.Pix[off+3] != curr.Pix[off+3] {
				maxX = x + 1
				break
			}
		}
		// Early exit: we've found the widest possible range.
		if minX == 0 && maxX == w {
			break
		}
	}

	if maxX <= minX {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

// snapToEven adjusts the rectangle so offsets are even (VP8 requirement).
// When an offset is odd, the width/height is expanded by 1 to compensate,
// so the rectangle still covers the same area plus the extra pixel from
// snapping the offset down. This matches the C libwebp SnapToEvenOffsets:
//
//	rect->width  += (rect->x_offset & 1);
//	rect->height += (rect->y_offset & 1);
//	rect->x_offset &= ~1;
//	rect->y_offset &= ~1;
func snapToEven(r image.Rectangle) image.Rectangle {
	// Compute current width/height before adjusting offsets.
	w := r.Dx()
	h := r.Dy()
	// Expand width/height by 1 if the corresponding offset is odd.
	w += r.Min.X & 1
	h += r.Min.Y & 1
	// Snap offsets to even.
	minX := r.Min.X &^ 1
	minY := r.Min.Y &^ 1
	return image.Rect(minX, minY, minX+w, minY+h)
}

// extractSubImage creates a new NRGBA image containing the pixels from src
// within the given rectangle. The returned image has bounds starting at (0,0).
func extractSubImage(src *image.NRGBA, rect image.Rectangle) *image.NRGBA {
	w := rect.Dx()
	h := rect.Dy()
	if w <= 0 || h <= 0 {
		return image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		srcOff := (rect.Min.Y+y)*src.Stride + rect.Min.X*4
		dstOff := y * dst.Stride
		copy(dst.Pix[dstOff:dstOff+w*4], src.Pix[srcOff:srcOff+w*4])
	}
	return dst
}

// cloneNRGBA creates a deep copy of an NRGBA image.
func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// copyImageRect copies src pixels onto dst at the given offset.
func copyImageRect(dst *image.NRGBA, src *image.NRGBA, offX, offY int) {
	sb := src.Bounds()
	for y := sb.Min.Y; y < sb.Max.Y; y++ {
		dy := y - sb.Min.Y + offY
		if dy < 0 || dy >= dst.Bounds().Dy() {
			continue
		}
		srcOff := (y - sb.Min.Y) * src.Stride
		dstOff := dy*dst.Stride + offX*4
		w := sb.Dx()
		if offX+w > dst.Bounds().Dx() {
			w = dst.Bounds().Dx() - offX
		}
		if w > 0 {
			copy(dst.Pix[dstOff:dstOff+w*4], src.Pix[srcOff:srcOff+w*4])
		}
	}
}

// AddRawFrame adds a pre-encoded frame bitstream with options.
func (e *AnimEncoder) AddRawFrame(bitstreamData []byte, duration time.Duration, offsetX, offsetY int, blend BlendMethod, dispose DisposeMethod) error {
	if e.closed {
		return errors.New("animation: encoder is closed")
	}
	return e.muxer.AddFrame(bitstreamData, &mux.FrameOptions{
		Duration:    int(duration / time.Millisecond),
		OffsetX:     offsetX,
		OffsetY:     offsetY,
		BlendMode:   mux.BlendMode(blend),
		DisposeMode: mux.DisposeMode(dispose),
	})
}

// SetICCProfile sets the ICC color profile for the output file.
func (e *AnimEncoder) SetICCProfile(data []byte) {
	e.muxer.SetICCProfile(data)
}

// SetEXIF sets EXIF metadata for the output file.
func (e *AnimEncoder) SetEXIF(data []byte) {
	e.muxer.SetEXIF(data)
}

// SetXMP sets XMP metadata for the output file.
func (e *AnimEncoder) SetXMP(data []byte) {
	e.muxer.SetXMP(data)
}

// Close finalizes the animation and writes the WebP file to the writer.
// When there is exactly one frame and SimpleEncodeFunc is available, the
// encoder also tries encoding the image as a simple (non-animated) WebP.
// If the simple version is smaller, it is used instead. This matches the
// C libwebp OptimizeSingleFrame behavior.
func (e *AnimEncoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true

	// Assemble the animated output into a buffer first so we can compare
	// sizes with a simple (non-animated) encoding when there is 1 frame.
	var animBuf bytes.Buffer
	if err := e.muxer.Assemble(&animBuf); err != nil {
		return err
	}
	animData := animBuf.Bytes()

	// Single-frame optimization: if there is exactly 1 frame and we have
	// the canvas image and the simple encoder, try encoding as a simple
	// WebP and pick the smaller output.
	if e.frameCount == 1 && e.prevCanvas != nil && SimpleEncodeFunc != nil {
		simpleData, err := SimpleEncodeFunc(e.prevCanvas, e.opts.Lossless, float32(e.opts.Quality))
		if err == nil && len(simpleData) > 0 && len(simpleData) < len(animData) {
			_, writeErr := e.w.Write(simpleData)
			return writeErr
		}
	}

	_, err := e.w.Write(animData)
	return err
}

// bitstreamFrame wraps raw bitstream data as an image.Image for AddFrame.
type bitstreamFrame struct {
	data   []byte
	width  int
	height int
}

func (b *bitstreamFrame) ColorModel() color.Model    { return color.NRGBAModel }
func (b *bitstreamFrame) Bounds() image.Rectangle     { return image.Rect(0, 0, b.width, b.height) }
func (b *bitstreamFrame) At(_, _ int) color.Color     { return color.NRGBA{} }

// NewBitstreamFrame wraps raw VP8/VP8L data as an image.Image suitable for AddFrame.
func NewBitstreamFrame(data []byte, width, height int) image.Image {
	return &bitstreamFrame{data: data, width: width, height: height}
}

// --- Canvas blending helpers (shared with AnimDecoder) ---

// alphaBlendNRGBA performs "src over dst" compositing in non-premultiplied RGBA.
// This matches the C libwebp BlendPixelNonPremult formula:
//   dst_factor_a = (dst_a * (256 - src_a)) >> 8
//   blend_a = src_a + dst_factor_a
//   channel = (src_channel * src_a + dst_channel * dst_factor_a) * scale >> 24
// where scale = (1 << 24) / blend_a
func alphaBlendNRGBA(src, dst color.NRGBA) color.NRGBA {
	if src.A == 0 {
		return dst
	}
	if src.A == 255 || dst.A == 0 {
		return src
	}

	srcA := uint32(src.A)
	dstA := uint32(dst.A)

	// C: dst_factor_a = (dst_a * (256 - src_a)) >> 8
	dstFactorA := (dstA * (256 - srcA)) >> 8
	blendA := srcA + dstFactorA
	if blendA == 0 {
		return color.NRGBA{}
	}

	// C: scale = (1 << 24) / blend_a
	scale := (1 << 24) / blendA

	blend := func(sc, dc uint8) uint8 {
		// C: (src_channel * src_a + dst_channel * dst_factor_a) * scale >> 24
		v := (uint32(sc)*srcA + uint32(dc)*dstFactorA) * scale >> 24
		if v > 255 {
			v = 255
		}
		return uint8(v)
	}

	return color.NRGBA{
		R: blend(src.R, dst.R),
		G: blend(src.G, dst.G),
		B: blend(src.B, dst.B),
		A: uint8(blendA),
	}
}

// applyDispose modifies the canvas based on the frame's dispose method.
// Per the C libwebp reference, dispose-to-background fills with transparent
// (0,0,0,0), not the container's background color.
func applyDispose(canvas *image.NRGBA, f *Frame) {
	if f.Dispose == DisposeBackground {
		fillRect(canvas, f.Bounds(), color.NRGBA{})
	}
}

// fillRect fills a rectangle on the canvas with a solid color.
func fillRect(canvas *image.NRGBA, rect image.Rectangle, c color.NRGBA) {
	rect = rect.Intersect(canvas.Bounds())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			canvas.SetNRGBA(x, y, c)
		}
	}
}
