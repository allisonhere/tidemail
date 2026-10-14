package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Shared drawing primitives for the animated Settings scenes (About's Ocean
// night, Support's Café): colors, easing, deterministic noise, and a cell
// canvas that renders as same-style runs through lipgloss, so colors still
// downsample on terminals without truecolor.

type sceneRGB struct{ r, g, b uint8 }

func sceneHex(s string) sceneRGB {
	var c sceneRGB
	_, _ = fmt.Sscanf(s, "#%02x%02x%02x", &c.r, &c.g, &c.b)
	return c
}

func sceneFromColor(c lipgloss.Color) sceneRGB {
	r, g, b, ok := hexToRGB(c)
	if !ok {
		return sceneRGB{}
	}
	return sceneRGB{uint8(r*255 + 0.5), uint8(g*255 + 0.5), uint8(b*255 + 0.5)}
}

func (c sceneRGB) color() lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b))
}

func sceneLerp(a, b sceneRGB, t float64) sceneRGB {
	t = clamp01(t)
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return sceneRGB{m(a.r, b.r), m(a.g, b.g), m(a.b, b.b)}
}

// sceneEase is a smoothstep from 0 at frame a to 1 at frame b.
func sceneEase(f, a, b float64) float64 {
	if f <= a {
		return 0
	}
	if f >= b {
		return 1
	}
	t := (f - a) / (b - a)
	return t * t * (3 - 2*t)
}

func sceneHash(n int) uint32 {
	x := uint32(n)*0x9E3779B1 + 0x7F4A7C15
	x ^= x >> 16
	x *= 0x85EBCA6B
	x ^= x >> 13
	x *= 0xC2B2AE35
	x ^= x >> 16
	return x
}

func sceneRand(n int) float64 { return float64(sceneHash(n)%100000) / 100000 }

type sceneCell struct {
	ch     rune
	fg, bg sceneRGB
	bold   bool
}

type sceneCanvas struct {
	w, h  int
	cells []sceneCell
}

func newSceneCanvas(w, h int) *sceneCanvas {
	return &sceneCanvas{w: w, h: h, cells: make([]sceneCell, w*h)}
}

func (c *sceneCanvas) at(x, y int) *sceneCell {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return nil
	}
	return &c.cells[y*c.w+x]
}

func (c *sceneCanvas) set(x, y int, ch rune, fg, bg sceneRGB) {
	if p := c.at(x, y); p != nil {
		*p = sceneCell{ch: ch, fg: fg, bg: bg}
	}
}

// put draws a glyph over whatever background is already there.
func (c *sceneCanvas) put(x, y int, ch rune, fg sceneRGB) {
	if p := c.at(x, y); p != nil {
		p.ch, p.fg = ch, fg
	}
}

// sceneWideCont marks the second column of a double-width glyph such as ☕;
// lines() emits nothing for it.
const sceneWideCont rune = -1

// putWide draws a double-width glyph across cells x and x+1, giving both
// the same style so the pair always lands in one run.
func (c *sceneCanvas) putWide(x, y int, ch rune, fg sceneRGB) {
	p, q := c.at(x, y), c.at(x+1, y)
	if p == nil || q == nil {
		return
	}
	p.ch, p.fg = ch, fg
	*q = sceneCell{ch: sceneWideCont, fg: fg, bg: p.bg, bold: p.bold}
}

func (c *sceneCanvas) centerText(y int, s string, fg sceneRGB) int {
	return c.textAt(c.w/2, y, s, fg)
}

// textAt centers s on column cx.
func (c *sceneCanvas) textAt(cx, y int, s string, fg sceneRGB) int {
	x := cx - len([]rune(s))/2
	for i, r := range []rune(s) {
		c.put(x+i, y, r, fg)
	}
	return x
}

// plot sets one half-block pixel: each cell is two pixels tall, drawn as ▀
// with the top pixel in the foreground and the bottom one in the background.
func (c *sceneCanvas) plot(x, py int, col sceneRGB) {
	p := c.at(x, py/2)
	if p == nil {
		return
	}
	if p.ch != '▀' {
		p.ch, p.fg = '▀', p.bg
	}
	if py%2 == 0 {
		p.fg = col
	} else {
		p.bg = col
	}
}

// lines renders each row as runs of identically styled cells, so lipgloss
// can still downsample the colors for terminals without truecolor.
func (c *sceneCanvas) lines() []string {
	out := make([]string, c.h)
	var run strings.Builder
	for y := range c.h {
		var b strings.Builder
		row := c.cells[y*c.w : (y+1)*c.w]
		start := 0
		for x := 1; x <= c.w; x++ {
			if x < c.w && row[x].fg == row[start].fg && row[x].bg == row[start].bg && row[x].bold == row[start].bold {
				continue
			}
			run.Reset()
			for _, cl := range row[start:x] {
				switch cl.ch {
				case sceneWideCont:
				case 0:
					run.WriteByte(' ')
				default:
					run.WriteRune(cl.ch)
				}
			}
			first := row[start]
			b.WriteString(lipgloss.NewStyle().
				Foreground(first.fg.color()).
				Background(first.bg.color()).
				Bold(first.bold).
				Render(run.String()))
			start = x
		}
		out[y] = b.String()
	}
	return out
}
