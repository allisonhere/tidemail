package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/richmail"
	"github.com/allisonhere/tidemail/internal/termimage"
)

// Image markers are private-use runes embedded by the HTML->markdown rule and
// replaced with either raster placeholder rows or a fallback text block once
// the finished body is known. They survive glamour and ANSI stripping, unlike
// control characters.
const (
	imageMarkerOpen  = '\uE000'
	imageMarkerClose = '\uE001'
	imageMarkerImage = 'I'
	imageMarkerBlock = 'B'
	imageMarkerLoad  = 'L'
)

// placeholderRune mirrors termimage's placeholder so this package can detect
// and strip placeholder cells without importing internals.
const placeholderRune = '\U0010EEEE'

// renderImage is one image actually placed in the reading pane, carried from the
// body render to the terminal upload step. Only identity strings are retained,
// never the source bytes, so a data URI cannot keep its payload alive through
// the viewport cache once decoding has happened.
type renderImage struct {
	id        uint32
	messageID int64
	sourceCID string
	sourceLoc string
	image     *richmail.Decoded
	placement richmail.Placement
	label     string
	link      string
}

// imagePlan is the per-image decision made before markdown conversion.
type imagePlan struct {
	image    richmail.Image
	resolved richmail.Resolved
	id       uint32
	place    richmail.Placement
	label    string
	emit     bool
	blocked  bool
	loading  bool
}

// remoteEntry memoises one remote fetch.
type remoteEntry struct {
	data  []byte
	ctype string
	err   error
	done  bool
}

// imageRenderContext is the per-message state handed to the HTML renderer.
type imageRenderContext struct {
	store       *uiImageStore
	messageID   int64
	allowRemote bool
	availCols   int
	cellGeom    richmail.CellGeometry
	parts       *richmail.PartStore
}

// uiImageStore owns the terminal graphics backend and every cache that keeps
// image-heavy mail cheap to redraw. It is shared by pointer across the value
// copies Bubble Tea makes of Model.
type uiImageStore struct {
	backend    termimage.Backend
	limits     richmail.Limits
	remoteOpts richmail.RemoteOptions
	ids        *termimage.IDAllocator
	enabled    bool

	mu          sync.Mutex
	partStores  map[int64]*richmail.PartStore
	decoded     map[string]*richmail.Decoded
	remote      map[string]*remoteEntry
	remoteOrder []string
	remoteBytes int
	allowed     map[int64]bool
	idFor       map[string]uint32
	activeIDs   []uint32
	activeKey   string
	generation  uint64
	tty         *os.File
	// sink, when non-nil, receives terminal escape bytes instead of /dev/tty.
	// Tests set it to assert uploads without touching a real terminal.
	sink func([]byte)
}

// maxDecodedCacheEntries bounds decoded-image memory across a large mailbox.
const maxDecodedCacheEntries = 128

// maxRemoteCacheBytes bounds the bytes of remote image data retained for
// redraws. Remote payloads are capped at 8 MiB each, so an entry count alone
// would still allow hundreds of megabytes; eviction is by byte budget in
// insertion order.
const maxRemoteCacheBytes = 32 << 20

// maxRemoteCacheEntries bounds the remote map when payloads are tiny or fail
// (a failed fetch stores an error entry with no bytes).
const maxRemoteCacheEntries = 256

func newUIImageStore(imagesSetting string) *uiImageStore {
	caps := termimage.DetectFromEnv()
	return &uiImageStore{
		backend:    termimage.New(caps),
		limits:     richmail.DefaultLimits(),
		remoteOpts: richmail.DefaultRemoteOptions(),
		ids:        termimage.NewIDAllocator(),
		enabled:    !strings.EqualFold(strings.TrimSpace(imagesSetting), "off"),
		partStores: map[int64]*richmail.PartStore{},
		decoded:    map[string]*richmail.Decoded{},
		remote:     map[string]*remoteEntry{},
		allowed:    map[int64]bool{},
		idFor:      map[string]uint32{},
	}
}

// graphics returns whether real raster images can be placed.
func (s *uiImageStore) graphics() bool {
	return s != nil && s.enabled && s.backend.Capabilities().GraphicsEnabled()
}

// applyImageSetting commits the saved preference without discarding consent.
func (m *Model) applyImageSetting() {
	if m.images == nil {
		m.images = newUIImageStore(m.cfg.Display.Images)
	}
	enabled := !strings.EqualFold(strings.TrimSpace(m.cfg.Display.Images), "off")
	if m.images.enabled == enabled {
		return
	}
	// Delete while graphics are still enabled, before clearImages becomes a no-op.
	m.images.clearImages()
	m.images.enabled = enabled
	m.images.bumpGeneration()
	m.bodyCache.clear()
	m.viewportCache.clear()
}

// isAllowed reports whether the reader has consented to remote images for one
// message.
func (s *uiImageStore) isAllowed(messageID int64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allowed[messageID]
}

func (s *uiImageStore) allow(messageID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.allowed[messageID] = true
	s.generation++
	s.mu.Unlock()
}

// bumpGeneration invalidates render cache keys after the image state changes.
func (s *uiImageStore) bumpGeneration() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.generation++
	s.mu.Unlock()
}

func (s *uiImageStore) gen() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation
}

// partsFor returns the cached MIME part index for a message, loading it once.
func (s *uiImageStore) partsFor(messageID int64, load func() []richmail.Part) *richmail.PartStore {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if store, ok := s.partStores[messageID]; ok {
		s.mu.Unlock()
		return store
	}
	s.mu.Unlock()

	store := richmail.NewPartStore(load())

	s.mu.Lock()
	s.partStores[messageID] = store
	s.mu.Unlock()
	return store
}

// decodeCacheKey is the identity under which decoded bytes are reused. It mixes
// the message so two messages sharing a Content-ID never share pixels.
func decodeCacheKey(messageID int64, im richmail.Image) string {
	switch im.Source.Kind {
	case richmail.SourceCID:
		return fmt.Sprintf("%d:cid:%s", messageID, richmail.CIDKey(im.Source.CID))
	case richmail.SourceData:
		return fmt.Sprintf("%d:data:%d:%d", messageID, im.Index, len(im.Source.Data))
	default:
		return fmt.Sprintf("%d:raw:%d:%s", messageID, im.Index, im.Source.Raw)
	}
}

func (s *uiImageStore) cachedDecode(key string) (*richmail.Decoded, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.decoded[key]
	return d, ok
}

func (s *uiImageStore) storeDecode(key string, d *richmail.Decoded) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.decoded) >= maxDecodedCacheEntries {
		// Cheap deterministic eviction: drop one arbitrary entry. The cache is
		// a redraw optimisation, not a correctness requirement.
		for k := range s.decoded {
			delete(s.decoded, k)
			break
		}
	}
	s.decoded[key] = d
}

// remoteResult returns the fetch outcome for a URL.
func (s *uiImageStore) remoteResult(url string) (*remoteEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.remote[url]
	return e, ok
}

// recordRemote stores one fetch outcome, evicting the oldest entries until the
// cache fits its byte and entry budgets. Every production path (including the
// async result handler) must go through here; writing s.remote directly leaks
// downloaded bytes for the life of the process.
func (s *uiImageStore) recordRemote(url string, e *remoteEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.remote[url]; ok {
		s.remoteBytes -= len(old.data)
		delete(s.remote, url)
		for i, key := range s.remoteOrder {
			if key == url {
				s.remoteOrder = append(s.remoteOrder[:i], s.remoteOrder[i+1:]...)
				break
			}
		}
	}
	s.remote[url] = e
	s.remoteOrder = append(s.remoteOrder, url)
	s.remoteBytes += len(e.data)

	for len(s.remoteOrder) > 0 &&
		(s.remoteBytes > maxRemoteCacheBytes || len(s.remoteOrder) > maxRemoteCacheEntries) {
		oldest := s.remoteOrder[0]
		s.remoteOrder = s.remoteOrder[1:]
		if victim, ok := s.remote[oldest]; ok {
			s.remoteBytes -= len(victim.data)
			delete(s.remote, oldest)
		}
	}
	s.generation++
}

// allocateID returns a stable image id for a message/image pair, allocating on
// first use so re-renders address the same terminal image.
func (s *uiImageStore) allocateID(messageID int64, index int) uint32 {
	key := fmt.Sprintf("%d:%d", messageID, index)
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.idFor[key]; ok {
		return id
	}
	id := s.ids.Next()
	s.idFor[key] = id
	return id
}

// imageSetKey identifies a set of images so identical redraws do not re-upload.
func (s *uiImageStore) imageSetKey(images []renderImage) string {
	if len(images) == 0 {
		return ""
	}
	var b strings.Builder
	for _, im := range images {
		fmt.Fprintf(&b, "%d:%d:%d;", im.id, im.placement.Cols, im.placement.Rows)
	}
	return b.String()
}

// applyImages uploads the current view's images and deletes the previous set,
// so switching messages cannot leak terminal image memory.
func (s *uiImageStore) applyImages(images []renderImage) {
	if !s.graphics() {
		return
	}
	key := s.imageSetKey(images)

	s.mu.Lock()
	if key == s.activeKey {
		s.mu.Unlock()
		return
	}
	old := s.activeIDs
	s.activeIDs = nil
	s.activeKey = key
	s.mu.Unlock()

	var buf bytes.Buffer
	for _, id := range old {
		buf.Write(s.backend.Delete(id))
	}
	var active []uint32
	for _, im := range images {
		if im.image == nil {
			continue
		}
		data, ok := s.backend.Transmit(im.id, im.image.Image, im.placement.Cols, im.placement.Rows, im.placement.PixelWidth, im.placement.PixelHeight)
		if !ok {
			continue
		}
		buf.Write(data)
		active = append(active, im.id)
	}

	s.mu.Lock()
	s.activeIDs = active
	s.mu.Unlock()
	s.writeTTY(buf.Bytes())
}

// clearImages removes every transmitted image from the terminal.
func (s *uiImageStore) clearImages() {
	if !s.graphics() {
		return
	}
	s.mu.Lock()
	ids := s.activeIDs
	s.activeIDs = nil
	s.activeKey = ""
	s.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	var buf bytes.Buffer
	for _, id := range ids {
		buf.Write(s.backend.Delete(id))
	}
	s.writeTTY(buf.Bytes())
}

// writeTTY writes an escape sequence straight to the controlling terminal.
// It is called during Update, while Bubble Tea's renderer is not writing, so
// the APC upload cannot be interleaved with a frame.
func (s *uiImageStore) writeTTY(data []byte) {
	if len(data) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sink != nil {
		s.sink(data)
		return
	}
	if s.tty == nil {
		f, err := openTTY()
		if err != nil {
			return
		}
		s.tty = f
	}
	_, _ = s.tty.Write(data)
}

// imageContext builds the per-message rendering context, or nil when raster
// rendering is disabled or unsupported so the fallback path is used verbatim.
func (m Model) imageContext(msg db.Message, width int) *imageRenderContext {
	if m.images == nil || !m.images.graphics() {
		return nil
	}
	// The retro/plain UI promises no escape sequences beyond plain text, so it
	// always takes the text-placeholder path.
	if m.styles.PlainUI {
		return nil
	}
	// Text-only mail never touches the attachment store or the image caches.
	if strings.TrimSpace(msg.BodyHTML) == "" {
		return nil
	}
	messageID := msg.ID
	store := m.images
	var parts *richmail.PartStore
	if !msg.HasAttachment {
		// No MIME parts means no CID/data resolution is possible, so skip the
		// database lookup entirely for the common text-only newsletter shell.
		parts = richmail.NewPartStore(nil)
	} else {
		parts = store.partsFor(messageID, func() []richmail.Part {
			if m.db == nil || messageID == 0 {
				return nil
			}
			atts, err := m.db.GetImageParts(messageID)
			if err != nil {
				return nil
			}
			out := make([]richmail.Part, 0, len(atts))
			for _, a := range atts {
				out = append(out, richmail.Part{
					ContentID:       a.ContentID,
					ContentLocation: a.ContentLocation,
					ContentType:     a.ContentType,
					Data:            a.Data,
				})
			}
			return out
		})
	}
	return &imageRenderContext{
		store:       store,
		messageID:   messageID,
		allowRemote: store.isAllowed(messageID),
		availCols:   width,
		cellGeom:    store.backend.Capabilities().Cell,
		parts:       parts,
	}
}

// buildImagePlans resolves and lays out every image in a manifest. Images that
// cannot be shown are simply absent from the map, so the markdown rule falls
// back to the historic alt-text placeholder.
func buildImagePlans(manifest *richmail.Manifest, ictx *imageRenderContext) map[int]imagePlan {
	plans := map[int]imagePlan{}
	if manifest == nil || ictx == nil {
		return plans
	}
	for _, im := range manifest.Images {
		if im.Tracking {
			continue
		}
		plan := imagePlan{image: im, label: im.Label()}

		switch im.Source.Kind {
		case richmail.SourceRemote:
			plan = planRemoteImage(im, ictx, plan)
		case richmail.SourceCID, richmail.SourceData, richmail.SourceUnknown:
			plan = planEmbeddedImage(im, ictx, plan)
		default:
			continue
		}

		if plan.emit || plan.blocked || plan.loading {
			plans[im.Index] = plan
		}
	}
	return plans
}

func planEmbeddedImage(im richmail.Image, ictx *imageRenderContext, plan imagePlan) imagePlan {
	key := decodeCacheKey(ictx.messageID, im)
	if d, ok := ictx.store.cachedDecode(key); ok {
		plan.resolved = richmail.Resolved{Status: richmail.ResolveOK, Decoded: d}
	} else {
		plan.resolved = richmail.ResolveEmbedded(im, ictx.parts, ictx.store.limits)
		if plan.resolved.Status == richmail.ResolveOK && plan.resolved.Decoded != nil {
			ictx.store.storeDecode(key, plan.resolved.Decoded)
		}
	}
	if plan.resolved.Status != richmail.ResolveOK || plan.resolved.Decoded == nil {
		return plan
	}
	return finalizeImagePlan(im, ictx, plan, plan.resolved.Decoded)
}

func planRemoteImage(im richmail.Image, ictx *imageRenderContext, plan imagePlan) imagePlan {
	if im.Decorative && !hasLargeHint(im) {
		return plan
	}
	if !ictx.allowRemote {
		plan.blocked = true
		return plan
	}
	entry, ok := ictx.store.remoteResult(im.Source.URL)
	if !ok || !entry.done {
		plan.loading = true
		return plan
	}
	if entry.err != nil {
		plan.blocked = true
		return plan
	}
	key := "remote:" + im.Source.URL
	if d, ok := ictx.store.cachedDecode(key); ok {
		plan.resolved = richmail.Resolved{Status: richmail.ResolveOK, Decoded: d}
		return finalizeImagePlan(im, ictx, plan, d)
	}
	plan.resolved = richmail.DecodeRemote(entry.data, ictx.store.limits)
	if plan.resolved.Status != richmail.ResolveOK || plan.resolved.Decoded == nil {
		return plan
	}
	ictx.store.storeDecode(key, plan.resolved.Decoded)
	return finalizeImagePlan(im, ictx, plan, plan.resolved.Decoded)
}

// finalizeImagePlan decides whether a decoded image earns space in the pane and
// computes its cell rectangle.
func finalizeImagePlan(im richmail.Image, ictx *imageRenderContext, plan imagePlan, decoded *richmail.Decoded) imagePlan {
	if im.Decorative && !largeDecoded(decoded) {
		return plan
	}
	place, ok := richmail.Layout(richmail.Intrinsic{
		PixelWidth:  decoded.Width,
		PixelHeight: decoded.Height,
		HintWidth:   im.HintWidth,
		HintHeight:  im.HintHeight,
		MaxWidth:    im.MaxWidth,
		Align:       im.Align,
	}, ictx.availCols, ictx.cellGeom)
	if !ok {
		return plan
	}
	plan.id = ictx.store.allocateID(ictx.messageID, im.Index)
	plan.place = place
	plan.emit = true
	return plan
}

// largeDecoded reports whether a decoded image is large enough to be content
// even if its alt text marked it as chrome.
func largeDecoded(d *richmail.Decoded) bool {
	if d == nil {
		return false
	}
	return d.Width >= 48 || d.Height >= 48
}

func hasLargeHint(im richmail.Image) bool {
	return im.HintWidth >= 48 || im.HintHeight >= 48
}

// imageMarker builds the sentinel for one plan kind and image index.
func imageMarker(kind rune, index int) string {
	return string([]rune{imageMarkerOpen, kind}) + strconv.Itoa(index) + string(imageMarkerClose)
}

// parseImageMarkerLine parses a line that consists solely of one marker. ANSI
// styling added by glamour is stripped first, since the marker is just text to
// the markdown renderer.
func parseImageMarkerLine(line string) (kind rune, index int, ok bool) {
	s := strings.TrimSpace(ansi.Strip(line))
	runes := []rune(s)
	if len(runes) < 4 || runes[0] != imageMarkerOpen || runes[len(runes)-1] != imageMarkerClose {
		return 0, 0, false
	}
	kind = runes[1]
	digits := string(runes[2 : len(runes)-1])
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 {
		return 0, 0, false
	}
	return kind, n, true
}

// expandImageMarkers replaces marker lines with raster placeholder rows or
// fallback text, returning the finished body and the images to upload.
func expandImageMarkers(rendered string, plans map[int]imagePlan, ictx *imageRenderContext) (string, []renderImage) {
	lines := strings.Split(rendered, "\n")
	out := make([]string, 0, len(lines))
	var images []renderImage
	for _, line := range lines {
		kind, index, ok := parseImageMarkerLine(line)
		if !ok {
			out = append(out, replaceInlineImageMarkers(line, plans))
			continue
		}
		plan, found := plans[index]
		if !found {
			out = append(out, fallbackImageText(""))
			continue
		}
		switch kind {
		case imageMarkerImage:
			rows := ictx.store.backend.PlaceholderRows(plan.id, plan.place.Cols, plan.place.Rows)
			if len(rows) == 0 {
				out = append(out, fallbackImageText(plan.label))
				continue
			}
			pad := strings.Repeat(" ", plan.place.OffsetCols)
			for _, row := range rows {
				out = append(out, pad+row)
			}
			images = append(images, renderImage{
				id:        plan.id,
				messageID: ictx.messageID,
				sourceCID: plan.image.Source.CID,
				sourceLoc: richmail.LocationKey(plan.image.Source.Raw),
				image:     plan.resolved.Decoded,
				placement: plan.place,
				label:     plan.label,
				link:      plan.image.Link,
			})
		case imageMarkerBlock:
			out = append(out, blockImageText(plan.label))
		case imageMarkerLoad:
			out = append(out, loadingImageText(plan.label))
		default:
			out = append(out, fallbackImageText(plan.label))
		}
	}
	return strings.Join(out, "\n"), images
}

// replaceInlineImageMarkers handles the defensive case where glamour merged a
// marker into surrounding prose: the marker becomes an ordinary text
// placeholder so no text is lost.
func replaceInlineImageMarkers(line string, plans map[int]imagePlan) string {
	if !strings.ContainsRune(line, imageMarkerOpen) {
		return line
	}
	var b strings.Builder
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		if runes[i] != imageMarkerOpen {
			b.WriteRune(runes[i])
			continue
		}
		j := i + 1
		for j < len(runes) && runes[j] != imageMarkerClose {
			j++
		}
		if j >= len(runes) {
			b.WriteRune(runes[i])
			continue
		}
		// An opening marker with nothing between it and the close (or only a
		// lone kind rune) is malformed, not an image reference. Emitting the
		// open rune and leaving i in place lets the loop write the close rune
		// next iteration instead of slicing runes[i+2:j] out of bounds.
		if j <= i+1 {
			b.WriteRune(runes[i])
			continue
		}
		label := ""
		idxStr := string(runes[i+2 : j])
		if idx, err := strconv.Atoi(idxStr); err == nil {
			if p, ok := plans[idx]; ok {
				label = p.label
			}
		}
		b.WriteString(fallbackImageText(label))
		i = j
	}
	return b.String()
}

func fallbackImageText(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return " "
	}
	return "[image: " + label + "]"
}

func blockImageText(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "[image: remote image blocked — press i to load]"
	}
	return "[image: " + label + " · remote image blocked — press i to load]"
}

func loadingImageText(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "[image: loading remote image…]"
	}
	return "[image: " + label + " · loading remote image…]"
}

// lineHasImagePlaceholder reports whether a rendered line contains raster
// placeholder cells. Such lines must not be re-styled: re-rendering them would
// strip the foreground color that carries the image id.
func lineHasImagePlaceholder(line string) bool {
	return strings.ContainsRune(line, placeholderRune)
}

// stripImagePlaceholders removes placeholder cells (the base rune and its
// combining diacritics) from display/copy text so focus, selection and the
// clipboard never see the protocol glyphs.
func stripImagePlaceholders(s string) string {
	if !strings.ContainsRune(s, placeholderRune) {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(runes); i++ {
		if runes[i] == placeholderRune {
			j := i + 1
			for j < len(runes) && unicode.Is(unicode.Mn, runes[j]) {
				j++
			}
			i = j - 1
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

// remoteFetchResult is one URL's outcome.
type remoteFetchResult struct {
	url   string
	data  []byte
	ctype string
	err   error
}

// remoteImagesLoadedMsg reports completed remote image fetches.
type remoteImagesLoadedMsg struct {
	messageID int64
	results   []remoteFetchResult
}

// fetchRemoteImagesCmd fetches a batch of remote images off the UI goroutine.
// Failures are reported per URL rather than aborting the batch.
func fetchRemoteImagesCmd(store *uiImageStore, messageID int64, urls []string) tea.Cmd {
	if store == nil || len(urls) == 0 {
		return nil
	}
	opts := store.remoteOpts
	return func() tea.Msg {
		results := make([]remoteFetchResult, 0, len(urls))
		for _, u := range urls {
			data, ctype, err := opts.Fetch(context.Background(), u)
			results = append(results, remoteFetchResult{url: u, data: data, ctype: ctype, err: err})
		}
		return remoteImagesLoadedMsg{messageID: messageID, results: results}
	}
}

// applyRemoteResults records fetch outcomes and reports whether the current view
// must be re-rendered.
func (s *uiImageStore) applyRemoteResults(msg remoteImagesLoadedMsg) bool {
	if s == nil {
		return false
	}
	for _, r := range msg.results {
		s.recordRemote(r.url, &remoteEntry{data: r.data, ctype: r.ctype, err: r.err, done: true})
	}
	return true
}

// remoteURLs returns the distinct, non-decorative remote image URLs in an HTML
// body, used when the reader presses i.
func remoteURLs(html string) []string {
	if strings.TrimSpace(html) == "" {
		return nil
	}
	// Normalize first so hidden elements and tracking pixels never trigger a
	// fetch even when the reader has consented to remote images.
	doc, err := goqueryDocument(normalizeHTMLForRendering(html))
	if err != nil {
		return nil
	}
	manifest := richmail.ExtractImages(doc)
	seen := map[string]bool{}
	var urls []string
	for _, im := range manifest.Images {
		if im.Source.Kind != richmail.SourceRemote || im.Source.URL == "" {
			continue
		}
		if im.Decorative && !hasLargeHint(im) {
			continue
		}
		if seen[im.Source.URL] {
			continue
		}
		seen[im.Source.URL] = true
		urls = append(urls, im.Source.URL)
	}
	return urls
}

// goqueryDocument parses HTML for manifest extraction.
func goqueryDocument(html string) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(html))
}

// applyViewportImages uploads the images of the currently shown content,
// replacing whatever was shown before.
func (m *Model) applyViewportImages(images []renderImage) {
	if m.images == nil {
		return
	}
	m.images.applyImages(images)
}

// clearViewportImages removes every image TideMail previously transmitted.
func (m *Model) clearViewportImages() {
	if m.images == nil {
		return
	}
	m.images.clearImages()
}

// threadImageRenderKey folds per-message remote consent and the store
// generation into an image cache key for a whole thread.
func (m Model) threadImageRenderKey(thread messageThread) string {
	if m.images == nil {
		return "off"
	}
	if !m.images.graphics() {
		return "none"
	}
	var b strings.Builder
	for _, msg := range thread.Messages {
		fmt.Fprintf(&b, "%d:%t,", msg.ID, m.images.isAllowed(msg.ID))
	}
	fmt.Fprintf(&b, "g%d", m.images.gen())
	return b.String()
}

// currentViewMessages returns the messages the reading pane is showing: every
// message in the current thread, or the single current message. Consent and
// remote-URL discovery both go through here so they always cover the same set.
func (m Model) currentViewMessages() []db.Message {
	if m.threadedMessagesEnabled() && m.messageCursor >= 0 && m.messageCursor < len(m.messageThreads) {
		return m.messageThreads[m.messageCursor].Messages
	}
	if msg := m.currentRowMessage(); msg != nil {
		return []db.Message{*msg}
	}
	return nil
}

// imageConsentMessageIDs is every message that `i` should grant remote-image
// consent to. In a thread that is the whole conversation, so a reply with its
// own remote images is not left blocked because only the representative was
// authorised.
func (m Model) imageConsentMessageIDs() []int64 {
	seen := map[int64]bool{}
	var ids []int64
	for _, msg := range m.currentViewMessages() {
		if msg.ID == 0 || seen[msg.ID] {
			continue
		}
		seen[msg.ID] = true
		ids = append(ids, msg.ID)
	}
	if m.contentMessageID != 0 && !seen[m.contentMessageID] {
		ids = append(ids, m.contentMessageID)
	}
	return ids
}

// remoteImageURLsForCurrent collects the remote image URLs of whatever the
// reading pane is showing, for the i key.
func (m Model) remoteImageURLsForCurrent() []string {
	var urls []string
	for _, msg := range m.currentViewMessages() {
		urls = append(urls, remoteURLs(msg.BodyHTML)...)
	}
	return dedupeStrings(urls)
}

func countRemoteFailures(results []remoteFetchResult) int {
	n := 0
	for _, r := range results {
		if r.err != nil {
			n++
		}
	}
	return n
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// renderedInlineKeys indexes the inline attachments actually drawn this view,
// keyed by message plus Content-ID and Content-Location. Including the message
// id means two messages that share a Content-ID cannot suppress each other's
// attachment row.
func renderedInlineKeys(images []renderImage) map[string]bool {
	if len(images) == 0 {
		return nil
	}
	keys := make(map[string]bool, len(images)*2)
	for _, im := range images {
		if k := richmail.CIDKey(im.sourceCID); k != "" {
			keys[fmt.Sprintf("%d:cid:%s", im.messageID, k)] = true
		}
		if k := richmail.LocationKey(im.sourceLoc); k != "" {
			keys[fmt.Sprintf("%d:loc:%s", im.messageID, k)] = true
		}
	}
	return keys
}

// filterRenderedInline removes inline-image attachments that were drawn inline,
// for the displayed attachment list only. An inline image that failed to
// resolve, or that was never rendered because graphics were unavailable,
// produces no renderImage and therefore stays listed. The caller keeps the full
// list for saving, so hiding a rendered image here never removes access to it.
func filterRenderedInline(atts []db.Attachment, images []renderImage) []db.Attachment {
	rendered := renderedInlineKeys(images)
	if len(rendered) == 0 {
		return atts
	}
	var out []db.Attachment
	for _, a := range atts {
		if a.IsInlineImage() {
			cid := richmail.CIDKey(a.ContentID)
			loc := richmail.LocationKey(a.ContentLocation)
			if (cid != "" && rendered[fmt.Sprintf("%d:cid:%s", a.MessageID, cid)]) ||
				(loc != "" && rendered[fmt.Sprintf("%d:loc:%s", a.MessageID, loc)]) {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}
