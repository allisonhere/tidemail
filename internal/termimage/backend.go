// Package termimage renders raster images into a terminal using the Kitty
// graphics protocol's Unicode-placeholder mechanism, with a graceful fallback
// for terminals that have no graphics support.
//
// Unicode placeholders are chosen over absolute cursor-positioned placements
// for one decisive reason: the placeholder is ordinary text. Bubble Tea's
// viewport already moves text through the screen, clips panes, and repaints on
// resize — so an image anchored to placeholder cells scrolls, clips, and
// reflows with the document for free, without fighting the renderer over cursor
// position or screen ownership.
package termimage

import (
	"image"

	"github.com/allisonhere/tidemail/internal/richmail"
)

// Protocol identifies the terminal graphics mechanism in use.
type Protocol int

const (
	// ProtocolNone means text placeholders only.
	ProtocolNone Protocol = iota
	// ProtocolKitty requires Kitty graphics with Unicode placeholders and
	// virtual placements, rather than basic image transmission alone.
	ProtocolKitty
)

func (p Protocol) String() string {
	switch p {
	case ProtocolKitty:
		return "kitty"
	default:
		return "none"
	}
}

// Capabilities describes what the terminal can do.
type Capabilities struct {
	Protocol  Protocol
	TrueColor bool
	Cell      richmail.CellGeometry
}

// GraphicsEnabled reports whether real raster images can be placed inline.
func (c Capabilities) GraphicsEnabled() bool { return c.Protocol != ProtocolNone }

// Backend renders images for a particular terminal. Implementations must be
// safe for concurrent use; the UI builds placeholder rows on one goroutine and
// uploads bytes on another.
type Backend interface {
	Capabilities() Capabilities
	// Transmit returns escape bytes that upload img and create a virtual
	// placement of cols x rows cells. The image is scaled to pxWidth x
	// pxHeight first. ok is false when the backend cannot encode the request.
	Transmit(id uint32, img image.Image, cols, rows, pxWidth, pxHeight int) ([]byte, bool)
	// Delete returns escape bytes that delete a transmitted image id.
	Delete(id uint32) []byte
	// PlaceholderRows returns one string per cell row, each already carrying
	// its own foreground color and reset so it can be embedded directly in the
	// viewport content. The visible width of every row is exactly cols.
	PlaceholderRows(id uint32, cols, rows int) []string
}

// NoopBackend is the fallback: it never emits escape sequences and never
// produces placeholder rows, so the caller shows text placeholders instead.
type NoopBackend struct{}

func (NoopBackend) Capabilities() Capabilities { return Capabilities{Protocol: ProtocolNone} }
func (NoopBackend) Transmit(uint32, image.Image, int, int, int, int) ([]byte, bool) {
	return nil, false
}
func (NoopBackend) Delete(uint32) []byte                      { return nil }
func (NoopBackend) PlaceholderRows(uint32, int, int) []string { return nil }

// New returns the best backend for the detected capabilities.
func New(caps Capabilities) Backend {
	if caps.Protocol == ProtocolKitty {
		return NewKittyBackend(caps)
	}
	return NoopBackend{}
}
