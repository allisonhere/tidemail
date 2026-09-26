//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package termimage

import (
	"os"

	"golang.org/x/sys/unix"

	"github.com/allisonhere/tidemail/internal/richmail"
)

// DetectGeometry reads the terminal's window size in pixels and in cells and
// derives the cell size. Many terminals report zero pixel dimensions; in that
// case a zero CellGeometry is returned and richmail falls back to 8x16.
func DetectGeometry() richmail.CellGeometry {
	for _, fd := range []int{int(os.Stdout.Fd()), int(os.Stderr.Fd()), int(os.Stdin.Fd())} {
		ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
		if err != nil || ws == nil {
			continue
		}
		if ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
			continue
		}
		return richmail.CellGeometry{
			CellWidthPx:  float64(ws.Xpixel) / float64(ws.Col),
			CellHeightPx: float64(ws.Ypixel) / float64(ws.Row),
		}
	}
	return richmail.CellGeometry{}
}

// GeometryFromSize converts a known window pixel size and cell grid into cell
// metrics. Exposed for tests and for callers that already have the values.
func GeometryFromSize(pxWidth, pxHeight, cols, rows int) richmail.CellGeometry {
	if pxWidth <= 0 || pxHeight <= 0 || cols <= 0 || rows <= 0 {
		return richmail.CellGeometry{}
	}
	return richmail.CellGeometry{
		CellWidthPx:  float64(pxWidth) / float64(cols),
		CellHeightPx: float64(pxHeight) / float64(rows),
	}
}
