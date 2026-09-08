package imagepreview

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Run in a real graphics-capable terminal with TIDEMAIL_INTERACTIVE_IMAGE_TEST=1.
// This exercises Bubble Tea's release/restore path, beyond protocol unit tests.
func TestInteractivePreview(t *testing.T) {
	if os.Getenv("TIDEMAIL_INTERACTIVE_IMAGE_TEST") != "1" {
		t.Skip("requires an interactive graphics terminal")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 600, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 600; x++ {
			c := color.RGBA{30, 80, 200, 255}
			if x >= 300 {
				c = color.RGBA{220, 90, 30, 255}
			}
			if (x-300)*(x-300)+(y-150)*(y-150) < 100*100 {
				c = color.RGBA{240, 240, 240, 255}
			}
			canvas.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, canvas); err != nil {
		t.Fatal(err)
	}
	img, err := Decode(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	model, err := tea.NewProgram(interactivePreviewModel{img: img}, tea.WithInput(tty), tea.WithOutput(tty), tea.WithAltScreen()).Run()
	if err != nil {
		t.Fatal(err)
	}
	if err := model.(interactivePreviewModel).err; err != nil {
		t.Fatal(err)
	}
}

type interactivePreviewModel struct {
	img Image
	err error
}
type interactivePreviewDone struct{ err error }

func (m interactivePreviewModel) Init() tea.Cmd {
	return tea.Exec(&Terminal{Image: m.img}, func(err error) tea.Msg { return interactivePreviewDone{err} })
}
func (m interactivePreviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if done, ok := msg.(interactivePreviewDone); ok {
		m.err = done.err
		return m, tea.Quit
	}
	return m, nil
}
func (interactivePreviewModel) View() string { return "Image preview integration test" }
