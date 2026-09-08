package ui

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/imagepreview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func inlineTestModel(t *testing.T) Model {
	t.Helper()
	m := imageTestModel()
	m.resetImagePreview()
	m.width = 100
	m.height = 40
	var raw bytes.Buffer
	if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 40, 20))); err != nil {
		t.Fatal(err)
	}
	m.filteredMessages = []db.Message{{ID: 1, BodyHTML: `<p>Before picture</p><img src="cid:chart" alt="Chart"><p>After picture</p>`, AttachmentData: []db.Attachment{{ContentID: "chart", ContentType: "image/png", Data: raw.Bytes()}}}}
	m.inlineImages = inlineImageState{output: imagepreview.NewInlineWriter(io.Discard), checked: true, supported: true, cellAspect: 2}
	m.viewport.Width = m.contentBodyWidth()
	m.viewport.Height = m.contentBodyHeight()
	m.setViewportMessage(m.filteredMessages[0])
	cmd := m.syncInlineImages()
	if cmd == nil {
		t.Fatal("didn't schedule local image")
	}
	next, _ := m.handleInlineLoaded(cmd().(inlineLoadedMsg))
	m = next.(Model)
	return m
}
func TestInlineImagesRemainBetweenParagraphs(t *testing.T) {
	m := inlineTestModel(t)
	for _, width := range []int{24, 80} {
		body := ansi.Strip(m.renderMessageBody(m.filteredMessages[0], width))
		before, picture, after := strings.Index(body, "Before picture"), strings.IndexRune(body, imagepreview.Placeholder), strings.Index(body, "After picture")
		if before < 0 || picture < before || after < picture {
			t.Fatalf("image order: %q", body)
		}
		for _, line := range strings.Split(body, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("overflow %q", line)
			}
		}
	}
	m.cfg.Display.ImagePreviews = false
	m.syncInlineImages()
	if strings.ContainsRune(m.viewport.View(), imagepreview.Placeholder) {
		t.Fatal("toggle retained inline image")
	}
}
func TestInlineTablePreservesAdjacentContent(t *testing.T) {
	m := inlineTestModel(t)
	msg := m.filteredMessages[0]
	msg.BodyHTML = `<table><tr><th>Details</th><th>Chart</th></tr><tr><td>Keep this text</td><td><img src="cid:chart"></td></tr></table>`
	for _, width := range []int{12, 24, 80} {
		body := ansi.Strip(m.renderMessageBody(msg, width))
		if !strings.Contains(strings.Join(strings.Fields(body), " "), "Keep this text") || !strings.ContainsRune(body, imagepreview.Placeholder) || strings.Contains(body, "TMI") {
			t.Fatalf("lost table content at width %d: %q", width, body)
		}
	}
}
func TestInlineHighlightPreservesImageIDColors(t *testing.T) {
	m := inlineTestModel(t)
	m.focused = paneContent
	m.cfg.Display.FocusLine = true
	for i, line := range m.contentLines {
		if strings.ContainsRune(line, imagepreview.Placeholder) {
			m.contentFocusLine = i
			m.viewport.SetYOffset(i)
			break
		}
	}
	body := m.viewport.View()
	got := m.renderContentFocusLine(body, 80, 20, true)
	if !strings.Contains(got, "\x1b[38;2;") || !strings.ContainsRune(got, imagepreview.Placeholder) {
		t.Fatalf("focus highlight destroyed image identity: %q", got)
	}
}
func TestInlineRemoteConsentAndStaleResults(t *testing.T) {
	m := imageTestModel()
	m.resetImagePreview()
	m.inlineImages = inlineImageState{output: imagepreview.NewInlineWriter(io.Discard), checked: true, supported: true}
	if cmd := m.syncInlineImages(); cmd != nil {
		t.Fatal("opening message downloaded remote image")
	}
	m.inlineImages.requestedMessageID = 1
	cmd := m.syncInlineImages()
	if cmd == nil {
		t.Fatal("explicit image request did not load")
	}
	generation := m.inlineImages.generation
	ctx, cancel := context.WithCancel(context.Background())
	m.inlineImages.cancel()
	m.inlineImages.cancel = cancel
	m.cfg.Display.ImagePreviews = false
	m.syncInlineImages()
	if ctx.Err() == nil {
		t.Fatal("toggle did not cancel")
	}
	next, _ := m.handleInlineLoaded(inlineLoadedMsg{generation: generation, index: 0, image: imagepreview.Image{PNG: []byte("late")}})
	if len(next.(Model).inlineImages.assets) != 0 {
		t.Fatal("stale result restored images")
	}
}
func TestInlineToggleRequestsOnlyCurrentMessage(t *testing.T) {
	original := configSave
	t.Cleanup(func() { configSave = original })
	configSave = func(c config.Config) error { return nil }
	m := imageTestModel()
	m.resetImagePreview()
	m.cfg.Display.ImagePreviews = false
	m.focused = paneContent
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = next.(Model)
	if m.inlineImages.requestedMessageID != 1 {
		t.Fatal("i did not request current images")
	}
}

func TestInlineImageRowsScrollAndDoNotCopyMarkers(t *testing.T) {
	m := inlineTestModel(t)
	found := false
	for i, line := range m.contentLines {
		if strings.ContainsRune(line, imagepreview.Placeholder) {
			if !m.contentFocusable[i] {
				t.Fatal("keyboard navigation would skip image rows")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing image rows")
	}
	m.contentSelectionActive = true
	m.contentSelectionAll = true
	if strings.ContainsRune(m.contentSelectionText(false), imagepreview.Placeholder) {
		t.Fatal("copied terminal graphics markers")
	}
}
func TestInlineImageInsideCollapsedQuoteIsHidden(t *testing.T) {
	m := inlineTestModel(t)
	msg := m.filteredMessages[0]
	msg.BodyHTML = `<p>New reply</p><blockquote><p>Old reply</p><img src="cid:chart"></blockquote>`
	body := m.renderMessageBody(msg, 60)
	if !strings.ContainsRune(body, imagepreview.Placeholder) {
		t.Fatal("missing quoted image")
	}
	if strings.ContainsRune(collapseQuoteBlocks(body, true), imagepreview.Placeholder) {
		t.Fatal("collapsed quote still displays its image")
	}
}
