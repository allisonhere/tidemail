package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/imagepreview"
	tea "github.com/charmbracelet/bubbletea"
)

// Opt-in smoke test for the actual reader, scrolling, overlays, resize, and i.
func TestInteractiveInlineImages(t *testing.T) {
	if os.Getenv("TIDEMAIL_INTERACTIVE_INLINE_TEST") != "1" {
		t.Skip("requires interactive Kitty-compatible terminal")
	}
	m := inlineTestModel(t)
	oldSave := configSave
	t.Cleanup(func() { configSave = oldSave })
	configSave = func(config.Config) error { return nil }
	m.cfg.Display.ConfirmQuit = false
	m.focused = paneContent
	img := image.NewRGBA(image.Rect(0, 0, 600, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 600; x++ {
			c := color.RGBA{35, 90, 200, 255}
			if x >= 300 {
				c = color.RGBA{220, 90, 30, 255}
			}
			if (x-300)*(x-300)+(y-150)*(y-150) < 10000 {
				c = color.RGBA{240, 240, 240, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	decoded, err := imagepreview.Decode(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	m.inlineImages.assets[0].image = decoded
	m.inlineImages.assets[0].source.data = encoded.Bytes()
	m.filteredMessages[0].AttachmentData[0].Data = encoded.Bytes()
	m.filteredMessages[0].Subject = "Inline image rendering check"
	m.filteredMessages[0].From = "Example Sender <sender@example.com>"
	m.filteredMessages[0].Date = time.Now()
	m.filteredMessages[0].BodyHTML += strings.Repeat("<p>More text below the image for scrolling.</p>", 30)
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	output := imagepreview.NewInlineWriter(tty)
	defer output.Close()
	m.SetInlineImageOutput(output)
	m.setViewportMessage(m.filteredMessages[0])
	final, err := tea.NewProgram(inlineInteractiveModel{m}, tea.WithInput(tty), tea.WithOutput(output), tea.WithAltScreen()).Run()
	if err != nil {
		t.Fatal(err)
	}
	if result, ok := final.(Model); ok {
		result.CancelImageLoads()
	}
}

type inlineInteractiveModel struct{ Model }

func (inlineInteractiveModel) Init() tea.Cmd { return nil }
