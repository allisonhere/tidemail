package termimage

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/richmail"
)

func TestDetectProtocol(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Protocol
	}{
		{"force kitty", map[string]string{"TIDEMAIL_IMAGE_PROTOCOL": "kitty"}, ProtocolKitty},
		{"force off", map[string]string{"TIDEMAIL_IMAGE_PROTOCOL": "off", "KITTY_WINDOW_ID": "1"}, ProtocolNone},
		{"force zero", map[string]string{"TIDEMAIL_IMAGE_PROTOCOL": "0", "TERM": "xterm-kitty"}, ProtocolNone},
		{"unknown override falls back", map[string]string{"TIDEMAIL_IMAGE_PROTOCOL": "bogus", "TERM": "xterm-kitty"}, ProtocolKitty},
		{"kitty window id", map[string]string{"KITTY_WINDOW_ID": "7"}, ProtocolKitty},
		{"ghostty term", map[string]string{"TERM": "xterm-ghostty"}, ProtocolKitty},
		{"ghostty program", map[string]string{"TERM_PROGRAM": "ghostty"}, ProtocolKitty},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, ProtocolNone},
		{"wezterm executable", map[string]string{"WEZTERM_EXECUTABLE": "/usr/bin/wezterm"}, ProtocolNone},
		{"wezterm pane", map[string]string{"WEZTERM_PANE": "0"}, ProtocolNone},
		{"wezterm term", map[string]string{"TERM": "wezterm"}, ProtocolNone},
		{"wezterm inherited kitty", map[string]string{"TERM_PROGRAM": "WezTerm", "KITTY_WINDOW_ID": "7", "TERM": "xterm-kitty"}, ProtocolNone},
		{"wezterm explicit opt in", map[string]string{"TERM_PROGRAM": "WezTerm", "TIDEMAIL_IMAGE_PROTOCOL": "kitty"}, ProtocolKitty},
		{"foot", map[string]string{"TERM": "foot"}, ProtocolKitty},
		{"dumb", map[string]string{"TERM": "dumb"}, ProtocolNone},
		{"plain xterm", map[string]string{"TERM": "xterm-256color"}, ProtocolNone},
		{"empty env", map[string]string{}, ProtocolNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			if got := DetectProtocol(getenv); got != tt.want {
				t.Fatalf("DetectProtocol = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGeometryFromSize(t *testing.T) {
	g := GeometryFromSize(800, 480, 100, 30)
	if g.CellWidthPx != 8 || g.CellHeightPx != 16 {
		t.Fatalf("geometry = %+v", g)
	}
	if (GeometryFromSize(0, 480, 100, 30)) != (richmail.CellGeometry{}) {
		t.Error("expected zero geometry for invalid input")
	}
}

func TestNoopBackendProducesNoEscapes(t *testing.T) {
	var b Backend = NoopBackend{}
	if b.Capabilities().GraphicsEnabled() {
		t.Fatal("noop must not report graphics")
	}
	if data, ok := b.Transmit(1, image.NewRGBA(image.Rect(0, 0, 2, 2)), 2, 2, 16, 16); ok || len(data) != 0 {
		t.Fatal("noop must not transmit")
	}
	if rows := b.PlaceholderRows(1, 2, 2); rows != nil {
		t.Fatal("noop must not produce placeholder rows")
	}
	if data := b.Delete(1); data != nil {
		t.Fatal("noop must not delete")
	}
}

func TestKittyPlaceholderRows(t *testing.T) {
	b := NewKittyBackend(Capabilities{Protocol: ProtocolKitty})
	id := MakeID(42, 2) // 42 + (2 << 24)
	rows := b.PlaceholderRows(id, 3, 2)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	for i, row := range rows {
		if !strings.HasPrefix(row, "\x1b[38;5;42m") {
			t.Fatalf("row %d missing fg color: %q", i, row)
		}
		if !strings.HasSuffix(row, "\x1b[39m") {
			t.Fatalf("row %d missing reset: %q", i, row)
		}
		if got := strings.Count(row, string(placeholderRune)); got != 3 {
			t.Fatalf("row %d has %d placeholders, want 3", i, got)
		}
		if w := ansi.StringWidth(row); w != 3 {
			t.Fatalf("row %d width = %d, want 3", i, w)
		}
		// Each cell is placeholder + row + column + hi-byte diacritic.
		runes := []rune(row)
		for j, r := range runes {
			if r == placeholderRune {
				if j+3 >= len(runes) {
					t.Fatalf("row %d: placeholder without full diacritics", i)
				}
				if runes[j+3] != rowColumnDiacritics[2] {
					t.Fatalf("row %d: hi byte diacritic = %U, want %U", i, runes[j+3], rowColumnDiacritics[2])
				}
			}
		}
	}
	// Row 1 must encode row index 1.
	if !strings.ContainsRune(rows[1], rowColumnDiacritics[1]) {
		t.Error("second row does not encode row index 1")
	}
}

func TestKittyPlaceholderRejectsInvalid(t *testing.T) {
	b := NewKittyBackend(Capabilities{Protocol: ProtocolKitty})
	if rows := b.PlaceholderRows(MakeID(0, 0), 2, 2); rows != nil {
		t.Error("zero low byte must be rejected")
	}
	if rows := b.PlaceholderRows(MakeID(1, 2), MaxGridValue+2, 2); rows != nil {
		t.Error("oversized grid must be rejected")
	}
}

func TestKittyTransmitAndDelete(t *testing.T) {
	b := NewKittyBackend(Capabilities{Protocol: ProtocolKitty})
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	id := MakeID(7, 0)
	data, ok := b.Transmit(id, img, 2, 2, 16, 32)
	if !ok {
		t.Fatal("expected transmit to succeed")
	}
	if !bytes.Contains(data, []byte("\x1b_Ga=T,f=100,i=")) {
		t.Fatalf("missing control data: %q", data)
	}
	if !bytes.Contains(data, []byte("U=1,c=2,r=2,q=2")) {
		t.Fatalf("missing virtual placement keys: %q", data)
	}
	if !bytes.HasSuffix(data, []byte("\x1b\\")) {
		t.Fatal("escape not terminated")
	}

	// The payload must be valid base64 that decodes to a PNG.
	payload := extractPayload(t, data)
	if _, err := png.Decode(bytes.NewReader(payload)); err != nil {
		t.Fatalf("payload is not a PNG: %v", err)
	}

	del := b.Delete(id)
	if !bytes.Contains(del, []byte("a=d,d=i")) || !bytes.Contains(del, []byte("i=")) {
		t.Fatalf("bad delete escape: %q", del)
	}
}

// extractPayload concatenates the base64 chunks between the first ';' and the
// terminating ST of each escape.
func extractPayload(t *testing.T, data []byte) []byte {
	t.Helper()
	s := string(data)
	var b64 strings.Builder
	for {
		start := strings.Index(s, ";")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "\x1b\\")
		if end < 0 {
			t.Fatalf("unterminated escape: %q", s)
		}
		b64.WriteString(s[start+1 : start+end])
		s = s[start+end+2:]
	}
	decoded, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		t.Fatalf("payload not base64: %v", err)
	}
	return decoded
}

func TestKittyTransmitChunks(t *testing.T) {
	b := NewKittyBackend(Capabilities{Protocol: ProtocolKitty})
	// A noisy image compresses poorly, producing a payload large enough to need
	// more than one 4096-byte chunk.
	img := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	if _, err := rand.Read(img.Pix); err != nil {
		t.Fatal(err)
	}
	data, ok := b.Transmit(MakeID(1, 1), img, 40, 20, 400, 400)
	if !ok {
		t.Fatal("transmit failed")
	}
	if strings.Count(string(data), "\x1b_G") < 2 {
		t.Fatal("expected more than one chunk")
	}
	if !bytes.Contains(data, []byte(",m=1;")) {
		t.Fatal("expected a continuation chunk")
	}
	if _, err := png.Decode(bytes.NewReader(extractPayload(t, data))); err != nil {
		t.Fatalf("chunked payload is not a PNG: %v", err)
	}
}

func TestKittyTransmitRejectsBadID(t *testing.T) {
	b := NewKittyBackend(Capabilities{Protocol: ProtocolKitty})
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if _, ok := b.Transmit(0, img, 1, 1, 8, 8); ok {
		t.Error("zero id must be rejected")
	}
}

func TestAllocatorInRangeAndDistinct(t *testing.T) {
	a := NewIDAllocator()
	seen := map[uint32]bool{}
	for i := 0; i < 1000; i++ {
		id := a.Next()
		low, hi, ok := idParts(id)
		if !ok || low < 1 || low > 255 || hi < 0 || hi > MaxGridValue {
			t.Fatalf("id %d out of range: low=%d hi=%d ok=%v", id, low, hi, ok)
		}
		seen[id] = true
	}
	if len(seen) != 1000 {
		t.Fatalf("expected 1000 distinct ids, got %d", len(seen))
	}
}

func TestNewSelectsBackend(t *testing.T) {
	if _, ok := New(Capabilities{Protocol: ProtocolNone}).(NoopBackend); !ok {
		t.Error("none should select noop backend")
	}
	if _, ok := New(Capabilities{Protocol: ProtocolKitty}).(*KittyBackend); !ok {
		t.Error("kitty should select kitty backend")
	}
}

func TestScaleForUploadDownscalesOnly(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 50))
	scaled := scaleForUpload(src, 20, 10)
	if scaled.Bounds().Dx() != 20 || scaled.Bounds().Dy() != 10 {
		t.Fatalf("scaled bounds = %v", scaled.Bounds())
	}
	same := scaleForUpload(src, 200, 100)
	if same.Bounds() != src.Bounds() {
		t.Fatal("must not upscale")
	}
}
