package richmail

import (
	"bytes"
	"errors"
	"fmt"
	"image"

	// Register the decoders image.Decode dispatches to. GIF decoding yields the
	// first frame, which is the documented behaviour for this first release.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp" // registers the webp decoder with image.Decode
)

// Limits bounds image decoding. Email is untrusted input, so every embedded or
// fetched image passes through these before any pixel buffer is allocated.
type Limits struct {
	// MaxBytes caps the encoded size in bytes.
	MaxBytes int
	// MaxPixels caps width*height after header inspection.
	MaxPixels int64
	// MaxDimension caps either side after header inspection.
	MaxDimension int
}

// DefaultLimits are generous enough for any real email image and small enough
// that a decompression bomb is refused before pixels are allocated.
func DefaultLimits() Limits {
	return Limits{
		MaxBytes:     12 << 20, // 12 MiB encoded
		MaxPixels:    40_000_000,
		MaxDimension: 12_000,
	}
}

// Decode errors. Callers distinguish "not an image" from "too big" so the UI can
// say something useful.
var (
	ErrImageTooLarge = errors.New("richmail: image exceeds size limit")
	ErrImageTooWide  = errors.New("richmail: image dimensions exceed limit")
	ErrNotImage      = errors.New("richmail: data is not a supported image")
	ErrImageDecode   = errors.New("richmail: image decode failed")
)

// Decoded is a decoded image plus the facts layout and diagnostics need.
type Decoded struct {
	Image  image.Image
	Format string
	Width  int
	Height int
}

// Decode validates and decodes encoded image bytes. It reads the header first so
// an oversized image is rejected without materializing its pixels.
func Decode(data []byte, limits Limits) (*Decoded, error) {
	if len(data) == 0 {
		return nil, ErrNotImage
	}
	if limits.MaxBytes > 0 && len(data) > limits.MaxBytes {
		return nil, ErrImageTooLarge
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, ErrNotImage
	}
	if limits.MaxDimension > 0 && (cfg.Width > limits.MaxDimension || cfg.Height > limits.MaxDimension) {
		return nil, ErrImageTooWide
	}
	if limits.MaxPixels > 0 && int64(cfg.Width)*int64(cfg.Height) > limits.MaxPixels {
		return nil, ErrImageTooWide
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrImageDecode, err)
	}
	b := img.Bounds()
	return &Decoded{
		Image:  img,
		Format: format,
		Width:  b.Dx(),
		Height: b.Dy(),
	}, nil
}
