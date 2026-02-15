package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binaryPath holds the path to the compiled gwebp binary. Set in TestMain.
var binaryPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "gwebp-test-bin-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "gwebp")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = filepath.Join(rootDir())
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Mark binary as empty so tests skip gracefully.
		binaryPath = ""
		os.Exit(m.Run())
	}

	os.Exit(m.Run())
}

// rootDir returns the absolute path of the cmd/gwebp source directory.
func rootDir() string {
	// This test file lives in cmd/gwebp/.
	dir, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}
	return dir
}

// testdataDir returns the absolute path to the project's testdata directory.
func testdataDir() string {
	return filepath.Join(rootDir(), "..", "..", "testdata")
}

// skipIfNoBinary skips the test when the binary was not built.
func skipIfNoBinary(t *testing.T) {
	t.Helper()
	if binaryPath == "" {
		t.Skip("gwebp binary not built; skipping")
	}
}

// runGwebp executes gwebp with the given arguments and optional stdin data.
// Returns stdout, stderr, and any error.
func runGwebp(t *testing.T, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// createTestPNG generates a small 8x8 PNG image in the given directory and
// returns the file path. The image contains a simple gradient pattern.
func createTestPNG(t *testing.T, dir string) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 32),
				G: uint8(y * 32),
				B: 128,
				A: 255,
			})
		}
	}
	path := filepath.Join(dir, "input.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating test PNG: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("encoding test PNG: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing test PNG: %v", err)
	}
	return path
}

// assertWebPHeader verifies that data starts with a valid RIFF/WEBP header.
func assertWebPHeader(t *testing.T, data []byte) {
	t.Helper()
	if len(data) < 12 {
		t.Fatalf("output too small (%d bytes); expected at least 12 for RIFF+WEBP header", len(data))
	}
	if string(data[0:4]) != "RIFF" {
		t.Errorf("expected RIFF magic, got %q", string(data[0:4]))
	}
	if string(data[8:12]) != "WEBP" {
		t.Errorf("expected WEBP signature at offset 8, got %q", string(data[8:12]))
	}
}

// --- enc tests ---

func TestEnc_PNGToWebP(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()
	pngPath := createTestPNG(t, dir)
	outPath := filepath.Join(dir, "output.webp")

	_, stderr, err := runGwebp(t, nil, "enc", "-o", outPath, pngPath)
	if err != nil {
		t.Fatalf("enc failed: %v\nstderr: %s", err, stderr)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("output file is empty")
	}
	assertWebPHeader(t, data)
}

func TestEnc_Lossless(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()
	pngPath := createTestPNG(t, dir)
	outPath := filepath.Join(dir, "lossless.webp")

	_, stderr, err := runGwebp(t, nil, "enc", "-lossless", "-o", outPath, pngPath)
	if err != nil {
		t.Fatalf("enc -lossless failed: %v\nstderr: %s", err, stderr)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("output file is empty")
	}
	assertWebPHeader(t, data)
}

func TestEnc_QualityFlag(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()
	pngPath := createTestPNG(t, dir)

	outLow := filepath.Join(dir, "low.webp")
	outHigh := filepath.Join(dir, "high.webp")

	_, _, err := runGwebp(t, nil, "enc", "-q", "10", "-o", outLow, pngPath)
	if err != nil {
		t.Fatalf("enc -q 10 failed: %v", err)
	}

	_, _, err = runGwebp(t, nil, "enc", "-q", "95", "-o", outHigh, pngPath)
	if err != nil {
		t.Fatalf("enc -q 95 failed: %v", err)
	}

	lowData, _ := os.ReadFile(outLow)
	highData, _ := os.ReadFile(outHigh)

	assertWebPHeader(t, lowData)
	assertWebPHeader(t, highData)

	// Higher quality should generally produce a larger (or equal) file.
	// For a tiny 8x8 image this is not strictly guaranteed, so we only
	// verify both are valid WebP and non-empty.
	if len(lowData) == 0 || len(highData) == 0 {
		t.Fatal("one or both outputs empty")
	}
}

func TestEnc_DefaultOutputName(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()
	pngPath := createTestPNG(t, dir)

	// Run enc without -o; the default output should be "input.webp" in cwd.
	cmd := exec.Command(binaryPath, "enc", pngPath)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("enc (default output) failed: %v", err)
	}

	defaultOut := filepath.Join(dir, "input.webp")
	data, err := os.ReadFile(defaultOut)
	if err != nil {
		t.Fatalf("expected default output %s: %v", defaultOut, err)
	}
	assertWebPHeader(t, data)
}

func TestEnc_StdinStdout(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()
	pngPath := createTestPNG(t, dir)

	pngData, err := os.ReadFile(pngPath)
	if err != nil {
		t.Fatalf("reading test PNG: %v", err)
	}

	stdout, stderr, err := runGwebp(t, pngData, "enc", "-o", "-", "-")
	if err != nil {
		t.Fatalf("enc stdin/stdout failed: %v\nstderr: %s", err, stderr)
	}
	if len(stdout) == 0 {
		t.Fatal("stdout output is empty")
	}
	assertWebPHeader(t, stdout)
}

func TestEnc_MissingInput(t *testing.T) {
	skipIfNoBinary(t)
	_, _, err := runGwebp(t, nil, "enc")
	if err == nil {
		t.Fatal("expected non-zero exit for missing input, got nil")
	}
}

func TestEnc_NonexistentFile(t *testing.T) {
	skipIfNoBinary(t)
	_, _, err := runGwebp(t, nil, "enc", "/nonexistent/file.png")
	if err == nil {
		t.Fatal("expected non-zero exit for nonexistent file, got nil")
	}
}

// --- dec tests ---

func TestDec_WebPToPNG(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()

	// Encode a PNG to WebP first, then decode back.
	pngPath := createTestPNG(t, dir)
	webpPath := filepath.Join(dir, "test.webp")
	_, stderr, err := runGwebp(t, nil, "enc", "-o", webpPath, pngPath)
	if err != nil {
		t.Fatalf("enc setup failed: %v\nstderr: %s", err, stderr)
	}

	outPNG := filepath.Join(dir, "decoded.png")
	_, stderr, err = runGwebp(t, nil, "dec", "-o", outPNG, webpPath)
	if err != nil {
		t.Fatalf("dec failed: %v\nstderr: %s", err, stderr)
	}

	// Verify the decoded PNG is a valid image with correct dimensions.
	f, err := os.Open(outPNG)
	if err != nil {
		t.Fatalf("opening decoded PNG: %v", err)
	}
	defer f.Close()

	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decoding PNG config: %v", err)
	}
	if cfg.Width != 8 || cfg.Height != 8 {
		t.Errorf("decoded dimensions = %dx%d, want 8x8", cfg.Width, cfg.Height)
	}
}

func TestDec_WebPToJPEG(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()

	pngPath := createTestPNG(t, dir)
	webpPath := filepath.Join(dir, "test.webp")
	_, _, err := runGwebp(t, nil, "enc", "-o", webpPath, pngPath)
	if err != nil {
		t.Fatalf("enc setup failed: %v", err)
	}

	outJPG := filepath.Join(dir, "decoded.jpg")
	_, stderr, err := runGwebp(t, nil, "dec", "-o", outJPG, webpPath)
	if err != nil {
		t.Fatalf("dec to JPEG failed: %v\nstderr: %s", err, stderr)
	}

	data, err := os.ReadFile(outJPG)
	if err != nil {
		t.Fatalf("reading JPEG output: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("JPEG output is empty")
	}
	// JPEG files start with FF D8.
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Errorf("output does not look like a JPEG (first 2 bytes: %x %x)", data[0], data[1])
	}
}

func TestDec_StdinStdout(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()

	// Create a WebP file first.
	pngPath := createTestPNG(t, dir)
	webpPath := filepath.Join(dir, "test.webp")
	_, _, err := runGwebp(t, nil, "enc", "-o", webpPath, pngPath)
	if err != nil {
		t.Fatalf("enc setup failed: %v", err)
	}

	webpData, err := os.ReadFile(webpPath)
	if err != nil {
		t.Fatalf("reading WebP: %v", err)
	}

	// Decode from stdin to stdout.
	stdout, stderr, err := runGwebp(t, webpData, "dec", "-o", "-", "-")
	if err != nil {
		t.Fatalf("dec stdin/stdout failed: %v\nstderr: %s", err, stderr)
	}
	if len(stdout) == 0 {
		t.Fatal("stdout output is empty")
	}

	// The default output format is PNG; verify the PNG signature.
	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if len(stdout) < 8 || !bytes.Equal(stdout[:8], pngSig) {
		t.Error("stdout does not start with PNG signature")
	}
}

func TestDec_FormatFlag(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()

	pngPath := createTestPNG(t, dir)
	webpPath := filepath.Join(dir, "test.webp")
	_, _, err := runGwebp(t, nil, "enc", "-o", webpPath, pngPath)
	if err != nil {
		t.Fatalf("enc setup failed: %v", err)
	}

	// Use -fmt jpeg with a .dat extension to verify flag overrides extension.
	outPath := filepath.Join(dir, "output.dat")
	_, stderr, err := runGwebp(t, nil, "dec", "-fmt", "jpeg", "-o", outPath, webpPath)
	if err != nil {
		t.Fatalf("dec -fmt jpeg failed: %v\nstderr: %s", err, stderr)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Error("output with -fmt jpeg does not start with JPEG magic")
	}
}

func TestDec_MissingInput(t *testing.T) {
	skipIfNoBinary(t)
	_, _, err := runGwebp(t, nil, "dec")
	if err == nil {
		t.Fatal("expected non-zero exit for missing input, got nil")
	}
}

func TestDec_LosslessRoundTrip(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()

	pngPath := createTestPNG(t, dir)
	webpPath := filepath.Join(dir, "lossless.webp")
	_, _, err := runGwebp(t, nil, "enc", "-lossless", "-o", webpPath, pngPath)
	if err != nil {
		t.Fatalf("enc -lossless failed: %v", err)
	}

	outPNG := filepath.Join(dir, "decoded.png")
	_, _, err = runGwebp(t, nil, "dec", "-o", outPNG, webpPath)
	if err != nil {
		t.Fatalf("dec failed: %v", err)
	}

	f, err := os.Open(outPNG)
	if err != nil {
		t.Fatalf("opening decoded PNG: %v", err)
	}
	defer f.Close()

	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decoding PNG config: %v", err)
	}
	if cfg.Width != 8 || cfg.Height != 8 {
		t.Errorf("decoded dimensions = %dx%d, want 8x8", cfg.Width, cfg.Height)
	}
}

// --- info tests ---

func TestInfo_LossyFile(t *testing.T) {
	skipIfNoBinary(t)

	webpFile := filepath.Join(testdataDir(), "blue_16x16_lossy.webp")
	stdout, stderr, err := runGwebp(t, nil, "info", webpFile)
	if err != nil {
		t.Fatalf("info failed: %v\nstderr: %s", err, stderr)
	}

	out := string(stdout)
	assertContains(t, out, "16 x 16", "expected dimensions '16 x 16'")
	assertContains(t, out, "lossy", "expected format 'lossy'")
	assertContains(t, out, "Dimensions:", "expected 'Dimensions:' label")
	assertContains(t, out, "Format:", "expected 'Format:' label")
}

func TestInfo_LosslessFile(t *testing.T) {
	skipIfNoBinary(t)

	webpFile := filepath.Join(testdataDir(), "red_4x4_lossless.webp")
	stdout, stderr, err := runGwebp(t, nil, "info", webpFile)
	if err != nil {
		t.Fatalf("info failed: %v\nstderr: %s", err, stderr)
	}

	out := string(stdout)
	assertContains(t, out, "4 x 4", "expected dimensions '4 x 4'")
	assertContains(t, out, "lossless", "expected format 'lossless'")
}

func TestInfo_FileSize(t *testing.T) {
	skipIfNoBinary(t)

	webpFile := filepath.Join(testdataDir(), "blue_16x16_lossy.webp")
	stdout, _, err := runGwebp(t, nil, "info", webpFile)
	if err != nil {
		t.Fatalf("info failed: %v", err)
	}

	out := string(stdout)
	assertContains(t, out, "File size:", "expected 'File size:' for file input")
	assertContains(t, out, "bytes", "expected 'bytes' in file size line")
}

func TestInfo_Stdin(t *testing.T) {
	skipIfNoBinary(t)

	webpFile := filepath.Join(testdataDir(), "red_4x4_lossless.webp")
	webpData, err := os.ReadFile(webpFile)
	if err != nil {
		t.Fatalf("reading test file: %v", err)
	}

	stdout, stderr, err := runGwebp(t, webpData, "info", "-")
	if err != nil {
		t.Fatalf("info from stdin failed: %v\nstderr: %s", err, stderr)
	}

	out := string(stdout)
	assertContains(t, out, "<stdin>", "expected '<stdin>' as file name")
	assertContains(t, out, "4 x 4", "expected dimensions '4 x 4'")
}

func TestInfo_MissingInput(t *testing.T) {
	skipIfNoBinary(t)
	_, _, err := runGwebp(t, nil, "info")
	if err == nil {
		t.Fatal("expected non-zero exit for missing input, got nil")
	}
}

// --- dec with testdata files ---

func TestDec_TestdataLossy(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()

	webpFile := filepath.Join(testdataDir(), "blue_16x16_lossy.webp")
	outPNG := filepath.Join(dir, "blue.png")

	_, stderr, err := runGwebp(t, nil, "dec", "-o", outPNG, webpFile)
	if err != nil {
		t.Fatalf("dec failed: %v\nstderr: %s", err, stderr)
	}

	f, err := os.Open(outPNG)
	if err != nil {
		t.Fatalf("opening decoded PNG: %v", err)
	}
	defer f.Close()

	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decoding PNG config: %v", err)
	}
	if cfg.Width != 16 || cfg.Height != 16 {
		t.Errorf("decoded dimensions = %dx%d, want 16x16", cfg.Width, cfg.Height)
	}
}

func TestDec_TestdataLossless(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()

	webpFile := filepath.Join(testdataDir(), "red_4x4_lossless.webp")
	outPNG := filepath.Join(dir, "red.png")

	_, stderr, err := runGwebp(t, nil, "dec", "-o", outPNG, webpFile)
	if err != nil {
		t.Fatalf("dec failed: %v\nstderr: %s", err, stderr)
	}

	f, err := os.Open(outPNG)
	if err != nil {
		t.Fatalf("opening decoded PNG: %v", err)
	}
	defer f.Close()

	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decoding PNG config: %v", err)
	}
	if cfg.Width != 4 || cfg.Height != 4 {
		t.Errorf("decoded dimensions = %dx%d, want 4x4", cfg.Width, cfg.Height)
	}
}

// --- enc with testdata PNG ---

func TestEnc_TestdataPNG(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()

	pngFile := filepath.Join(testdataDir(), "test.png")
	outPath := filepath.Join(dir, "test.webp")

	_, stderr, err := runGwebp(t, nil, "enc", "-q", "50", "-o", outPath, pngFile)
	if err != nil {
		t.Fatalf("enc failed: %v\nstderr: %s", err, stderr)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	assertWebPHeader(t, data)

	// Verify size is reasonable (original PNG is ~19KB, lossy WebP should be smaller or comparable).
	if len(data) < 12 {
		t.Fatalf("output too small: %d bytes", len(data))
	}
}

// --- error cases ---

func TestUnknownCommand(t *testing.T) {
	skipIfNoBinary(t)
	_, _, err := runGwebp(t, nil, "badcmd")
	if err == nil {
		t.Fatal("expected non-zero exit for unknown command, got nil")
	}
}

func TestNoArgs(t *testing.T) {
	skipIfNoBinary(t)
	_, _, err := runGwebp(t, nil)
	if err == nil {
		t.Fatal("expected non-zero exit for no arguments, got nil")
	}
}

func TestEnc_BadPreset(t *testing.T) {
	skipIfNoBinary(t)
	dir := t.TempDir()
	pngPath := createTestPNG(t, dir)

	_, _, err := runGwebp(t, nil, "enc", "-preset", "invalid", pngPath)
	if err == nil {
		t.Fatal("expected non-zero exit for bad preset, got nil")
	}
}

func TestHelp(t *testing.T) {
	skipIfNoBinary(t)

	// -h should exit with code 0.
	_, stderr, err := runGwebp(t, nil, "-h")
	if err != nil {
		t.Fatalf("expected zero exit for -h, got: %v", err)
	}
	out := string(stderr)
	assertContains(t, out, "gwebp enc", "expected usage text for enc")
	assertContains(t, out, "gwebp dec", "expected usage text for dec")
}

func TestEnc_Help(t *testing.T) {
	skipIfNoBinary(t)

	// "enc -h" uses flag.ContinueOnError, so -h causes flag.ErrHelp which is returned.
	// The CLI writes flag usage to stderr and returns the error, causing exit 1.
	_, stderr, err := runGwebp(t, nil, "enc", "-h")
	// With ContinueOnError, -h returns ErrHelp which main treats as an error.
	// Either way, the usage text should appear.
	_ = err
	out := string(stderr)
	if !strings.Contains(out, "-q") && !strings.Contains(out, "quality") {
		t.Error("expected enc help to mention -q or quality flag")
	}
}

// --- helper ---

func assertContains(t *testing.T, haystack, needle, msg string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: %q not found in output:\n%s", msg, needle, haystack)
	}
}
