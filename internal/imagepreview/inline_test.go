package imagepreview

import (
	"bytes"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"io"
	"strings"
	"testing"
)

func TestInlineGridCoordinatesAndWidth(t *testing.T) {
	id := uint32(0x123456)
	grid, cols, rows := Grid(id, Image{Width: 400, Height: 200}, 40, 2)
	if cols != 40 || rows != 10 {
		t.Fatalf("dimensions %dx%d", cols, rows)
	}
	for row, line := range strings.Split(grid, "\n") {
		if ansi.StringWidth(line) != cols {
			t.Fatalf("width %d", ansi.StringWidth(line))
		}
		if !strings.HasPrefix(line, "\x1b[38;2;18;52;86m") {
			t.Fatal("missing image ID color")
		}
		runes := []rune(ansi.Strip(line))
		for col := 0; col < cols; col++ {
			if runes[col*3] != Placeholder || runes[col*3+1] != coordinates[row] || runes[col*3+2] != coordinates[col] {
				t.Fatal("incorrect cell coordinates")
			}
		}
	}
	_, cols, rows = Grid(id, Image{Width: 10, Height: 10000}, 80, 2)
	if cols < 1 || rows != 32 {
		t.Fatal("tall image not bounded")
	}
}
func TestInlineWriterOrderingCachingAndCleanup(t *testing.T) {
	var output bytes.Buffer
	writer := NewInlineWriter(&output)
	img := Image{PNG: samplePNG(t), Width: 12, Height: 8}
	writer.Register(42, img, 12, 4)
	writer.Write([]byte("\x1b[?1049h\x1b[2Jframe"))
	rendered := output.String()
	if !strings.Contains(rendered, "C=1") || !strings.Contains(rendered, "\x1b7") || !strings.HasSuffix(rendered, "\x1b8") {
		t.Fatal("graphics disturbed renderer cursor")
	}
	if strings.Index(rendered, "a=t") < strings.Index(rendered, "frame") || !strings.Contains(rendered, "U=1,i=42") {
		t.Fatalf("bad upload ordering: %q", rendered)
	}
	output.Reset()
	writer.Write([]byte("scroll"))
	if output.String() != "scroll" {
		t.Fatal("scroll retransmitted pixels")
	}
	writer.Register(42, img, 6, 2)
	output.Reset()
	writer.Write([]byte("resize"))
	if strings.Contains(output.String(), "a=t") || !strings.Contains(output.String(), "c=6,r=2") {
		t.Fatal("resize should update virtual placement only")
	}
	output.Reset()
	writer.Write([]byte("\x1b[2Jrepaint"))
	if !strings.Contains(output.String(), "a=t") {
		t.Fatal("clear did not restore pixels")
	}
	output.Reset()
	writer.Write([]byte("\x1b[?1049l"))
	writer.Write([]byte("shell"))
	if strings.Contains(output.String(), "a=t") {
		t.Fatal("uploaded outside alternate screen")
	}
	writer.Write([]byte("\x1b[?1049h"))
	output.Reset()
	writer.Reset()
	writer.Write([]byte("hidden"))
	if !strings.Contains(output.String(), "d=I,i=42") {
		t.Fatal("toggle left image data behind")
	}
	writer.Register(43, img, 4, 2)
	writer.Write([]byte("new image"))
	output.Reset()
	writer.Reset()
	writer.Close()
	if !strings.Contains(output.String(), fmt.Sprintf("d=I,i=%d", 43)) {
		t.Fatal("close missed retired image")
	}
}

type descriptorWriter struct{ bytes.Buffer }

func (*descriptorWriter) Fd() uintptr { return 123 }
func TestInlineWriterPreservesTerminalDescriptor(t *testing.T) {
	writer := NewInlineWriter(&descriptorWriter{})
	if writer.Fd() != 123 {
		t.Fatal("wrapper hid the terminal descriptor")
	}
	var _ interface {
		io.ReadWriteCloser
		Fd() uintptr
	} = writer
}
