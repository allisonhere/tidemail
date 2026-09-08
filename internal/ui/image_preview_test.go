package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/imagepreview"
	tea "github.com/charmbracelet/bubbletea"
)

func TestImageReferencesPreserveOrderAndCID(t *testing.T) {
	html := `<img width="1" height="1" src="https://example.com/pixel"><img src="cid:chart%40mail" alt="Chart"><img src="https://example.com/photo" alt="Photo"><img src="https://example.com/photo"><img src="cid:missing"><img src="file:///tmp/secret">`
	atts := []db.Attachment{{ContentID: "chart@mail", Filename: "chart.png", ContentType: "image/png", Data: []byte("chart")}, {Filename: "photo.jpg", ContentType: "image/jpeg", Data: []byte("photo")}, {Filename: "notes", ContentType: "text/plain"}}
	got := messageImages(html, atts)
	if len(got) != 4 || got[0].label != "Chart" || string(got[0].data) != "chart" || got[1].label != "Photo" || !got[2].missing || got[3].label != "photo.jpg" {
		t.Fatalf("references: %+v", got)
	}
	if got := safeImageLabel("Photo\x1b]52;secret\x07\n"); strings.ContainsAny(got, "\x1b\x07\n") {
		t.Fatalf("unsafe label: %q", got)
	}
}
func TestImageSettingDefaultsAndRoundTrip(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Display.ImagePreviews {
		t.Fatal("images enabled by default")
	}
	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssDisplay)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfImagePreviews)
	s, _, _ = s.Update(tea.KeyMsg{Type: tea.KeySpace}, DefaultKeys)
	cfg = s.ApplyTo(cfg)
	if !cfg.Display.ImagePreviews || !newSettings(cfg, settingsUpdateState{}).imagePreviews {
		t.Fatal("setting not retained")
	}
}
func imageTestModel() Model {
	cfg := config.DefaultConfig()
	cfg.Display.ImagePreviews = true
	m := NewModel(nil, cfg, "dev", false)
	m.filteredMessages = []db.Message{{ID: 1, BodyHTML: `<img src="https://example.com/photo.png" alt="Photo">`}}
	m.contentMessageID = 1
	model, _ := m.openImagePreview()
	return model.(Model)
}
func TestImagePreviewRequiresExplicitRemoteLoad(t *testing.T) {
	m := imageTestModel()
	if m.imagePreview.loading || m.overlay != overlayImagePreview {
		t.Fatal("opening picker started load")
	}
	model, cmd := m.handleImagePreviewKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	if cmd != nil || !m.imagePreview.confirmRemote {
		t.Fatal("remote selected without confirmation")
	}
	model, cmd = m.handleImagePreviewKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	if cmd == nil || !m.imagePreview.loading {
		t.Fatal("explicit load did not schedule download")
	}
	// Do not run the network command: cancellation must invalidate its result.
	generation := m.imagePreview.generation
	model, _ = m.handleImagePreviewKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(Model)
	_, cmd = m.handleImageLoaded(imageLoadedMsg{generation: generation, image: imagepreview.Image{PNG: []byte("late")}})
	if cmd != nil || m.overlay == overlayImagePreview {
		t.Fatal("late result reopened preview")
	}
}
func TestImagePreviewDisabledAndMessageSwitch(t *testing.T) {
	m := imageTestModel()
	m.cfg.Display.ImagePreviews = false
	model, cmd := m.openImagePreview()
	if cmd != nil || model.(Model).imagePreview.loading {
		t.Fatal("disabled image preview loaded")
	}
	m.cfg.Display.ImagePreviews = true
	ctx, cancel := context.WithCancel(context.Background())
	m.imagePreview.cancel = cancel
	generation := m.imagePreview.generation
	m.setViewportMessage(db.Message{ID: 2, BodyText: "different message"})
	if ctx.Err() == nil || m.imagePreview.generation == generation || m.overlay == overlayImagePreview {
		t.Fatal("message change retained preview")
	}
}
func TestImagePreviewMissingCIDAndCache(t *testing.T) {
	m := imageTestModel()
	m.imagePreview.items = []messageImage{{missing: true, label: "Missing"}}
	model, cmd := m.handleImagePreviewKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	if cmd != nil || !strings.Contains(m.imagePreview.status, "unavailable") {
		t.Fatal("missing CID not explained")
	}
	m.imagePreview.items = []messageImage{{source: "https://example.com/photo.png"}}
	m.imagePreview.cachedIndex = 0
	m.imagePreview.cached = imagepreview.Image{PNG: []byte("cached")}
	model, cmd = m.handleImagePreviewKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || model.(Model).imagePreview.confirmRemote {
		t.Fatal("cached image requested download again")
	}
}
