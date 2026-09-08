package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/imagepreview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type inlineAsset struct {
	source messageImage
	id     uint32
	image  imagepreview.Image
	done   bool
	err    string
}
type inlineImageState struct {
	output                       *imagepreview.InlineWriter
	checked, checking, supported bool
	cellAspect                   float64
	messageID                    int64
	sourceHTML                   string
	sourceHasAttachment          bool
	requestedMessageID           int64
	generation                   uint64
	assets                       []inlineAsset
	loading                      bool
	cancel                       context.CancelFunc
	pixels                       int64
}
type inlineProbeMsg struct {
	supported bool
	aspect    float64
}
type inlineLoadedMsg struct {
	generation uint64
	index      int
	image      imagepreview.Image
	err        error
}

func (m *Model) SetInlineImageOutput(output *imagepreview.InlineWriter) {
	m.inlineImages.output = output
}
func (m *Model) clearInlineImages() {
	s := &m.inlineImages
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.output != nil {
		s.output.Reset()
	}
	s.messageID = 0
	s.generation++
	s.assets = nil
	s.loading = false
	s.pixels = 0
}
func (m *Model) refreshInlineContent() {
	offset := m.viewport.YOffset
	m.setViewportForCurrentRow()
	m.viewport.SetYOffset(offset)
}

// Called after each model update so every message-navigation path shares the
// same cancellation/loading rules. View never starts network or decoding work.
func (m *Model) syncInlineImages() tea.Cmd {
	s := &m.inlineImages
	if s.output == nil {
		return nil
	}
	if !m.cfg.Display.ImagePreviews {
		if s.messageID != 0 {
			m.clearInlineImages()
			m.refreshInlineContent()
		}
		return nil
	}
	if !s.checked {
		if s.checking {
			return nil
		}
		s.checking = true
		probe := &imagepreview.Probe{}
		return tea.Exec(probe, func(err error) tea.Msg { return inlineProbeMsg{err == nil && probe.Supported, probe.CellAspect} })
	}
	if !s.supported {
		return nil
	}
	msg := m.currentContentMessage()
	if msg == nil {
		if s.messageID != 0 {
			m.clearInlineImages()
		}
		return nil
	}
	if s.messageID != msg.ID || s.sourceHTML != msg.BodyHTML || s.sourceHasAttachment != msg.HasAttachment {
		m.clearInlineImages()
		s.messageID = msg.ID
		s.sourceHTML = msg.BodyHTML
		s.sourceHasAttachment = msg.HasAttachment
		attachments := msg.AttachmentData
		if m.db != nil {
			if cached, err := m.db.GetAttachments(msg.ID); err == nil {
				attachments = cached
			}
		}
		for _, source := range messageImages(msg.BodyHTML, attachments) {
			asset := inlineAsset{source: source, id: imagepreview.NewInlineID()}
			if source.missing {
				asset.done = true
				asset.err = "embedded image unavailable"
			}
			s.assets = append(s.assets, asset)
		}
		m.refreshInlineContent()
	}
	if s.loading {
		return nil
	}
	for i, asset := range s.assets {
		if asset.done {
			continue
		}
		remote := strings.HasPrefix(asset.source.source, "http")
		if remote && s.requestedMessageID != s.messageID {
			continue
		}
		if s.pixels >= 40_000_000 {
			s.assets[i].done = true
			s.assets[i].err = "message image limit reached"
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.loading = true
		generation := s.generation
		return func() tea.Msg {
			var img imagepreview.Image
			var err error
			if remote {
				img, err = imagepreview.Fetch(ctx, asset.source.source)
			} else {
				img, err = imagepreview.Decode(asset.source.data)
			}
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			return inlineLoadedMsg{generation, i, img, err}
		}
	}
	return nil
}
func (m Model) handleInlineLoaded(msg inlineLoadedMsg) (tea.Model, tea.Cmd) {
	s := &m.inlineImages
	if msg.generation != s.generation || msg.index < 0 || msg.index >= len(s.assets) || !m.cfg.Display.ImagePreviews {
		return m, nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.loading = false
	asset := &s.assets[msg.index]
	asset.done = true
	if msg.err != nil {
		asset.err = safeImageLabel(msg.err.Error())
	} else {
		pixels := int64(msg.image.Width) * int64(msg.image.Height)
		if s.pixels+pixels > 40_000_000 {
			asset.err = "message image limit reached"
		} else {
			asset.image = msg.image
			s.pixels += pixels
		}
	}
	m.refreshInlineContent()
	// New image cells change line extents. Repaint the complete terminal frame
	// so the renderer cannot retain stale text from the previous layout.
	return m, tea.ClearScreen
}
func (m Model) loadRemoteInlineImages() (tea.Model, tea.Cmd) {
	if !m.cfg.Display.ImagePreviews {
		m.cfg.Display.ImagePreviews = true
		m.saveConfig()
	}
	if msg := m.currentContentMessage(); msg != nil {
		m.inlineImages.requestedMessageID = msg.ID
	}
	// Retry failed remote requests when the user explicitly requests another load.
	for i := range m.inlineImages.assets {
		asset := &m.inlineImages.assets[i]
		if strings.HasPrefix(asset.source.source, "http") && asset.err != "" {
			asset.done = false
			asset.err = ""
		}
	}
	m.setStatus("Loading images for this message", false)
	return m, m.clearStatusCmd()
}

func (m Model) renderInlineMessageBody(msg db.Message, width int) (string, bool) {
	s := m.inlineImages
	if !m.cfg.Display.ImagePreviews || !s.supported || s.messageID != msg.ID || len(s.assets) == 0 || width < 12 {
		return "", false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(msg.BodyHTML))
	if err != nil {
		return "", false
	}
	replacements := map[string]string{}
	used := map[int]bool{}
	for i, asset := range s.assets {
		if len(asset.image.PNG) == 0 {
			continue
		}
		grid, cols, rows := imagepreview.Grid(asset.id, asset.image, max(1, width-2), s.cellAspect)
		s.output.Register(asset.id, asset.image, cols, rows)
		token := "TMI" + strconv.FormatUint(uint64(asset.id), 36) + "X"
		replacements[token] = grid
		if asset.source.source != "" {
			doc.Find("img").Each(func(_ int, img *goquery.Selection) {
				if strings.TrimSpace(attrFirst(img, "src")) == asset.source.source {
					img.ParentsFiltered("table").SetAttr("data-tidemail-inline-images", "true")
					img.ReplaceWithHtml("<div>" + token + "</div>")
					used[i] = true
				}
			})
		}
	}
	// An image occupies the reading width, so linearize its containing tables
	// before conversion. Narrow data-table columns must not split image markers.
	tables := doc.Find("table[data-tidemail-inline-images]")
	for i := tables.Length() - 1; i >= 0; i-- {
		flattenLayoutTable(tables.Eq(i))
	}
	html, err := doc.Html()
	if err != nil {
		return "", false
	}
	// Generic conversion preserves image location; the compact provider renderer
	// intentionally discards layout, so use it only when inline images are off.
	body := renderHTMLBodyOpts(html, width, m.styles.Theme, m.styles.PlainUI, m.cfg.Display.FilterLinks)
	if strings.TrimSpace(body) == "" && msg.BodyText != "" {
		body = m.renderMessageForDisplay(db.Message{BodyText: msg.BodyText}, width).body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		for token, grid := range replacements {
			if strings.Contains(ansi.Strip(line), token) {
				replacement := grid
				if isCollapsibleQuoteLine(line) {
					replacement = "│ " + strings.ReplaceAll(grid, "\n", "\n│ ")
				}
				line = strings.ReplaceAll(line, token, "\n"+replacement+"\n")
			}
		}
		lines[i] = line
	}
	body = strings.Join(lines, "\n")
	blocked := false
	for i, asset := range s.assets {
		if asset.err != "" {
			body += "\n\n" + m.styles.ContentBody.Width(width).Render(asset.source.label+": "+asset.err)
		}
		if len(asset.image.PNG) > 0 && !used[i] {
			grid, _, _ := imagepreview.Grid(asset.id, asset.image, max(1, width-2), s.cellAspect)
			body += "\n\n" + asset.source.label + "\n" + grid
		}
		if strings.HasPrefix(asset.source.source, "http") && !asset.done && s.requestedMessageID != s.messageID {
			blocked = true
		}
	}
	if blocked {
		body += "\n\n" + m.styles.ContentBody.Width(width).Render("Remote images hidden — use Load remote images in the command palette.")
	}
	return body, true
}

// CancelImageLoads releases work owned by the final model during shutdown.
func (m Model) CancelImageLoads() {
	if m.inlineImages.cancel != nil {
		m.inlineImages.cancel()
	}
	if m.imagePreview.cancel != nil {
		m.imagePreview.cancel()
	}
}
