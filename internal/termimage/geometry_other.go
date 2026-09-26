//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package termimage

import "github.com/allisonhere/tidemail/internal/richmail"

// DetectGeometry is unavailable on non-Unix platforms; layout falls back to the
// conventional 8x16 cell size.
func DetectGeometry() richmail.CellGeometry { return richmail.CellGeometry{} }

// GeometryFromSize converts a known window pixel size and cell grid into cell
// metrics.
func GeometryFromSize(pxWidth, pxHeight, cols, rows int) richmail.CellGeometry {
	if pxWidth <= 0 || pxHeight <= 0 || cols <= 0 || rows <= 0 {
		return richmail.CellGeometry{}
	}
	return richmail.CellGeometry{
		CellWidthPx:  float64(pxWidth) / float64(cols),
		CellHeightPx: float64(pxHeight) / float64(rows),
	}
}
