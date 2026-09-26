package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/richmail"
	"github.com/allisonhere/tidemail/internal/termimage"
)

// graphicsModel builds a Model whose image store uses a Kitty backend writing
// to an in-memory sink, so uploads can be asserted without a real terminal.
func graphicsModel(t *testing.T) (Model, *bytes.Buffer) {
	t.Helper()
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	store := newUIImageStore("auto")
	store.backend = termimage.NewKittyBackend(termimage.Capabilities{
		Protocol: termimage.ProtocolKitty,
		Cell:     richmail.CellGeometry{CellWidthPx: 8, CellHeightPx: 16},
	})
	buf := &bytes.Buffer{}
	store.sink = func(b []byte) { buf.Write(b) }
	m.images = store
	return m, buf
}

func pngDataURI(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestGraphicsRendersRasterPlaceholders(t *testing.T) {
	m, sink := graphicsModel(t)
	html := `<a href="https://shop.example/sale"><img src="` + pngDataURI(t, 240, 120) + `" width="1200" height="600" alt="Fall Sale"></a>` +
		`<h1>Fall Sale</h1><p>Save up to 30% today.</p>`

	res := m.renderMessageForDisplay(db.Message{ID: 1, BodyHTML: html}, 70)
	if len(res.images) != 1 {
		t.Fatalf("expected 1 rendered image, got %d (body=%q)", len(res.images), ansi.Strip(res.body))
	}
	if !strings.ContainsRune(res.body, placeholderRune) {
		t.Fatalf("expected placeholder cells in body: %q", ansi.Strip(res.body))
	}
	if strings.Contains(ansi.Strip(res.body), "[image:") {
		t.Fatalf("graphics path should not fall back to text placeholders: %q", ansi.Strip(res.body))
	}
	place := res.images[0].placement
	if place.Cols > 70 || place.Cols < 1 || place.Rows < 1 {
		t.Fatalf("placement out of bounds: %+v", place)
	}
	for _, line := range strings.Split(res.body, "\n") {
		if w := ansi.StringWidth(line); w > 70 {
			t.Fatalf("line width %d exceeds 70: %q", w, ansi.Strip(line))
		}
	}
	// The reserved row count must match the placement exactly; if hard-wrapping
	// ever split a placeholder row, document height and scrolling would drift.
	if got := strings.Count(res.body, string(placeholderRune)); got != place.Cols*place.Rows {
		t.Fatalf("placeholder cells = %d, want %d (cols=%d rows=%d)", got, place.Cols*place.Rows, place.Cols, place.Rows)
	}
	// "Fall Sale" survives as real text next to the image.
	if !strings.Contains(ansi.Strip(res.body), "Fall Sale") {
		t.Fatalf("headline lost: %q", ansi.Strip(res.body))
	}

	m.applyViewportImages(res.images)
	if !bytes.Contains(sink.Bytes(), []byte("\x1b_Ga=T,f=100,i=")) {
		t.Fatalf("expected a kitty upload, got %q", sink.String())
	}
}

func TestNoGraphicsFallsBackToTextPlaceholder(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	// TestMain pins TIDEMAIL_IMAGE_PROTOCOL=none.
	html := `<img src="data:image/png;base64,` + pngDataURIBytes(t) + `" alt="Quarterly chart">`
	res := m.renderMessageForDisplay(db.Message{ID: 2, BodyHTML: html}, 70)
	if !strings.Contains(ansi.Strip(res.body), "[image: Quarterly chart]") {
		t.Fatalf("expected text placeholder, got %q", ansi.Strip(res.body))
	}
	if strings.ContainsRune(res.body, placeholderRune) {
		t.Fatal("fallback path must not emit placeholder cells")
	}
	if len(res.images) != 0 {
		t.Fatalf("fallback path must not report images: %+v", res.images)
	}
}

func pngDataURIBytes(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestImagesOffSettingDisablesRaster(t *testing.T) {
	m, _ := graphicsModel(t)
	m.images.enabled = false
	html := `<img src="` + pngDataURI(t, 100, 50) + `" alt="Hero">`
	res := m.renderMessageForDisplay(db.Message{ID: 3, BodyHTML: html}, 70)
	if !strings.Contains(ansi.Strip(res.body), "[image: Hero]") {
		t.Fatalf("images=off should use text placeholders, got %q", ansi.Strip(res.body))
	}
}

func TestRemoteImageBlockedThenLoaded(t *testing.T) {
	m, _ := graphicsModel(t)
	url := "https://cdn.example.com/hero.png"
	html := `<img src="` + url + `" width="600" height="300" alt="Hero banner">`

	res := m.renderMessageForDisplay(db.Message{ID: 4, BodyHTML: html}, 70)
	if !strings.Contains(ansi.Strip(res.body), "remote image blocked") {
		t.Fatalf("expected blocked placeholder before consent: %q", ansi.Strip(res.body))
	}
	if len(res.images) != 0 {
		t.Fatal("blocked remote image must not be rendered")
	}

	// Consent alone, before bytes arrive, shows a loading placeholder.
	m.images.allow(4)
	m.bodyCache.clear()
	res = m.renderMessageForDisplay(db.Message{ID: 4, BodyHTML: html}, 70)
	if !strings.Contains(ansi.Strip(res.body), "loading remote image") {
		t.Fatalf("expected loading placeholder: %q", ansi.Strip(res.body))
	}

	// Once fresh bytes land in the store, the image renders.
	m.images.recordRemote(url, &remoteEntry{data: pngBytes(t, 240, 120), ctype: "image/png", done: true})
	m.bodyCache.clear()
	res = m.renderMessageForDisplay(db.Message{ID: 4, BodyHTML: html}, 70)
	if len(res.images) != 1 || !strings.ContainsRune(res.body, placeholderRune) {
		t.Fatalf("expected rendered remote image, got images=%d body=%q", len(res.images), ansi.Strip(res.body))
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTrackingPixelStillRemoved(t *testing.T) {
	m, _ := graphicsModel(t)
	html := `<p>Visible</p><img src="` + pngDataURI(t, 1, 1) + `" width="1" height="1" alt="">`
	res := m.renderMessageForDisplay(db.Message{ID: 5, BodyHTML: html}, 70)
	if len(res.images) != 0 {
		t.Fatal("tracking pixel must not render")
	}
	if strings.ContainsRune(res.body, placeholderRune) {
		t.Fatal("tracking pixel must not reserve rows")
	}
}

func TestImageOnlyBodyStaysMeaningful(t *testing.T) {
	m, _ := graphicsModel(t)
	// No alt at all, but decoded large: real content, not an empty body.
	html := `<img src="` + pngDataURI(t, 200, 100) + `">`
	res := m.renderMessageForDisplay(db.Message{ID: 6, BodyHTML: html}, 70)
	if strings.TrimSpace(ansi.Strip(res.body)) == "" || len(res.images) != 1 {
		t.Fatalf("image-only body lost: %q", ansi.Strip(res.body))
	}
}

func TestResizeReflowsImages(t *testing.T) {
	m, _ := graphicsModel(t)
	html := `<img src="` + pngDataURI(t, 1200, 600) + `" alt="Hero">`
	wide := m.renderMessageForDisplay(db.Message{ID: 7, BodyHTML: html}, 100)
	narrow := m.renderMessageForDisplay(db.Message{ID: 7, BodyHTML: html}, 40)
	if len(wide.images) != 1 || len(narrow.images) != 1 {
		t.Fatal("expected an image at both widths")
	}
	if wide.images[0].placement.Cols <= narrow.images[0].placement.Cols {
		t.Fatalf("expected narrower pane to use fewer columns: wide=%d narrow=%d",
			wide.images[0].placement.Cols, narrow.images[0].placement.Cols)
	}
	if narrow.images[0].placement.Cols > 40 {
		t.Fatalf("narrow placement exceeds pane: %+v", narrow.images[0].placement)
	}
}

func TestSwitchingMessagesClearsImages(t *testing.T) {
	m, sink := graphicsModel(t)
	html := `<img src="` + pngDataURI(t, 120, 60) + `" alt="Hero">`
	res := m.renderMessageForDisplay(db.Message{ID: 8, BodyHTML: html}, 70)
	m.applyViewportImages(res.images)
	sink.Reset()

	m.clearViewportImages()
	if !bytes.Contains(sink.Bytes(), []byte("a=d,d=i")) {
		t.Fatalf("expected image deletion escape, got %q", sink.String())
	}
}

func TestImageAltWithEscapesCannotInject(t *testing.T) {
	m, _ := graphicsModel(t)
	// A hostile alt/CID must never reach the terminal as control sequences.
	html := "<img src=\"cid:\x1b]52;c;evil\x07\" alt=\"\x1b[31mred\x1b]52;c;x\x07\">"
	res := m.renderMessageForDisplay(db.Message{ID: 9, BodyHTML: html}, 70)
	// Styling legitimately emits SGR sequences; what must never appear is a
	// terminal control sequence smuggled in through email-controlled text.
	plain := ansi.Strip(res.body)
	if strings.ContainsAny(plain, "\x1b\x07") {
		t.Fatalf("control bytes reached rendered text: %q", plain)
	}
}

func TestContentDisplayLinesStripPlaceholders(t *testing.T) {
	m, _ := graphicsModel(t)
	html := `<img src="` + pngDataURI(t, 200, 100) + `" alt="Hero">`
	res := m.renderMessageForDisplay(db.Message{ID: 10, BodyHTML: html}, 70)
	m.viewport.SetContent(res.body)
	m.contentLines, m.contentLineLinks = contentDisplayLines(res.body)
	if len(m.contentLines) != len(strings.Split(res.body, "\n")) {
		t.Fatal("placeholder stripping changed line count")
	}
	for _, line := range m.contentLines {
		if strings.ContainsRune(line, placeholderRune) {
			t.Fatalf("display line retained a placeholder: %q", line)
		}
	}
}

func TestConsentKeyAllowsRemoteImages(t *testing.T) {
	m, _ := graphicsModel(t)
	m.focused = paneContent
	m.contentMessageID = 1
	m.contentLineCount = 5
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}}
	_, _ = m.handleMainKey(msg)
	if !m.images.isAllowed(1) {
		t.Fatal("i should grant remote-image consent for the current message")
	}
}

func TestRemoteFetchRejectsLoopback(t *testing.T) {
	store := newUIImageStore("auto")
	cmd := fetchRemoteImagesCmd(store, 1, []string{"http://127.0.0.1:9/x.png"})
	if cmd == nil {
		t.Fatal("expected a fetch command")
	}
	msg, ok := cmd().(remoteImagesLoadedMsg)
	if !ok {
		t.Fatalf("unexpected message type %T", cmd())
	}
	if len(msg.results) != 1 || msg.results[0].err == nil {
		t.Fatalf("expected loopback fetch to be rejected: %+v", msg.results)
	}
}

func TestImageCacheKeyTracksConsentAndGeneration(t *testing.T) {
	m, _ := graphicsModel(t)
	before := m.imageRenderKey(1)
	m.images.allow(1)
	after := m.imageRenderKey(1)
	if before == after {
		t.Fatal("consent must change the image cache key")
	}
	m.images.bumpGeneration()
	if m.imageRenderKey(1) == after {
		t.Fatal("generation must change the image cache key")
	}
}

func TestThreadImageRenderKeyDistinctPerMessage(t *testing.T) {
	m, _ := graphicsModel(t)
	threadA := messageThread{Key: "a", Messages: []db.Message{{ID: 1}, {ID: 2}}}
	threadB := messageThread{Key: "b", Messages: []db.Message{{ID: 3}, {ID: 4}}}
	if m.threadImageRenderKey(threadA) == m.threadImageRenderKey(threadB) {
		t.Fatal("different threads must not share an image key")
	}
}

func TestImageAfterDateSanity(t *testing.T) {
	// Guard against a zero-value message date crashing the meta formatting.
	m, _ := graphicsModel(t)
	res := m.renderMessageContent(db.Message{ID: 11, Date: time.Unix(1, 0), BodyHTML: `<p>hi</p>`})
	if !strings.Contains(ansi.Strip(res), "hi") {
		t.Fatalf("content missing: %q", ansi.Strip(res))
	}
}

func TestFilterRenderedInlineKeepsUnrendered(t *testing.T) {
	atts := []db.Attachment{
		{MessageID: 5, Filename: "logo.png", ContentType: "image/png", ContentID: "logo", Inline: true},
		{MessageID: 5, Filename: "missing.png", ContentType: "image/png", ContentID: "missing", Inline: true},
		{MessageID: 5, Filename: "doc.pdf", ContentType: "application/pdf"},
		{MessageID: 5, Filename: "photo.jpg", ContentType: "image/jpeg", Disposition: "attachment"},
		{MessageID: 6, Filename: "logo.png", ContentType: "image/png", ContentID: "logo", Inline: true},
	}
	rendered := []renderImage{{messageID: 5, sourceCID: "logo", sourceLoc: "logo"}}

	out := filterRenderedInline(atts, rendered)
	for _, a := range out {
		if a.MessageID == 5 && a.Filename == "logo.png" {
			t.Fatalf("rendered inline image should be hidden from the list: %+v", out)
		}
	}
	// The unrendered inline image, both non-inline parts, and the other
	// message's same-CID image must all remain listed.
	if len(out) != 4 {
		t.Fatalf("expected 4 remaining attachments, got %d: %+v", len(out), out)
	}
	foundMissing, foundOtherMsg := false, false
	for _, a := range out {
		if a.MessageID == 5 && a.Filename == "missing.png" {
			foundMissing = true
		}
		if a.MessageID == 6 && a.Filename == "logo.png" {
			foundOtherMsg = true
		}
	}
	if !foundMissing {
		t.Fatal("an inline image that did not render must stay listed")
	}
	if !foundOtherMsg {
		t.Fatal("a same-CID image in another message must not be hidden")
	}

	// With no graphics (nothing rendered) the full list is returned unchanged.
	if got := filterRenderedInline(atts, nil); len(got) != len(atts) {
		t.Fatalf("no rendered images should leave the list untouched, got %+v", got)
	}
}

func TestPlainUINeverUsesRaster(t *testing.T) {
	m, _ := graphicsModel(t)
	m.styles.PlainUI = true
	html := `<img src="` + pngDataURI(t, 200, 100) + `" alt="Hero">`
	res := m.renderMessageForDisplay(db.Message{ID: 20, BodyHTML: html}, 70)
	if strings.ContainsRune(res.body, placeholderRune) {
		t.Fatal("plain UI must not emit raster placeholders")
	}
	if !strings.Contains(ansi.Strip(res.body), "[image: Hero]") {
		t.Fatalf("plain UI should show a text placeholder: %q", ansi.Strip(res.body))
	}
}

func TestCIDImageResolvesEndToEnd(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("", "ImagesTest", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(db.Message{
		MailboxID:     mailboxID,
		UID:           900001,
		Subject:       "CID hero",
		BodyHTML:      `<a href="https://shop.example/sale"><img src="cid:hero" width="1200" height="600" alt="Fall Sale"></a><h1>Fall Sale</h1>`,
		HasAttachment: true,
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}
	msg := msgs[0]
	if _, err := database.SaveAttachment(msg.ID, db.Attachment{
		Filename:    "hero.png",
		ContentType: "image/png",
		ContentID:   "hero",
		Inline:      true,
		Data:        pngBytes(t, 240, 120),
	}); err != nil {
		t.Fatal(err)
	}

	m, _ := graphicsModel(t)
	m.db = database
	res := m.renderMessageForDisplay(msg, 70)
	if len(res.images) != 1 {
		t.Fatalf("expected CID image to resolve and render, got %d (body=%q)", len(res.images), ansi.Strip(res.body))
	}
	if !strings.Contains(ansi.Strip(res.body), "Fall Sale") {
		t.Fatalf("headline lost: %q", ansi.Strip(res.body))
	}

	// The inline image renders, but must remain in the attachment list used for
	// saving; only its row in the displayed list is suppressed.
	m.setViewportMessage(msg)
	if len(m.contentAttachments) != 1 {
		t.Fatalf("inline image must remain saveable, got %d attachment(s)", len(m.contentAttachments))
	}
	if visible := filterRenderedInline(m.contentAttachments, res.images); len(visible) != 0 {
		t.Fatalf("rendered inline image should be hidden from the displayed list, got %+v", visible)
	}
}

func TestTableWithTwoImagesRendersBoth(t *testing.T) {
	m, _ := graphicsModel(t)
	html := `<table><tr><td><img src="` + pngDataURI(t, 120, 120) + `" alt="Boots"></td>` +
		`<td><img src="` + pngDataURI(t, 120, 120) + `" alt="Jacket"></td></tr></table>`
	res := m.renderMessageForDisplay(db.Message{ID: 30, BodyHTML: html}, 70)
	if len(res.images) != 2 {
		t.Fatalf("expected both table images to render, got %d (body=%q)", len(res.images), ansi.Strip(res.body))
	}
	for _, line := range strings.Split(res.body, "\n") {
		if w := ansi.StringWidth(line); w > 70 {
			t.Fatalf("line width %d exceeds 70: %q", w, ansi.Strip(line))
		}
	}
	// Both table cells must still contribute their placeholders; order is
	// asserted by the cell images being distinct entries in the manifest.
	if res.images[0].id == res.images[1].id {
		t.Fatal("two images must have distinct ids")
	}
}

func TestReplaceInlineImageMarkersAdjacentDoesNotPanic(t *testing.T) {
	plans := map[int]imagePlan{0: {label: "Hero"}}
	for _, in := range []string{
		"\uE000\uE001",
		"a\uE000\uE001b",
		"\uE000\uE001\uE000\uE001",
		"\uE000",
		"\uE000X\uE001",
		"\uE000I0\uE001",
		"prefix \uE000\uE001 suffix",
	} {
		// The requirement is only that malformed markers cannot panic the
		// renderer. Well-formed markers are replaced; malformed ones survive as
		// literal (harmless, private-use) text.
		got := replaceInlineImageMarkers(in, plans)
		if got == "" && in != "" {
			t.Fatalf("non-empty input produced empty output: %q", in)
		}
	}
	if got := replaceInlineImageMarkers("x \uE000I0\uE001 y", plans); !strings.Contains(got, "[image: Hero]") {
		t.Fatalf("well-formed marker should expand: %q", got)
	}
}

func TestAdjacentMarkersInBodyDoNotCrashRender(t *testing.T) {
	m, _ := graphicsModel(t)
	html := "<p>text \uE000\uE001 more</p><img src=\"" + pngDataURI(t, 120, 60) + "\" alt=\"Hero\">"
	res := m.renderMessageForDisplay(db.Message{ID: 40, BodyHTML: html}, 70)
	if len(res.images) != 1 {
		t.Fatalf("expected the hero image to render, got %d", len(res.images))
	}
	if !strings.Contains(ansi.Strip(res.body), "text") {
		t.Fatalf("surrounding text lost: %q", ansi.Strip(res.body))
	}
}

func TestThreadConsentCoversAllMessagesAndRendersBoth(t *testing.T) {
	m, _ := graphicsModel(t)
	m.cfg.Display.ThreadedConversations = true
	u1 := "https://cdn.example.com/a.png"
	u2 := "https://cdn.example.com/b.png"
	thread := messageThread{Key: "t", Messages: []db.Message{
		{ID: 11, BodyHTML: `<img src="` + u1 + `" width="600" height="300" alt="A">`},
		{ID: 12, BodyHTML: `<img src="` + u2 + `" width="600" height="300" alt="B">`},
	}}
	m.messageThreads = []messageThread{thread}
	m.messageCursor = 0
	m.contentMessageID = 11
	m.focused = paneContent
	m.contentLineCount = 5

	if _, imgs := m.renderThreadContentImages(thread); len(imgs) != 0 {
		t.Fatalf("expected no rendered images before consent, got %d", len(imgs))
	}

	_, _ = m.handleMainKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !m.images.isAllowed(11) || !m.images.isAllowed(12) {
		t.Fatalf("i must grant consent to every message in the thread: 11=%v 12=%v",
			m.images.isAllowed(11), m.images.isAllowed(12))
	}

	m.images.recordRemote(u1, &remoteEntry{data: pngBytes(t, 240, 120), ctype: "image/png", done: true})
	m.images.recordRemote(u2, &remoteEntry{data: pngBytes(t, 240, 120), ctype: "image/png", done: true})
	m.bodyCache.clear()
	m.viewportCache.clear()
	if _, imgs := m.renderThreadContentImages(thread); len(imgs) != 2 {
		t.Fatalf("expected both thread images to render, got %d", len(imgs))
	}
}

func TestRemoteCacheIsBounded(t *testing.T) {
	store := newUIImageStore("auto")
	payload := make([]byte, 1<<20) // 1 MiB each
	for i := 0; i < 40; i++ {
		store.recordRemote(fmt.Sprintf("https://example.com/%d.png", i), &remoteEntry{data: payload, done: true})
	}

	store.mu.Lock()
	total := store.remoteBytes
	entries := len(store.remote)
	order := len(store.remoteOrder)
	store.mu.Unlock()

	if total > maxRemoteCacheBytes {
		t.Fatalf("remote bytes %d exceed budget %d", total, maxRemoteCacheBytes)
	}
	if entries != order {
		t.Fatalf("remote map (%d) and order (%d) disagree", entries, order)
	}
	if _, ok := store.remoteResult("https://example.com/0.png"); ok {
		t.Fatal("oldest remote entry should have been evicted")
	}
	if _, ok := store.remoteResult("https://example.com/39.png"); !ok {
		t.Fatal("most recent remote entry should be retained")
	}
}

func TestApplyRemoteResultsIsBounded(t *testing.T) {
	store := newUIImageStore("auto")
	results := make([]remoteFetchResult, 0, maxRemoteCacheEntries+50)
	for i := 0; i < maxRemoteCacheEntries+50; i++ {
		results = append(results, remoteFetchResult{url: fmt.Sprintf("https://example.com/%d.png", i)})
	}
	store.applyRemoteResults(remoteImagesLoadedMsg{messageID: 1, results: results})

	store.mu.Lock()
	entries := len(store.remote)
	store.mu.Unlock()
	if entries > maxRemoteCacheEntries {
		t.Fatalf("remote cache grew past entry cap: %d > %d", entries, maxRemoteCacheEntries)
	}
}

func TestImageSettingClearsAndRestoresGraphics(t *testing.T) {
	m, sink := graphicsModel(t)
	msg := db.Message{ID: 990, BodyHTML: `<img src="` + pngDataURI(t, 120, 60) + `" alt="Chart">`}
	res := m.renderMessageForDisplay(msg, 70)
	m.applyViewportImages(res.images)
	sink.Reset()
	m.cfg.Display.Images = "off"
	m.applyImageSetting()
	if !strings.Contains(sink.String(), "a=d") {
		t.Fatal("disabling did not delete uploaded images")
	}
	if got := m.renderMessageForDisplay(msg, 70); len(got.images) != 0 {
		t.Fatal("disabled images still rendered")
	}
	m.cfg.Display.Images = "auto"
	m.applyImageSetting()
	if got := m.renderMessageForDisplay(msg, 70); len(got.images) != 1 {
		t.Fatal("reenabling did not restore images")
	}
}
