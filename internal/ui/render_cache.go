package ui

import "sync"

// Showing a message costs tens of milliseconds — a 21KB HTML mail measured
// ~29ms to render, roughly half of it inside glamour — and it used to run on
// every keystroke, once per message in the thread. Nothing about the result
// changes between those renders, so it is cached at two levels: the rendered
// body of one message, and the finished viewport content for a whole message
// or thread.

// boundedCache is a map that forgets its oldest entries. It is held by pointer
// on Model so that writes through Model's value receivers persist, and it is
// mutex-guarded because it is reachable from package-level render helpers.
type boundedCache[K comparable, V any] struct {
	mu      sync.Mutex
	entries map[K]V
	order   []K
	limit   int
}

func newBoundedCache[K comparable, V any](limit int) *boundedCache[K, V] {
	if limit <= 0 {
		limit = 1
	}
	return &boundedCache[K, V]{
		entries: make(map[K]V, limit),
		order:   make([]K, 0, limit),
		limit:   limit,
	}
}

func (c *boundedCache[K, V]) get(key K) (V, bool) {
	var zero V
	if c == nil {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	if !ok {
		return zero, false
	}
	return v, true
}

func (c *boundedCache[K, V]) put(key K, val V) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		if len(c.order) >= c.limit {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
		c.order = append(c.order, key)
	}
	c.entries[key] = val
}

// clear drops every entry, for changes the key does not capture — a theme
// edited in place, or a config toggle that feeds the renderer.
func (c *boundedCache[K, V]) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
	c.order = c.order[:0]
}

func (c *boundedCache[K, V]) len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// bodyCacheKey identifies a rendered message body by everything the render
// depends on.
type bodyCacheKey struct {
	messageID   int64
	width       int
	theme       string
	plainUI     bool
	filterLinks bool
	// fingerprint distinguishes two versions of the same message, so a body
	// replaced by a re-fetch cannot serve a stale render. Mail bodies do not
	// change in place once stored, so their length tells versions apart
	// without hashing the whole string on every lookup.
	fingerprint int
}

// viewportCacheKey identifies the finished content of the reading pane. The
// key covers the message or thread shown plus every setting that changes how
// it is laid out.
type viewportCacheKey struct {
	// id is the message ID, or the thread key prefixed so the two namespaces
	// cannot collide.
	id              string
	width           int
	paneWidth       int
	theme           string
	plainUI         bool
	filterLinks     bool
	showHeaders     bool
	quotesCollapsed bool
	actionableLinks bool
	fingerprint     int
}

// viewportContent is everything setViewportMessage/setViewportThread derives
// from one render: the content itself and the three line-indexed views of it
// that used to be recomputed with a full ansi.Strip each.
type viewportContent struct {
	content   string
	lines     []string
	lineLinks []string
	focusable []bool
}

const (
	// defaultBodyCacheLimit caps the body cache at a few screenfuls of
	// threads. Rendered bodies run ~20KB each.
	defaultBodyCacheLimit = 64
	// defaultViewportCacheLimit is smaller: each entry holds the finished
	// content plus its line index, so entries are larger and the working set
	// is whatever the reader is scrolling through right now.
	defaultViewportCacheLimit = 24
)
