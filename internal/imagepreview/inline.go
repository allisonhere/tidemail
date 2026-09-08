package imagepreview

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
)

const Placeholder = '\U0010EEEE'

// First 64 row/column values from the Kitty protocol's Unicode mapping:
// https://raw.githubusercontent.com/kovidgoyal/kitty/master/gen/rowcolumn-diacritics.txt
var coordinates = []rune{
	0x305, 0x30d, 0x30e, 0x310, 0x312, 0x33d, 0x33e, 0x33f, 0x346, 0x34a, 0x34b, 0x34c, 0x350, 0x351, 0x352, 0x357,
	0x35b, 0x363, 0x364, 0x365, 0x366, 0x367, 0x368, 0x369, 0x36a, 0x36b, 0x36c, 0x36d, 0x36e, 0x36f, 0x483, 0x484,
	0x485, 0x486, 0x487, 0x592, 0x593, 0x594, 0x595, 0x597, 0x598, 0x599, 0x59c, 0x59d, 0x59e, 0x59f, 0x5a0, 0x5a1,
	0x5a8, 0x5a9, 0x5ab, 0x5ac, 0x5af, 0x5c4, 0x610, 0x611, 0x612, 0x613, 0x614, 0x615, 0x616, 0x617, 0x657, 0x658,
}
var nextInlineID atomic.Uint32

func NewInlineID() uint32 { return 0x100000 + nextInlineID.Add(1)%0xE00000 }

type inlinePlacement struct {
	image      Image
	cols, rows int
}

// InlineWriter is the sole output path used by Bubble Tea. Graphics uploads are
// serialized with its repaint writes, never emitted from View or worker commands.
// Unicode placeholder cells let the normal viewport and overlays clip graphics.
type InlineWriter struct {
	mu      sync.Mutex
	out     io.Writer
	desired map[uint32]inlinePlacement
	sent    map[uint32]inlinePlacement
	active  bool
}

func NewInlineWriter(out io.Writer) *InlineWriter {
	return &InlineWriter{out: out, desired: map[uint32]inlinePlacement{}, sent: map[uint32]inlinePlacement{}}
}
func (w *InlineWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.desired = map[uint32]inlinePlacement{}
}
func (w *InlineWriter) Register(id uint32, img Image, cols, rows int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.desired[id] = inlinePlacement{img, cols, rows}
}
func (w *InlineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.out.Write(p)
	if err != nil {
		return n, err
	}
	if bytes.Contains(p, []byte("\x1b[?1049h")) {
		w.active = true
	}
	if bytes.Contains(p, []byte("\x1b[2J")) || bytes.Contains(p, []byte("\x1b[?1049h")) {
		w.sent = map[uint32]inlinePlacement{}
	}
	// Don't re-upload images while Bubble Tea releases the alternate screen for
	// a full-screen preview or exits. They'll be restored on the next repaint.
	if bytes.Contains(p, []byte("\x1b[?1049l")) {
		w.sent = map[uint32]inlinePlacement{}
		w.active = false
		return n, nil
	}
	if !w.active {
		return n, nil
	}
	changed := len(w.sent) != len(w.desired)
	for id, placement := range w.desired {
		previous, exists := w.sent[id]
		if !exists || previous.cols != placement.cols || previous.rows != placement.rows {
			changed = true
			break
		}
	}
	if !changed {
		return n, nil
	}
	// The renderer owns the cursor. Preserve it across graphics commands, even
	// on terminals that move it when a virtual placement is created.
	if _, err := io.WriteString(w.out, "\x1b7"); err != nil {
		return n, err
	}
	defer io.WriteString(w.out, "\x1b8")
	for id := range w.sent {
		if _, ok := w.desired[id]; !ok {
			if _, err := fmt.Fprintf(w.out, "\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", id); err != nil {
				return n, err
			}
			delete(w.sent, id)
		}
	}
	for id, placement := range w.desired {
		previous, exists := w.sent[id]
		if !exists {
			if err := transmitPNGWithID(w.out, placement.image.PNG, id); err != nil {
				return n, err
			}
		}
		if !exists || previous.cols != placement.cols || previous.rows != placement.rows {
			if _, err := fmt.Fprintf(w.out, "\x1b_Ga=p,U=1,i=%d,p=1,c=%d,r=%d,C=1,q=2;\x1b\\", id, placement.cols, placement.rows); err != nil {
				return n, err
			}
			w.sent[id] = placement
		}
	}
	return n, nil
}
func (w *InlineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var first error
	for id, placement := range w.sent {
		if _, ok := w.desired[id]; !ok {
			w.desired[id] = placement
		}
	}
	for id := range w.desired {
		if _, err := fmt.Fprintf(w.out, "\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", id); first == nil {
			first = err
		}
	}
	w.desired = map[uint32]inlinePlacement{}
	w.sent = map[uint32]inlinePlacement{}
	return first
}

// Grid explicitly addresses every cell, so partial scrolling and overlays cannot
// accidentally restart the image at column zero. Rows are capped at 32 to keep
// tall newsletters navigable; the virtual placement preserves the aspect ratio.
func Grid(id uint32, img Image, width int, cellAspect float64) (string, int, int) {
	if cellAspect <= 0 {
		cellAspect = 2
	}
	cols := max(1, min(width, len(coordinates)))
	rows := max(1, int(math.Ceil(float64(img.Height)/float64(img.Width)*float64(cols)/cellAspect)))
	if rows > 32 {
		rows = 32
		cols = max(1, min(cols, int(math.Ceil(float64(img.Width)/float64(img.Height)*float64(rows)*cellAspect))))
	}
	var b strings.Builder
	for row := 0; row < rows; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", (id>>16)&255, (id>>8)&255, id&255)
		for col := 0; col < cols; col++ {
			b.WriteRune(Placeholder)
			b.WriteRune(coordinates[row])
			b.WriteRune(coordinates[col])
		}
		b.WriteString("\x1b[39m")
	}
	return b.String(), cols, rows
}

// Preserve the terminal-file interface through the output wrapper: Bubble Tea
// uses it for the initial dimensions and subsequent SIGWINCH resize events.
func (w *InlineWriter) Fd() uintptr {
	if file, ok := w.out.(interface{ Fd() uintptr }); ok {
		return file.Fd()
	}
	return ^uintptr(0)
}
func (w *InlineWriter) Read(p []byte) (int, error) {
	if reader, ok := w.out.(io.Reader); ok {
		return reader.Read(p)
	}
	return 0, io.EOF
}
