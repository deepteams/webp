// Package webp provides a pure Go encoder and decoder for the WebP image format.
//
// WebP is a modern image format developed by Google that provides superior
// lossless and lossy compression for images on the web. This package implements
// the full WebP specification without any CGo dependencies, making it fully
// portable and easy to cross-compile.
//
// The package supports:
//   - Lossy decoding (VP8)
//   - Lossless decoding (VP8L)
//   - Alpha channel
//   - Lossy encoding (VP8)
//   - Lossless encoding (VP8L)
//   - Extended format (VP8X) with ICC, EXIF, XMP metadata
//   - Animation (ANIM/ANMF)
//
// Basic usage for decoding:
//
//	img, err := webp.Decode(reader)
//
// Basic usage for encoding:
//
//	err := webp.Encode(writer, img, &webp.Options{Quality: 80})
package webp
