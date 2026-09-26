package richmail

import "math"

// CellGeometry is the terminal's cell size in device pixels. Real values come
// from TIOCGWINSZ (Xpixel/Ypixel); when a terminal does not report them the
// conventional 8x16 is assumed, which keeps aspect ratios sensible.
type CellGeometry struct {
	CellWidthPx  float64
	CellHeightPx float64
}

// DefaultCellGeometry is used when the terminal does not report pixel metrics.
func DefaultCellGeometry() CellGeometry {
	return CellGeometry{CellWidthPx: 8, CellHeightPx: 16}
}

// normalized returns a geometry with sane, non-zero cell dimensions.
func (g CellGeometry) normalized() CellGeometry {
	if g.CellWidthPx <= 0 {
		g.CellWidthPx = 8
	}
	if g.CellHeightPx <= 0 {
		g.CellHeightPx = 16
	}
	return g
}

// Align is the horizontal placement of an image within the content column.
type Align int

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

// Intrinsic describes an image's decoded size and any HTML authoring hints.
// Hint values are CSS pixels as written by the sender; they can only shrink an
// image, never enlarge it past its decoded size.
type Intrinsic struct {
	PixelWidth  int
	PixelHeight int
	HintWidth   int
	HintHeight  int
	MaxWidth    int
	Align       Align
}

// Placement is the terminal-cell rectangle an image occupies within the content
// column, plus the pixel size the image should be scaled to before upload.
type Placement struct {
	Cols        int
	Rows        int
	OffsetCols  int
	PixelWidth  int
	PixelHeight int
}

// maxImageRows bounds how tall a single image may be. A pathological image with
// an extreme portrait aspect ratio would otherwise reserve thousands of rows and
// make the whole message unnavigable.
const maxImageRows = 512

// Layout fits an image into availCols columns while preserving its aspect ratio.
// It returns ok=false when there is nothing to lay out (no intrinsic size or no
// available width), in which case the caller shows the text placeholder.
//
// The rules mirror how a browser approximates an email image:
//   - never enlarge beyond the decoded pixel size
//   - honour an author width hint as a ceiling
//   - honour CSS max-width as a ceiling
//   - never exceed the available column width
//   - preserve aspect ratio exactly
func Layout(in Intrinsic, availCols int, geom CellGeometry) (Placement, bool) {
	if availCols <= 0 || in.PixelWidth <= 0 || in.PixelHeight <= 0 {
		return Placement{}, false
	}
	geom = geom.normalized()

	targetW := float64(in.PixelWidth)
	if in.HintWidth > 0 && float64(in.HintWidth) < targetW {
		targetW = float64(in.HintWidth)
	}
	if in.MaxWidth > 0 && float64(in.MaxWidth) < targetW {
		targetW = float64(in.MaxWidth)
	}
	availPx := float64(availCols) * geom.CellWidthPx
	if targetW > availPx {
		targetW = availPx
	}
	if targetW < 1 {
		targetW = 1
	}

	aspect := float64(in.PixelHeight) / float64(in.PixelWidth)
	targetH := targetW * aspect

	cols := int(math.Round(targetW / geom.CellWidthPx))
	rows := int(math.Round(targetH / geom.CellHeightPx))
	cols = clampInt(cols, 1, availCols)
	rows = clampInt(rows, 1, maxImageRows)

	// Recompute the pixel target from the integer cell rectangle so the image
	// fills its cells exactly and no half-cell scaling artifacts appear.
	pxW := int(math.Round(float64(cols) * geom.CellWidthPx))
	pxH := int(math.Round(float64(rows) * geom.CellHeightPx))

	offset := 0
	switch in.Align {
	case AlignCenter:
		offset = (availCols - cols) / 2
	case AlignRight:
		offset = availCols - cols
	}
	if offset < 0 {
		offset = 0
	}

	return Placement{
		Cols:        cols,
		Rows:        rows,
		OffsetCols:  offset,
		PixelWidth:  pxW,
		PixelHeight: pxH,
	}, true
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
