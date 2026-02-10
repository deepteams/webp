# CLAUDE.md

## Project Overview

Pure Go WebP image encoder/decoder (`github.com/deepteams/webp`). No CGo dependencies. Supports VP8 (lossy), VP8L (lossless), VP8X (extended format with metadata), alpha channels, and animation.

## Tech Stack

- **Go 1.24.2** - No external dependencies
- Integrates with Go's `image` package via `image.RegisterFormat()`
- CLI tool: `cmd/gwebp`

## Commands

```bash
# Build
go build ./...

# Build CLI
go build -o gwebp ./cmd/gwebp

# Run all tests
go test ./...

# Run specific package tests
go test ./internal/lossy
go test ./internal/lossless
go test ./animation
go test ./mux

# Run tests with verbose output
go test -v ./...

# Run benchmarks
go test -bench=. ./...
```

## Project Structure

```
webp.go / encode.go       # Public API (Decode, Encode, Options)
doc.go                     # Package documentation
animation/                 # Animation support (ANIM/ANMF frames)
cmd/gwebp/                 # CLI tool (encode/decode/info)
mux/                       # WebP multiplex/demultiplex
sharpyuv/                  # Sharp YUV color space conversion
internal/
  bitio/                   # Bit-level I/O (boolean arithmetic, lossless streams)
  container/               # RIFF/WEBP container parsing
  dsp/                     # Digital Signal Processing (YUV, filters, prediction, cost)
  lossless/                # VP8L encoder/decoder
  lossy/                   # VP8 encoder/decoder
  pool/                    # Object pool utilities
testdata/                  # Test WebP files
libwebp/                   # Bundled reference C implementation (for reference only)
```

## Architecture

- **Layered**: Public API -> Container parsing -> Codec implementations -> DSP utilities
- **Options pattern**: `EncoderOptions` struct for encoder configuration
- **Frame-based processing**: `FrameDecoderFunc` / `FrameEncoderFunc` for animation
- **Metadata separation**: ICC, EXIF, XMP handled independently from pixel data

## Code Conventions

- Standard Go naming: exported types in PascalCase, internal in camelCase
- Explicit error returns with `fmt.Errorf()` context wrapping
- Tests alongside implementation (`*_test.go`), test data in `testdata/`
- Round-trip testing pattern: encode -> decode -> verify
- No external dependencies - everything is self-contained
- Internal packages (`internal/`) are not part of the public API

## Key Types

```go
// Decoding
func Decode(r io.Reader) (image.Image, error)
func DecodeConfig(r io.Reader) (image.Config, error)
func GetFeatures(r io.Reader) (*Features, error)

// Encoding
func Encode(w io.Writer, img image.Image, opts *EncoderOptions) error

// Options
type EncoderOptions struct {
    Lossless bool
    Quality  float32  // 0-100
    Method   int      // 0-6 (speed vs compression)
    // ...
}
```

## Important Notes

- `libwebp/` is the reference C implementation included for documentation purposes only - do not modify
- `internal/` packages contain the core codec logic (~30k+ lines) - changes here require careful testing
- Bitstream formats are binary-level precise - any bit-level error will corrupt output
- Always run `go test ./...` after modifying codec internals
