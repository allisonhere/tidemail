// Package imagepreview decodes bounded email images and previews them in a terminal.
package imagepreview

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"time"
)

const MaxBytes = 10 << 20
const MaxPixels = 20_000_000

type Image struct {
	PNG           []byte
	Width, Height int
}

func Decode(data []byte) (Image, error) {
	if len(data) > MaxBytes {
		return Image{}, fmt.Errorf("image exceeds 10 MiB")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("cannot decode image; supported formats are PNG, JPEG and GIF")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Image{}, fmt.Errorf("image exceeds 20 million pixels")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("image is damaged: %w", err)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, decoded); err != nil {
		return Image{}, err
	}
	return Image{PNG: out.Bytes(), Width: cfg.Width, Height: cfg.Height}, nil
}

// Fetch never runs implicitly during message rendering. No cookies, credentials,
// or referrer are inherited from the user's browser or mail account.
func Fetch(ctx context.Context, source string) (Image, error) {
	u, err := url.Parse(source)
	if err != nil || !remoteURL(u) {
		return Image{}, fmt.Errorf("only HTTP(S) image URLs are supported")
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many image redirects")
		}
		req.Header.Del("Referer")
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		if !remoteURL(req.URL) {
			return fmt.Errorf("unsupported image redirect")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return Image{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Image{}, fmt.Errorf("image download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("image server returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > MaxBytes {
		return Image{}, fmt.Errorf("image exceeds 10 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return Image{}, fmt.Errorf("image download failed")
	}
	return Decode(data)
}

func remoteURL(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}
