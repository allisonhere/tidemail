package imagepreview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeScreen struct {
	bytes.Buffer
	inputs               []string
	sizes                []screenSize
	readCount, sizeCount int
	failRead             bool
}

func (s *fakeScreen) read(time.Duration) (string, error) {
	if s.failRead {
		return "", io.EOF
	}
	if s.readCount >= len(s.inputs) {
		return "", io.EOF
	}
	input := s.inputs[s.readCount]
	s.readCount++
	return input, nil
}
func (s *fakeScreen) size() (screenSize, error) {
	i := min(s.sizeCount, len(s.sizes)-1)
	s.sizeCount++
	return s.sizes[i], nil
}
func TestTerminalProbeResizeAndCleanup(t *testing.T) {
	s := &fakeScreen{inputs: []string{fmt.Sprintf("\x1b_Gi=%d;", imageID), "OK\x1b\\", "", "\x1b"}, sizes: []screenSize{{80, 24, 800, 480}, {40, 12, 400, 240}}}
	err := runPreview(s, Image{PNG: samplePNG(t), Width: 12, Height: 8}, make(chan os.Signal))
	if err != nil {
		t.Fatal(err)
	}
	out := s.String()
	if strings.Contains(out[strings.Index(out, "a=t,"):], "\x1b[2J") {
		t.Fatal("clear screen discards transmitted image data")
	}
	if strings.Count(out, "a=p,") != 2 {
		t.Fatalf("expected placement then resize: %q", out)
	}
	if !strings.Contains(out, fmt.Sprintf("d=I,i=%d", imageID)) || !strings.HasSuffix(out, "\x1b[?25h\x1b[?1049l") {
		t.Fatal("missing cleanup")
	}
}
func TestTerminalFailureStillRestoresScreen(t *testing.T) {
	s := &fakeScreen{failRead: true}
	if err := runPreview(s, Image{}, make(chan os.Signal)); err == nil {
		t.Fatal("expected read failure")
	}
	if !strings.HasSuffix(s.String(), "\x1b[?25h\x1b[?1049l") {
		t.Fatal("failed to restore screen")
	}
	s = &fakeScreen{inputs: []string{strings.Repeat("x", 4097)}}
	err := runPreview(s, Image{}, make(chan os.Signal))
	if err == nil || !strings.Contains(err.Error(), "does not support") || strings.Contains(s.String(), "a=t,") {
		t.Fatal("unsupported terminal transmitted image", err)
	}
}
func TestTerminalSignalCleansUp(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- os.Interrupt
	s := &fakeScreen{}
	if err := runPreview(s, Image{}, signals); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(s.String(), "\x1b[?25h\x1b[?1049l") {
		t.Fatal("signal left terminal dirty")
	}
}
func TestPNGChunking(t *testing.T) {
	data := bytes.Repeat([]byte{1, 2, 3}, 4096)
	var out bytes.Buffer
	if err := transmitPNG(&out, data); err != nil {
		t.Fatal(err)
	}
	var encoded strings.Builder
	chunks := strings.Split(out.String(), "\x1b_G")[1:]
	for i, chunk := range chunks {
		metadata, payload, _ := strings.Cut(strings.TrimSuffix(chunk, "\x1b\\"), ";")
		if len(payload) > 4096 || (i < len(chunks)-1 && len(payload)%4 != 0) {
			t.Fatal("invalid chunk size")
		}
		if i == len(chunks)-1 && !strings.Contains(metadata, "m=0") {
			t.Fatal("missing last chunk")
		}
		encoded.WriteString(payload)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatal("corrupt transmission")
	}
}
func TestImageFitsTerminal(t *testing.T) {
	for _, size := range []screenSize{{80, 24, 800, 480}, {20, 8, 0, 0}, {4, 4, 40, 80}} {
		for _, img := range []Image{{Width: 2000, Height: 100}, {Width: 100, Height: 2000}, {Width: 300, Height: 200}} {
			cols, rows := fitImage(img, size)
			if cols < 1 || rows < 1 || cols > size.cols-2 || rows > size.rows-3 {
				t.Fatalf("bad size %dx%d for %+v", cols, rows, size)
			}
		}
	}
	// 400x200 pixels at 10x20 pixels/cell must occupy twice as many
	// columns as pixel width/height alone would suggest.
	cols, rows := fitImage(Image{Width: 400, Height: 200}, screenSize{82, 23, 820, 460})
	if cols != 80 || rows != 20 {
		t.Fatalf("aspect ratio: %dx%d", cols, rows)
	}
}
