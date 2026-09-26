package termimage

import (
	"image"

	xdraw "golang.org/x/image/draw"
)

// scaleForUpload downscales img to fit w x h using a high-quality resampler. It
// never enlarges: an image already smaller than the target is returned as-is,
// both to avoid pointless work and to keep small icons crisp. A non-positive
// target disables scaling.
func scaleForUpload(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	if w <= 0 || h <= 0 || (b.Dx() <= w && b.Dy() <= h) {
		return img
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}
