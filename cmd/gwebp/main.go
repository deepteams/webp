// Command gwebp encodes and decodes WebP images from the command line.
//
// Usage:
//
//	gwebp enc [options] <input>        PNG/JPEG/GIF → WebP (use "-" for stdin)
//	gwebp dec [options] <input.webp>   WebP → PNG/JPEG/GIF (use "-" for stdin, -o - for stdout)
//	gwebp info <input.webp>            Display WebP metadata
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"bytes"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepteams/webp"
	"github.com/deepteams/webp/animation"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "enc":
		err = runEnc(os.Args[2:])
	case "dec":
		err = runDec(os.Args[2:])
	case "info":
		err = runInfo(os.Args[2:])
	case "-h", "-help", "--help", "help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "gwebp: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "gwebp: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  gwebp enc [options] <input>        Encode PNG/JPEG/GIF to WebP
  gwebp dec [options] <input.webp>   Decode WebP to PNG, JPEG, or GIF

Use "-" as input to read from stdin, "-o -" to write to stdout.

Run "gwebp <command> -h" for command-specific options.
`)
}

// openInput returns an io.ReadCloser for the given path.
// If path is "-", stdin is returned (caller should not close).
func openInput(path string) (io.ReadCloser, error) {
	if path == "-" {
		return io.NopCloser(os.Stdin), nil
	}
	return os.Open(path)
}

// --- enc ---

func runEnc(args []string) error {
	fs := flag.NewFlagSet("enc", flag.ContinueOnError)
	quality := fs.Float64("q", 75, "quality 0-100")
	lossless := fs.Bool("lossless", false, "lossless VP8L encoding")
	method := fs.Int("m", 4, "compression effort 0-6")
	preset := fs.String("preset", "default", "preset: default/picture/photo/drawing/icon/text")
	sharpYUV := fs.Bool("sharp_yuv", false, "sharp RGB→YUV conversion")
	exact := fs.Bool("exact", false, "preserve RGB in transparent areas")
	targetSize := fs.Int("size", 0, "target size in bytes (0=use quality)")
	targetPSNR := fs.Float64("psnr", 0, "target PSNR in dB (0=use quality)")
	sns := fs.Int("sns", -1, "spatial noise shaping 0-100 (-1=default)")
	filterStrength := fs.Int("f", -1, "filter strength 0-100 (-1=default)")
	filterSharpness := fs.Int("sharpness", 0, "filter sharpness 0-7")
	strong := fs.Bool("strong", false, "use strong filter (default)")
	nostrong := fs.Bool("nostrong", false, "use simple filter instead of strong")
	segments := fs.Int("segments", -1, "number of segments 1-4 (-1=default)")
	pass := fs.Int("pass", -1, "analysis pass number 1-10 (-1=default)")
	alphaQ := fs.Int("alpha_q", -1, "alpha quality 0-100 (-1=default)")
	alphaMethod := fs.Int("alpha_method", -1, "alpha compression 0-1 (-1=default)")
	alphaFilter := fs.String("alpha_filter", "", "alpha filter: none/fast/best")
	pre := fs.Int("pre", 0, "pre-processing filter 0-3")
	qmin := fs.Int("qmin", 0, "minimum quality 0-100")
	qmax := fs.Int("qmax", -1, "maximum quality 0-100 (-1=default)")
	output := fs.String("o", "", `output path (default: <input>.webp, "-" for stdout)`)

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("enc: missing input file\nUsage: gwebp enc [options] <input>")
	}
	inputPath := fs.Arg(0)

	p, err := parsePreset(*preset)
	if err != nil {
		return err
	}

	// Start from preset defaults (like cwebp: -preset must come first),
	// then override with explicitly-set CLI flags.
	opts := webp.OptionsForPreset(p, float32(*quality))
	opts.Lossless = *lossless
	opts.Method = *method
	opts.UseSharpYUV = *sharpYUV
	opts.Exact = *exact
	opts.TargetSize = *targetSize
	opts.TargetPSNR = float32(*targetPSNR)
	opts.QMin = *qmin
	// Only override preset values when explicitly set by CLI flags.
	if *sns >= 0 {
		opts.SNSStrength = *sns
	}
	if *filterStrength >= 0 {
		opts.FilterStrength = *filterStrength
	}
	if *filterSharpness != 0 {
		opts.FilterSharpness = *filterSharpness
	}
	if *nostrong {
		opts.FilterType = 0
	} else if *strong {
		opts.FilterType = 1
	}
	if *segments >= 0 {
		opts.Segments = *segments
	}
	if *pass >= 0 {
		opts.Pass = *pass
	}
	if *pre != 0 {
		opts.Preprocessing = *pre
	}
	if *alphaQ >= 0 {
		opts.AlphaQuality = *alphaQ
	}
	if *alphaMethod >= 0 {
		opts.AlphaCompression = *alphaMethod
	}
	if *alphaFilter != "" {
		switch strings.ToLower(*alphaFilter) {
		case "none":
			opts.AlphaFiltering = 0
		case "fast":
			opts.AlphaFiltering = 1
		case "best":
			opts.AlphaFiltering = 2
		default:
			return fmt.Errorf("enc: unknown alpha_filter %q (use none/fast/best)", *alphaFilter)
		}
	}
	if *qmax >= 0 {
		opts.QMax = *qmax
	}

	ext := strings.ToLower(filepath.Ext(inputPath))
	if ext == ".gif" && inputPath != "-" {
		return encodeGIF(inputPath, *output, opts)
	}
	return encodeStatic(inputPath, *output, opts)
}

func parsePreset(s string) (webp.Preset, error) {
	switch strings.ToLower(s) {
	case "default":
		return webp.PresetDefault, nil
	case "picture":
		return webp.PresetPicture, nil
	case "photo":
		return webp.PresetPhoto, nil
	case "drawing":
		return webp.PresetDrawing, nil
	case "icon":
		return webp.PresetIcon, nil
	case "text":
		return webp.PresetText, nil
	default:
		return 0, fmt.Errorf("enc: unknown preset %q", s)
	}
}

func encodeStatic(inputPath, outputPath string, opts *webp.EncoderOptions) error {
	in, err := openInput(inputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	img, _, err := image.Decode(in)
	if err != nil {
		return fmt.Errorf("enc: decoding input: %w", err)
	}

	if outputPath == "-" {
		return webp.Encode(os.Stdout, img, opts)
	}

	if outputPath == "" {
		if inputPath == "-" {
			outputPath = "output.webp"
		} else {
			base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
			outputPath = base + ".webp"
		}
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}

	if err := webp.Encode(out, img, opts); err != nil {
		out.Close()
		os.Remove(outputPath)
		return fmt.Errorf("enc: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(outputPath)
		return err
	}

	fi, _ := os.Stat(outputPath)
	fmt.Fprintf(os.Stderr, "Encoded %s → %s (%d bytes)\n", inputPath, outputPath, fi.Size())
	return nil
}

func encodeGIF(inputPath, outputPath string, opts *webp.EncoderOptions) error {
	f, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	g, err := gif.DecodeAll(f)
	if err != nil {
		return fmt.Errorf("enc: decoding GIF: %w", err)
	}

	if len(g.Image) == 0 {
		return fmt.Errorf("enc: GIF has no frames")
	}

	if outputPath == "-" {
		return encodeGIFFrames(os.Stdout, g, opts)
	}

	if outputPath == "" {
		base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
		outputPath = base + ".webp"
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}

	if err := encodeGIFFrames(out, g, opts); err != nil {
		out.Close()
		os.Remove(outputPath)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(outputPath)
		return err
	}

	fi, _ := os.Stat(outputPath)
	fmt.Fprintf(os.Stderr, "Encoded %s → %s (%d frames, %d bytes)\n", inputPath, outputPath, len(g.Image), fi.Size())
	return nil
}

func encodeGIFFrames(w io.Writer, g *gif.GIF, opts *webp.EncoderOptions) error {
	canvasW := g.Config.Width
	canvasH := g.Config.Height
	if canvasW == 0 || canvasH == 0 {
		canvasW = g.Image[0].Bounds().Dx()
		canvasH = g.Image[0].Bounds().Dy()
	}

	enc := animation.NewEncoder(w, canvasW, canvasH, &animation.EncodeOptions{
		LoopCount: g.LoopCount,
		Lossless:  opts.Lossless,
		Quality:   int(opts.Quality),
	})

	canvas := image.NewNRGBA(image.Rect(0, 0, canvasW, canvasH))

	for i, frame := range g.Image {
		b := frame.Bounds()

		var disposal byte
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}

		// Save region for DisposalPrevious before compositing.
		var saved []uint8
		if disposal == gif.DisposalPrevious {
			saved = saveCanvasRect(canvas, b)
		}

		// Composite frame onto persistent canvas.
		draw.Draw(canvas, b, frame, b.Min, draw.Over)

		// Snapshot full canvas for the encoder.
		snap := image.NewNRGBA(canvas.Bounds())
		copy(snap.Pix, canvas.Pix)

		delay := 100 * time.Millisecond // default
		if i < len(g.Delay) && g.Delay[i] > 0 {
			delay = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}

		if err := enc.AddFrame(snap, delay); err != nil {
			return fmt.Errorf("enc: frame %d: %w", i, err)
		}

		// Apply disposal method to prepare canvas for next frame.
		switch disposal {
		case gif.DisposalBackground:
			clearCanvasRect(canvas, b)
		case gif.DisposalPrevious:
			restoreCanvasRect(canvas, b, saved)
		}
	}

	return enc.Close()
}

// --- dec ---

func runDec(args []string) error {
	fs := flag.NewFlagSet("dec", flag.ContinueOnError)
	output := fs.String("o", "", `output path (default: .png or .gif, "-" for stdout)`)
	fmtFlag := fs.String("fmt", "", "output format: png, jpeg (auto-detect from extension if omitted)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("dec: missing input file\nUsage: gwebp dec [options] <input.webp>")
	}
	inputPath := fs.Arg(0)

	in, err := openInput(inputPath)
	if err != nil {
		return err
	}

	data, err := io.ReadAll(in)
	in.Close()
	if err != nil {
		return fmt.Errorf("dec: reading input: %w", err)
	}

	feat, err := webp.GetFeatures(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("dec: %w", err)
	}

	if feat.HasAnimation {
		return decodeAnimated(data, inputPath, *output, feat)
	}
	return decodeStatic(data, inputPath, *output, *fmtFlag)
}

// detectOutputFormat returns "png", "jpeg", or "gif" based on flag/extension.
func detectOutputFormat(fmtFlag, outputPath string) string {
	if fmtFlag != "" {
		return strings.ToLower(fmtFlag)
	}
	if outputPath != "" && outputPath != "-" {
		switch strings.ToLower(filepath.Ext(outputPath)) {
		case ".jpg", ".jpeg":
			return "jpeg"
		case ".gif":
			return "gif"
		}
	}
	return "png"
}

func decodeStatic(data []byte, inputPath, outputPath, fmtFlag string) error {
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("dec: %w", err)
	}

	outFmt := detectOutputFormat(fmtFlag, outputPath)

	// Determine output writer.
	if outputPath == "-" {
		return encodeImage(os.Stdout, img, outFmt)
	}

	if outputPath == "" {
		ext := ".png"
		if outFmt == "jpeg" {
			ext = ".jpg"
		}
		if inputPath == "-" {
			outputPath = "output" + ext
		} else {
			base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
			outputPath = base + ext
		}
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}

	if err := encodeImage(out, img, outFmt); err != nil {
		out.Close()
		os.Remove(outputPath)
		return fmt.Errorf("dec: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(outputPath)
		return err
	}

	fmt.Fprintf(os.Stderr, "Decoded %s → %s\n", inputPath, outputPath)
	return nil
}

// encodeImage writes img in the specified format to w.
func encodeImage(w io.Writer, img image.Image, format string) error {
	switch format {
	case "jpeg", "jpg":
		return jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	default:
		return png.Encode(w, img)
	}
}

func decodeAnimated(data []byte, inputPath, outputPath string, feat *webp.Features) error {
	anim, err := animation.DecodeBytes(data)
	if err != nil {
		return fmt.Errorf("dec: %w", err)
	}

	if err := anim.DecodeFrames(); err != nil {
		return fmt.Errorf("dec: decoding frames: %w", err)
	}

	dec, err := animation.NewAnimDecoder(anim)
	if err != nil {
		return fmt.Errorf("dec: %w", err)
	}
	g := &gif.GIF{
		LoopCount: anim.LoopCount,
	}

	for dec.HasNext() {
		frame, dur, err := dec.NextFrame()
		if err != nil {
			return fmt.Errorf("dec: %w", err)
		}

		// Quantize to paletted image using Plan9 palette + Floyd-Steinberg dithering.
		b := frame.Bounds()
		paletted := image.NewPaletted(b, palette.Plan9)
		draw.FloydSteinberg.Draw(paletted, b, frame, b.Min)

		g.Image = append(g.Image, paletted)
		// GIF delay is in 1/100th of a second.
		delay := int(dur / (10 * time.Millisecond))
		if delay < 1 {
			delay = 10 // default 100ms
		}
		g.Delay = append(g.Delay, delay)
	}

	if outputPath == "-" {
		return gif.EncodeAll(os.Stdout, g)
	}

	if outputPath == "" {
		if inputPath == "-" {
			outputPath = "output.gif"
		} else {
			base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
			outputPath = base + ".gif"
		}
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}

	if err := gif.EncodeAll(out, g); err != nil {
		out.Close()
		os.Remove(outputPath)
		return fmt.Errorf("dec: encoding GIF: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(outputPath)
		return err
	}

	fmt.Fprintf(os.Stderr, "Decoded %s → %s (%d frames)\n", inputPath, outputPath, len(g.Image))
	return nil
}

// saveCanvasRect copies pixel data from the given rect of the canvas.
func saveCanvasRect(canvas *image.NRGBA, r image.Rectangle) []uint8 {
	r = r.Intersect(canvas.Bounds())
	if r.Empty() {
		return nil
	}
	w := r.Dx() * 4
	saved := make([]uint8, r.Dy()*w)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		srcOff := canvas.PixOffset(r.Min.X, y)
		dstOff := (y - r.Min.Y) * w
		copy(saved[dstOff:dstOff+w], canvas.Pix[srcOff:srcOff+w])
	}
	return saved
}

// restoreCanvasRect pastes previously saved pixel data back into the canvas rect.
func restoreCanvasRect(canvas *image.NRGBA, r image.Rectangle, saved []uint8) {
	r = r.Intersect(canvas.Bounds())
	if r.Empty() || saved == nil {
		return
	}
	w := r.Dx() * 4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		dstOff := canvas.PixOffset(r.Min.X, y)
		srcOff := (y - r.Min.Y) * w
		copy(canvas.Pix[dstOff:dstOff+w], saved[srcOff:srcOff+w])
	}
}

// clearCanvasRect fills the given rect of the canvas with transparent black (0,0,0,0).
func clearCanvasRect(canvas *image.NRGBA, r image.Rectangle) {
	r = r.Intersect(canvas.Bounds())
	if r.Empty() {
		return
	}
	w := r.Dx() * 4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		off := canvas.PixOffset(r.Min.X, y)
		for i := off; i < off+w; i++ {
			canvas.Pix[i] = 0
		}
	}
}

// --- info ---

func runInfo(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("info: missing input file\nUsage: gwebp info <input.webp>")
	}
	inputPath := args[0]

	in, err := openInput(inputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	feat, err := webp.GetFeatures(in)
	if err != nil {
		return fmt.Errorf("info: %w", err)
	}

	name := inputPath
	if inputPath == "-" {
		name = "<stdin>"
	}

	fmt.Printf("File:       %s\n", name)
	fmt.Printf("Format:     %s\n", feat.Format)
	fmt.Printf("Dimensions: %d x %d\n", feat.Width, feat.Height)
	fmt.Printf("Alpha:      %v\n", feat.HasAlpha)
	fmt.Printf("Animation:  %v\n", feat.HasAnimation)
	if feat.HasAnimation {
		fmt.Printf("Frames:     %d\n", feat.FrameCount)
		loop := "infinite"
		if feat.LoopCount > 0 {
			loop = fmt.Sprintf("%d", feat.LoopCount)
		}
		fmt.Printf("Loop count: %s\n", loop)
	}

	if inputPath != "-" {
		fi, err := os.Stat(inputPath)
		if err == nil {
			fmt.Printf("File size:  %d bytes\n", fi.Size())
		}
	}

	return nil
}
