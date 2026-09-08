package ui

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/imagepreview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type messageImage struct {
	label, source string
	data          []byte
	missing       bool
}
type imagePreviewState struct {
	items                  []messageImage
	cursor                 int
	generation             uint64
	messageID              int64
	loading, confirmRemote bool
	status                 string
	cancel                 context.CancelFunc
	cachedIndex            int
	cached                 imagepreview.Image
}
type imageLoadedMsg struct {
	generation uint64
	index      int
	image      imagepreview.Image
	err        error
}
type imagePreviewClosedMsg struct {
	generation uint64
	err        error
}

func safeImageLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}
func messageImages(html string, attachments []db.Attachment) []messageImage {
	var items []messageImage
	used := map[int]bool{}
	seen := map[string]bool{}
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(normalizeHTMLForRendering(html)))
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		source := strings.TrimSpace(attrFirst(s, "src"))
		if source == "" || seen[source] {
			return
		}
		seen[source] = true
		label := safeImageLabel(attrFirst(s, "alt", "title", "aria-label"))
		if label == "" {
			label = fmt.Sprintf("Image %d", len(items)+1)
		}
		item := messageImage{label: label, source: source}
		if strings.HasPrefix(strings.ToLower(source), "cid:") {
			cid, err := url.PathUnescape(source[4:])
			if err != nil {
				cid = source[4:]
			}
			cid = strings.Trim(cid, "<>")
			item.missing = true
			for i, a := range attachments {
				if a.ContentID != "" && strings.Trim(a.ContentID, "<>") == cid {
					item.data, item.missing = a.Data, false
					used[i] = true
					break
				}
			}
		} else {
			u, err := url.Parse(source)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				return
			}
		}
		items = append(items, item)
	})
	for i, a := range attachments {
		if !used[i] && strings.HasPrefix(strings.ToLower(a.ContentType), "image/") {
			items = append(items, messageImage{label: safeImageLabel(a.Filename), data: a.Data})
		}
	}
	return items
}
func (m *Model) resetImagePreview() {
	if m.imagePreview.cancel != nil {
		m.imagePreview.cancel()
	}
	generation := m.imagePreview.generation + 1
	m.imagePreview = imagePreviewState{generation: generation, cachedIndex: -1}
	if m.overlay == overlayImagePreview {
		m.overlay = overlayNone
	}
}
func (m Model) openImagePreview() (tea.Model, tea.Cmd) {
	if !m.cfg.Display.ImagePreviews {
		return m, nil
	}
	msg := m.currentContentMessage()
	if msg == nil {
		msg = m.currentRowMessage()
	}
	if msg == nil {
		return m, nil
	}
	m.resetImagePreview()
	m.imagePreview.messageID = msg.ID
	atts := msg.AttachmentData
	if m.db != nil {
		var err error
		atts, err = m.db.GetAttachments(msg.ID)
		if err != nil {
			m.imagePreview.status = "Could not read image attachments."
		}
	}
	m.imagePreview.items = messageImages(msg.BodyHTML, atts)
	m.overlay = overlayImagePreview
	return m, nil
}
func (m Model) handleImagePreviewKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.imagePreview
	switch {
	case keyMatches(key, m.keys.Cancel):
		if s.confirmRemote {
			s.confirmRemote = false
			return m, nil
		}
		m.resetImagePreview()
		return m, nil
	case keyMatches(key, m.keys.Up), keyMatches(key, m.keys.Down):
		if s.loading || s.confirmRemote {
			return m, nil
		}
		delta := 1
		if keyMatches(key, m.keys.Up) {
			delta = -1
		}
		s.cursor = clamp(s.cursor+delta, 0, max(0, len(s.items)-1))
		s.status = ""
	case keyMatches(key, m.keys.Confirm):
		if s.loading || len(s.items) == 0 {
			return m, nil
		}
		item := s.items[s.cursor]
		if item.missing {
			s.status = "Embedded image is unavailable in this cached message."
			return m, nil
		}
		if s.cachedIndex == s.cursor && s.cached.PNG != nil {
			return m, m.previewImageCmd(s.cached)
		}
		remote := strings.HasPrefix(item.source, "http://") || strings.HasPrefix(item.source, "https://")
		if remote && !s.confirmRemote {
			s.confirmRemote = true
			return m, nil
		}
		s.confirmRemote = false
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel, s.loading, s.status = cancel, true, "Loading image… Esc to cancel"
		generation, index := s.generation, s.cursor
		return m, func() tea.Msg {
			var image imagepreview.Image
			var err error
			if remote {
				image, err = imagepreview.Fetch(ctx, item.source)
			} else {
				image, err = imagepreview.Decode(item.data)
			}
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			return imageLoadedMsg{generation, index, image, err}
		}
	}
	return m, nil
}
func (m Model) previewImageCmd(image imagepreview.Image) tea.Cmd {
	generation := m.imagePreview.generation
	return tea.Exec(&imagepreview.Terminal{Image: image}, func(err error) tea.Msg { return imagePreviewClosedMsg{generation, err} })
}
func (m Model) handleImageLoaded(msg imageLoadedMsg) (tea.Model, tea.Cmd) {
	s := &m.imagePreview
	if msg.generation != s.generation || m.overlay != overlayImagePreview || !m.cfg.Display.ImagePreviews {
		return m, nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.loading = false
	if msg.err != nil {
		s.status = safeImageLabel(msg.err.Error())
		return m, nil
	}
	s.cached, s.cachedIndex, s.status = msg.image, msg.index, ""
	return m, m.previewImageCmd(msg.image)
}
func (m Model) renderImagePicker() string {
	width := max(1, min(m.width-6, 70))
	chrome := newManagerChrome(width, m.styles.Theme, m.styles.PlainUI)
	s := m.imagePreview
	var lines []string
	if len(s.items) == 0 {
		lines = append(lines, "No images in this message.")
	}
	visible := max(1, min(12, m.height-10))
	start := max(0, s.cursor-visible+1)
	for i := start; i < min(len(s.items), start+visible); i++ {
		item := s.items[i]
		prefix := "  "
		if i == s.cursor {
			prefix = "> "
		}
		suffix := ""
		if strings.HasPrefix(item.source, "http") {
			suffix = " (remote)"
		}
		lines = append(lines, truncate(prefix+item.label+suffix, max(1, width-4)))
	}
	if s.confirmRemote && len(s.items) > 0 {
		u, _ := url.Parse(s.items[s.cursor].source)
		lines = append(lines, "", "Load image from "+safeImageLabel(u.Host)+"?", "Enter: Load image   Esc: cancel")
	} else {
		lines = append(lines, "", "↑/↓ select  Enter preview  Esc close")
	}
	if s.status != "" {
		lines = append(lines, "", s.status)
	}
	body := lipgloss.NewStyle().Foreground(chrome.text).Background(chrome.baseBg).Width(width).Padding(1, 2).Render(strings.Join(lines, "\n"))
	return renderSoftPanelBox(body, width, "tidemail", "image previews", chrome)
}
