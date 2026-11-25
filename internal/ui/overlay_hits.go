package ui

import (
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// Overlay hit map. While renderOverlay builds a modal's box, renderers wrap
// clickable pieces with markHit. The markers are zero-width CSI sequences, so
// lipgloss width math, padding and truncation pass them through untouched.
// renderOverlay then strips them from the finished box in one pass over the
// box alone (never the full frame) and records where each piece landed.
// Mouse handling resolves a click against those rectangles, so it always
// follows the real rendered layout instead of a hand-kept copy of it.

// overlayHit is one marked piece, in cells relative to the box's top-left.
type overlayHit struct {
	id     string
	row    int
	x0, x1 int // half-open column range [x0, x1)
}

// overlayHitMap is the hit map for the overlay drawn by the latest View. It
// is pointer-owned by Model so the value-receiver View can refresh it.
type overlayHitMap struct {
	overlay          overlayMode
	width, height    int // screen size the map was recorded for
	originX, originY int // the box's top-left on screen
	boxW, boxH       int
	hits             []overlayHit
}

// hitMarking is true only while renderOverlay builds a box with mouse support
// on. Rendering runs on Bubble Tea's event loop, so a package flag is enough;
// any other caller of the shared render helpers gets plain, unmarked output.
var hitMarking bool

var hitRegistry = struct {
	sync.Mutex
	ids   map[string]int
	names []string
}{ids: map[string]int{}}

func hitNumber(id string) int {
	hitRegistry.Lock()
	defer hitRegistry.Unlock()
	if n, ok := hitRegistry.ids[id]; ok {
		return n
	}
	n := len(hitRegistry.names)
	hitRegistry.ids[id] = n
	hitRegistry.names = append(hitRegistry.names, id)
	return n
}

func hitName(n int) (string, bool) {
	hitRegistry.Lock()
	defer hitRegistry.Unlock()
	if n < 0 || n >= len(hitRegistry.names) {
		return "", false
	}
	return hitRegistry.names[n], true
}

const (
	hitOpenParam  = "7791"
	hitCloseParam = "7792"
)

// markHit wraps s so the cells it occupies resolve to id. It is a no-op
// unless renderOverlay is building a box with mouse support on.
func markHit(id, s string) string {
	if !hitMarking || s == "" {
		return s
	}
	n := strconv.Itoa(hitNumber(id))
	return "\x1b[" + hitOpenParam + ";" + n + "z" + s + "\x1b[" + hitCloseParam + ";" + n + "z"
}

// parseHitMarker reports whether s starts with a hit marker, returning
// whether it opens or closes, the id number and the marker's byte length.
func parseHitMarker(s string) (open bool, n, size int, ok bool) {
	if !strings.HasPrefix(s, "\x1b[") {
		return false, 0, 0, false
	}
	rest := s[2:]
	switch {
	case strings.HasPrefix(rest, hitOpenParam+";"):
		open = true
	case strings.HasPrefix(rest, hitCloseParam+";"):
	default:
		return false, 0, 0, false
	}
	rest = rest[len(hitOpenParam)+1:]
	end := strings.IndexByte(rest, 'z')
	if end <= 0 {
		return false, 0, 0, false
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return false, 0, 0, false
	}
	return open, n, 2 + len(hitOpenParam) + 1 + end + 1, true
}

// extractHits strips hit markers from box and returns the clean box and the
// recorded hits. A piece that wraps onto later lines records one hit per line.
// It makes one pass and one allocation for the clean box, since it runs on
// every frame of an open overlay (compose drags send many).
func extractHits(box string) (string, []overlayHit) {
	if !strings.Contains(box, "\x1b["+hitOpenParam+";") {
		return box, nil
	}
	var clean strings.Builder
	clean.Grow(len(box))
	var hits []overlayHit
	type openHit struct{ n, start int }
	var open []openHit // in opening order
	row, col, seg := 0, 0, 0
	// Columns only matter on lines with a marker or a piece still open from
	// an earlier line; measuring every chunk of a large box (the compose body)
	// would dominate the cost.
	hasMarker := func(from int) bool {
		line := box[from:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		return strings.Contains(line, "\x1b["+hitOpenParam+";") || strings.Contains(line, "\x1b["+hitCloseParam+";")
	}
	measure := hasMarker(0)
	flush := func(end int) {
		chunk := box[seg:end]
		clean.WriteString(chunk)
		if measure || len(open) > 0 {
			col += ansi.StringWidth(chunk)
		}
	}
	closeLine := func() {
		for _, o := range open {
			if name, known := hitName(o.n); known && col > o.start {
				hits = append(hits, overlayHit{id: name, row: row, x0: o.start, x1: col})
			}
		}
	}
	for i := 0; i < len(box); {
		switch box[i] {
		case '\n':
			flush(i)
			closeLine()
			clean.WriteByte('\n')
			row, col = row+1, 0
			for j := range open {
				open[j].start = 0 // pieces still open continue from column 0
			}
			i++
			seg = i
			measure = hasMarker(i)
		case 0x1b:
			isOpen, n, size, ok := parseHitMarker(box[i:])
			if !ok {
				i++
				continue
			}
			flush(i)
			if isOpen {
				open = append(open, openHit{n: n, start: col})
			} else {
				for j := len(open) - 1; j >= 0; j-- {
					if open[j].n != n {
						continue
					}
					if name, known := hitName(n); known && col > open[j].start {
						hits = append(hits, overlayHit{id: name, row: row, x0: open[j].start, x1: col})
					}
					open = append(open[:j], open[j+1:]...)
					break
				}
			}
			i += size
			seg = i
		default:
			i++
		}
	}
	flush(len(box))
	closeLine()
	return clean.String(), hits
}

// record stores the hit map for a box drawn at (x, y) on a width x height
// screen while overlay o is open. It takes plain values rather than a Model
// receiver so recording never copies the (large) Model.
func (hm *overlayHitMap) record(o overlayMode, width, height int, hits []overlayHit, x, y, boxW, boxH int) {
	if hm == nil {
		return
	}
	*hm = overlayHitMap{
		overlay: o, width: width, height: height,
		originX: x, originY: y, boxW: boxW, boxH: boxH,
		hits: hits,
	}
}

// overlayHitAt returns the innermost marked piece under screen cell (x, y)
// for the overlay currently open. inBox reports whether the cell is inside
// the box at all.
func (m Model) overlayHitAt(x, y int) (hit overlayHit, found, inBox bool) {
	hm := m.overlayHits
	if hm == nil || hm.overlay != m.overlay || hm.width != m.width || hm.height != m.height {
		return overlayHit{}, false, false
	}
	rx, ry := x-hm.originX, y-hm.originY
	inBox = rx >= 0 && ry >= 0 && rx < hm.boxW && ry < hm.boxH
	// Later hits were opened later on the line, so they are nested inside
	// earlier ones; prefer the narrowest match.
	best := -1
	for i, h := range hm.hits {
		if h.row == ry && rx >= h.x0 && rx < h.x1 {
			if best < 0 || h.x1-h.x0 <= hm.hits[best].x1-hm.hits[best].x0 {
				best = i
			}
		}
	}
	if best < 0 {
		return overlayHit{}, false, inBox
	}
	return hm.hits[best], true, inBox
}

// boxSize returns a rendered box's width and height in cells. Overlay boxes
// are rectangular (every renderer pads its rows to the box width), so the
// first line's width is the box's: measuring every line on every frame is
// measurable garbage on the compose path.
func boxSize(box string) (int, int) {
	first, _, _ := strings.Cut(box, "\n")
	return ansi.StringWidth(first), strings.Count(box, "\n") + 1
}
