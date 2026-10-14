// Command aboutpreview is a throwaway side-by-side preview of candidate
// animated About and Support screens for TideMail. Nothing in the app uses it.
//
//	go run ./cmd/aboutpreview
//
// 1-6 or ←/→ switch scenes, r replays the intro, q quits.
package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const version = "v1.3.1"

// ── color + canvas ─────────────────────────────────────────────────────────

type rgb struct{ r, g, b uint8 }

func hex(s string) rgb {
	var c rgb
	_, _ = fmt.Sscanf(s, "#%02x%02x%02x", &c.r, &c.g, &c.b)
	return c
}

func clamp01(t float64) float64 { return math.Max(0, math.Min(1, t)) }

func lerp(a, b rgb, t float64) rgb {
	t = clamp01(t)
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return rgb{m(a.r, b.r), m(a.g, b.g), m(a.b, b.b)}
}

// ease is a smoothstep from 0 at frame a to 1 at frame b.
func ease(f, a, b float64) float64 {
	if f <= a {
		return 0
	}
	if f >= b {
		return 1
	}
	t := (f - a) / (b - a)
	return t * t * (3 - 2*t)
}

func hash(n int) uint32 {
	x := uint32(n)*0x9E3779B1 + 0x7F4A7C15
	x ^= x >> 16
	x *= 0x85EBCA6B
	x ^= x >> 13
	x *= 0xC2B2AE35
	x ^= x >> 16
	return x
}

func frand(n int) float64 { return float64(hash(n)%100000) / 100000 }

// wideCont marks the second column of a double-width glyph such as ☕.
const wideCont rune = -1

type cell struct {
	ch     rune
	fg, bg rgb
}

// canvas is a grid of cells rendered straight to truecolor ANSI, emitting a
// new escape only when the colors change, so each frame stays small.
type canvas struct {
	w, h  int
	cells []cell
}

func newCanvas(w, h int) *canvas { return &canvas{w: w, h: h, cells: make([]cell, w*h)} }

func (c *canvas) at(x, y int) *cell {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return nil
	}
	return &c.cells[y*c.w+x]
}

func (c *canvas) set(x, y int, ch rune, fg, bg rgb) {
	if p := c.at(x, y); p != nil {
		*p = cell{ch, fg, bg}
	}
}

// put draws a glyph, keeping whatever background is already there.
func (c *canvas) put(x, y int, ch rune, fg rgb) {
	if p := c.at(x, y); p != nil {
		p.ch, p.fg = ch, fg
	}
}

// putWide draws a double-width glyph across two cells.
func (c *canvas) putWide(x, y int, ch rune, fg rgb) {
	if c.at(x, y) == nil || c.at(x+1, y) == nil {
		return
	}
	c.put(x, y, ch, fg)
	c.put(x+1, y, wideCont, fg)
}

func (c *canvas) text(x, y int, s string, fg rgb) {
	for i, r := range []rune(s) {
		c.put(x+i, y, r, fg)
	}
}

func (c *canvas) centerAt(cx, y int, s string, fg rgb) int {
	x := cx - len([]rune(s))/2
	c.text(x, y, s, fg)
	return x
}

func (c *canvas) centerText(y int, s string, fg rgb) int { return c.centerAt(c.w/2, y, s, fg) }

// plot sets one half-block "pixel": each cell is two pixels tall, drawn as
// ▀ with the top pixel in the foreground and the bottom in the background.
func (c *canvas) plot(x, py int, col rgb) {
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

func (c *canvas) String() string {
	var b strings.Builder
	for y := 0; y < c.h; y++ {
		var lfg, lbg rgb
		first := true
		for x := 0; x < c.w; x++ {
			cl := c.cells[y*c.w+x]
			if cl.ch == wideCont {
				continue
			}
			if first || cl.fg != lfg || cl.bg != lbg {
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm", cl.fg.r, cl.fg.g, cl.fg.b, cl.bg.r, cl.bg.g, cl.bg.b)
				lfg, lbg, first = cl.fg, cl.bg, false
			}
			ch := cl.ch
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
		b.WriteString("\x1b[0m")
		if y < c.h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// ── wordmark ───────────────────────────────────────────────────────────────

var font = map[rune][5]string{
	'T': {"█████", "  █  ", "  █  ", "  █  ", "  █  "},
	'I': {"███", " █ ", " █ ", " █ ", "███"},
	'D': {"████ ", "█   █", "█   █", "█   █", "████ "},
	'E': {"█████", "█    ", "████ ", "█    ", "█████"},
	'M': {"█   █", "██ ██", "█ █ █", "█   █", "█   █"},
	'A': {" ███ ", "█   █", "█████", "█   █", "█   █"},
	'L': {"█    ", "█    ", "█    ", "█    ", "█████"},
}

func wordBitmap(s string) [][]bool {
	rows := make([][]bool, 5)
	for i, r := range s {
		if i > 0 {
			for y := range rows {
				rows[y] = append(rows[y], false)
			}
		}
		g := font[r]
		for y := range 5 {
			for _, ch := range g[y] {
				rows[y] = append(rows[y], ch == '█')
			}
		}
	}
	return rows
}

var (
	wordmark = wordBitmap("TIDEMAIL")
	wordW    = len(wordmark[0])
)

// ── shared night scenery ───────────────────────────────────────────────────

var (
	skyTop = hex("#02040d")
	skyBot = hex("#0d2048")
)

func skyAt(y, skyRows int) rgb { return lerp(skyTop, skyBot, float64(y)/float64(max(1, skyRows-1))) }

// nightSky fills the canvas with the sky gradient and twinkling stars,
// keeping stars out of the content rectangle.
func nightSky(c *canvas, ff float64, skyRows int, inContent func(x, y int) bool) {
	for y := range c.h {
		bg := skyAt(min(y, skyRows-1), skyRows)
		for x := range c.w {
			c.set(x, y, ' ', bg, bg)
		}
	}
	for i := range c.w * skyRows / 16 {
		x := int(hash(i*3+1) % uint32(c.w))
		y := int(hash(i*3+2) % uint32(max(1, skyRows-1)))
		if inContent(x, y) {
			continue
		}
		start := frand(i) * 25
		b := ease(ff, start, start+12) * (0.5 + 0.5*math.Sin(ff*(0.06+frand(i+11)*0.14)+frand(i+7)*6.28))
		if b < 0.08 {
			continue
		}
		ch := '·'
		if s := frand(i + 5); s > 0.94 {
			ch = '✦'
		} else if s > 0.82 {
			ch = '+'
		}
		if p := c.at(x, y); p != nil {
			c.put(x, y, ch, lerp(p.bg, hex("#eef4ff"), b))
		}
	}
}

func moon(c *canvas, ff float64, cx, cyPx int) {
	m := ease(ff, 8, 35)
	if m <= 0 {
		return
	}
	mx, my := float64(cx), float64(cyPx)
	for y := 0; y < 8; y++ {
		for x := cx - 9; x < cx+9; x++ {
			d := math.Hypot(float64(x)-mx, float64(y*2+1)-my)
			if p := c.at(x, y); p != nil && d < 9 {
				p.bg = lerp(p.bg, hex("#1f3a78"), (1-d/9)*0.55*m)
			}
		}
	}
	for py := cyPx - 4; py <= cyPx+4; py++ {
		for x := cx - 4; x <= cx+4; x++ {
			in := math.Hypot(float64(x)-mx, float64(py)-my) < 3.6
			bite := math.Hypot(float64(x)-mx-1.8, float64(py)-my+1.2) < 3.1
			if p := c.at(x, py/2); p != nil && in && !bite {
				c.plot(x, py, lerp(p.bg, hex("#f7edcb"), m))
			}
		}
	}
}

func shootingStar(c *canvas, f, skyRows int, inContent func(x, y int) bool) {
	if f < 60 {
		return
	}
	n, cyc := (f-60)/150, (f-60)%150
	if cyc >= 16 {
		return
	}
	sx := c.w/6 + int(frand(n*13+1)*float64(c.w)*0.5)
	sy := 1 + int(frand(n*13+2)*float64(max(1, skyRows/4)))
	for k := 0; k < 8; k++ {
		pos := cyc - k
		x, y := sx+pos*3, sy+pos
		if pos < 0 || y >= skyRows-1 || inContent(x, y) {
			continue
		}
		if p := c.at(x, y); p != nil {
			ch := '·'
			if k == 0 {
				ch = '✦'
			}
			c.put(x, y, ch, lerp(p.bg, hex("#ffffff"), 1-float64(k)/8))
		}
	}
}

// waveHeight is layer l's water height, in rows, at column x.
func waveHeight(ff float64, x, l, rows int, tide float64) float64 {
	t := ff * 0.11
	r := float64(rows) * tide
	fx := float64(x)
	switch l {
	case 0:
		return r * (0.86 + 0.08*math.Sin(fx*0.11+t*0.7) + 0.04*math.Sin(fx*0.31-t*1.3))
	case 1:
		return r * (0.62 + 0.11*math.Sin(fx*0.17-t*0.9+1.7) + 0.05*math.Sin(fx*0.41+t*1.5))
	default:
		return r * (0.38 + 0.14*math.Sin(fx*0.23+t*1.2+3.1) + 0.06*math.Sin(fx*0.53-t*1.9))
	}
}

func oceanWaves(c *canvas, ff float64, top, rows int, tide float64) {
	layer := [3]rgb{hex("#0b3556"), hex("#11658a"), hex("#1aa0c8")}
	deep, foam := hex("#04121f"), hex("#e4fbff")
	blocks := []rune(" ▁▂▃▄▅▆▇█")
	for x := range c.w {
		hs := [3]float64{waveHeight(ff, x, 0, rows, tide), waveHeight(ff, x, 1, rows, tide), waveHeight(ff, x, 2, rows, tide)}
		for r := range rows {
			y := top + r
			p := c.at(x, y)
			if p == nil {
				continue
			}
			fromBottom := float64(rows - r)
			shade := func(l int) rgb {
				return lerp(layer[l], deep, clamp01((hs[l]-fromBottom)/float64(rows))*0.75)
			}
			front, fill := -1, 0.0
			for l := 2; l >= 0; l-- {
				if fl := hs[l] - (fromBottom - 1); fl > 0 {
					front, fill = l, fl
					break
				}
			}
			if front < 0 {
				continue
			}
			fg := shade(front)
			if fill >= 1 {
				c.set(x, y, '█', fg, fg)
				continue
			}
			fg = lerp(fg, foam, [3]float64{0.15, 0.3, 0.6}[front])
			bg := p.bg
			for l := front - 1; l >= 0; l-- {
				if hs[l]-(fromBottom-1) >= 1 {
					bg = shade(l)
					break
				}
			}
			c.set(x, y, blocks[max(1, int(fill*8))], fg, bg)
		}
	}
}

// ── shared support content ─────────────────────────────────────────────────

type supportPalette struct {
	heading, text, muted, edge, label, accent, accentFg, heartLo, heartHi, steam rgb
}

var nightSupport = supportPalette{
	heading: hex("#7fe3ff"), text: hex("#dbe6f8"), muted: hex("#8ea2c6"),
	edge: hex("#3d6aa8"), label: hex("#d2def5"), accent: hex("#4fd8ff"), accentFg: hex("#04121f"),
	heartLo: hex("#b0174f"), heartHi: hex("#ff6b8b"), steam: hex("#c9d6ea"),
}

const supportBlockH = 15

// supportBlock draws the Support page's content centered on cx: heading,
// pitch, the Ko-fi line with a steaming cup, Open/Copy buttons (the first
// shown focused), the thank-you and a pulsing heart.
func supportBlock(c *canvas, cx, y0 int, ff float64, pal supportPalette, fade float64, bgAt func(x, y int) rgb) {
	tint := func(x, y int, col rgb) rgb { return lerp(bgAt(x, y), col, fade) }
	line := func(y int, s string, col rgb) int {
		x := cx - len([]rune(s))/2
		for i, r := range []rune(s) {
			c.put(x+i, y, r, tint(x+i, y, col))
		}
		return x
	}
	line(y0, "Support Tidemail", pal.heading)
	line(y0+2, "Tidemail is free and open source.", pal.text)
	line(y0+3, "If it makes email a little better for you,", pal.muted)
	line(y0+4, "you can support continued development.", pal.muted)

	// "☕ Ko-fi · one-time or recurring", with steam curling off the cup.
	kofi := "   Ko-fi · one-time or recurring"
	kx := line(y0+6, kofi, pal.heading)
	c.putWide(kx, y0+6, '☕', tint(kx, y0+6, hex("#f2e6d0")))
	steam(c, kx, y0+5, ff, pal.steam, fade, 1) // the blank row above the cup

	buttons(c, cx, y0+8, []string{"Support on Ko-fi", "Copy link"}, 0, pal, fade)
	line(y0+12, "Thank you for supporting Tidemail -allie", pal.muted)
	pulse := 0.5 + 0.5*math.Sin(ff*0.22)
	line(y0+13, "❤", lerp(pal.heartLo, pal.heartHi, pulse))
}

// steam curls a couple of wisps up from (x, y), fading as they rise.
func steam(c *canvas, x, y int, ff float64, col rgb, fade float64, height int) {
	for k := range 2 {
		for s := range height {
			px := x + k + int(math.Round(math.Sin(ff*0.3+float64(s)*1.3+float64(k)*2.4)*0.8))
			py := y - s
			p := c.at(px, py)
			if p == nil || (p.ch != ' ' && p.ch != 0 && p.ch != '·' && p.ch != '+') {
				continue
			}
			ch := []rune("~∽∿")[(int(ff/3)+s+k)%3]
			c.put(px, py, ch, lerp(p.bg, col, fade*(0.75-float64(s)*0.3)))
		}
	}
}

// buttons draws rounded buttons side by side, centered on cx.
func buttons(c *canvas, cx, y int, labels []string, focused int, pal supportPalette, fade float64) {
	total := -2
	for _, l := range labels {
		total += len([]rune(l)) + 6
	}
	x := cx - total/2
	for i, l := range labels {
		bw := len([]rune(l)) + 4
		on := i == focused
		for dy := range 3 {
			for dx := range bw {
				p := c.at(x+dx, y+dy)
				if p == nil {
					continue
				}
				edge := lerp(p.bg, pal.edge, fade)
				if on {
					edge = lerp(p.bg, pal.accent, fade)
				}
				var ch rune
				switch {
				case dy == 0 && dx == 0:
					ch = '╭'
				case dy == 0 && dx == bw-1:
					ch = '╮'
				case dy == 2 && dx == 0:
					ch = '╰'
				case dy == 2 && dx == bw-1:
					ch = '╯'
				case dy == 0 || dy == 2:
					ch = '─'
				case dx == 0 || dx == bw-1:
					ch = '│'
				}
				if ch != 0 {
					c.put(x+dx, y+dy, ch, edge)
				} else if on {
					p.ch, p.bg = ' ', lerp(p.bg, pal.accent, fade)
				} else {
					p.ch = ' '
				}
			}
		}
		for j, r := range []rune(l) {
			p := c.at(x+2+j, y+1)
			if p == nil {
				continue
			}
			if on {
				p.ch, p.fg = r, lerp(p.bg, pal.accentFg, fade)
			} else {
				p.ch, p.fg = r, lerp(p.bg, pal.label, fade)
			}
		}
		x += bw + 2
	}
}

// ── 1. Ocean night ─────────────────────────────────────────────────────────

func oceanNight(c *canvas, f int) {
	ff := float64(f)
	waveRows := max(4, c.h/4)
	waveTop := c.h - waveRows
	y0 := max(0, (waveTop-12)/2)
	contentW := max(wordW, 48)
	inContent := func(x, y int) bool {
		return y >= y0-1 && y <= y0+12 && x >= (c.w-contentW)/2-3 && x <= (c.w+contentW)/2+3
	}
	nightSky(c, ff, waveTop, inContent)
	if c.w > 50 {
		moon(c, ff, c.w-11, 6)
	}
	shootingStar(c, f, waveTop, inContent)
	sky := func(y int) rgb { return skyAt(min(y, waveTop-1), waveTop) }

	if c.w >= wordW+4 {
		x0 := (c.w - wordW) / 2
		reveal := ease(ff, 12, 46) * float64(wordW+6)
		sweep := math.Mod((ff-46)*0.9, float64(wordW+70)) - 30
		for row := range 5 { // drop shadow first
			for col := range wordW {
				if wordmark[row][col] && float64(col) <= reveal {
					if p := c.at(x0+col+1, y0+row+1); p != nil {
						c.put(x0+col+1, y0+row+1, '█', lerp(p.bg, hex("#000000"), 0.55))
					}
				}
			}
		}
		for row := range 5 {
			for col := range wordW {
				if !wordmark[row][col] || float64(col) > reveal {
					continue
				}
				col0 := lerp(hex("#4fd8ff"), hex("#a07bff"), float64(col)/float64(wordW-1))
				col0 = lerp(col0, hex("#ffffff"), 0.14*float64(4-row)/4)
				if ff > 46 {
					if d := math.Abs(float64(col+row*2) - sweep); d < 5 {
						col0 = lerp(col0, hex("#ffffff"), (1-d/5)*0.85)
					}
				}
				if edge := reveal - float64(col); edge < 3 {
					col0 = lerp(col0, hex("#ffffff"), 1-edge/3)
				}
				c.put(x0+col, y0+row, '█', col0)
			}
		}
	} else {
		c.centerText(y0+2, "T I D E M A I L", lerp(sky(y0+2), hex("#4fd8ff"), ease(ff, 12, 30)))
	}

	fade := ease(ff, 40, 60)
	ty := y0 + 7
	c.centerText(ty, "your mail, your rules", lerp(sky(ty), hex("#a9bde0"), fade))
	vy := ty + 2
	line := version + "   ❤"
	vx := c.centerText(vy, line, lerp(sky(vy), hex("#6f83a8"), fade))
	pulse := 0.5 + 0.5*math.Sin(ff*0.22)
	c.put(vx+len([]rune(line))-1, vy, '❤', lerp(sky(vy), lerp(hex("#b0174f"), hex("#ff6b8b"), pulse), fade))
	ly := vy + 2
	c.centerText(ly, "Support Tidemail   ·   Repository   ·   Issues", lerp(sky(ly), hex("#d2def5"), ease(ff, 52, 72)))

	oceanWaves(c, ff, waveTop, waveRows, ease(ff, 0, 40))
}

// ── 2. Retro boot ──────────────────────────────────────────────────────────

var bootLines = []string{
	"TIDEMAIL BIOS " + version + "        (c) the tide",
	"",
	"> probing terminal .............. ok",
	"> loading tide engine ........... ok",
	"> mounting mailboxes ............ ok",
	"> calibrating waves ............. ok",
}

func retroBoot(c *canvas, f int) {
	ff := float64(f)
	green, dim, bright := hex("#3dff7f"), hex("#0f5a28"), hex("#c8ffd9")
	scan := f % (c.h + 25)
	for y := range c.h {
		bg := hex("#010803")
		if y%2 == 1 {
			bg = hex("#03120a") // faint scanlines
		}
		if d := scan - y; d >= 0 && d < 4 {
			bg = lerp(bg, hex("#0b4020"), (1-float64(d)/4)*0.7) // rolling refresh band
		}
		for x := range c.w {
			c.set(x, y, ' ', green, bg)
		}
	}
	flicker := 0.85 + 0.15*math.Sin(ff*0.9)*math.Sin(ff*0.37)

	x0 := max(1, (c.w-46)/2)
	remaining, total := f*3, 0
	cursorX, cursorY := x0, 1
	for i, ln := range bootLines {
		rs := []rune(ln)
		total += len(rs)
		n := min(len(rs), max(0, remaining))
		for j := 0; j < n; j++ {
			col := green
			if i == 0 || (strings.HasSuffix(ln, "ok") && j >= len(rs)-2) {
				col = bright
			}
			c.put(x0+j, 1+i, rs[j], lerp(dim, col, flicker))
		}
		if remaining >= 0 {
			cursorX, cursorY = x0+n, 1+i
		}
		remaining -= len(rs)
	}
	bootDone := total/3 + 6

	wy := 1 + len(bootLines) + 1
	if f >= bootDone && c.w >= wordW+4 {
		g := f - bootDone
		wx := (c.w - wordW) / 2
		for pass, off := range []int{-1, 1, 0} {
			if g >= 14 && pass < 2 {
				continue // chromatic fringes only while glitching
			}
			for row := range 5 {
				shift := 0
				if g < 14 && hash(row*131+f)%3 == 0 {
					shift = int(hash(row*7+f)%7) - 3
				}
				for col := range wordW {
					if !wordmark[row][col] {
						continue
					}
					ch, fg := '█', lerp(green, bright, 0.25*flicker)
					switch pass {
					case 0:
						fg = hex("#ff2d6f")
					case 1:
						fg = hex("#2df5ff")
					}
					if g < 14 && hash(col*17+row*5+f)%7 == 0 {
						ch = '▓'
					}
					c.put(wx+col+off+shift, wy+row, ch, fg)
				}
			}
		}
	}

	// Oscilloscope with a bit of phosphor persistence behind the live trace.
	sy, boxH := wy+6, 7
	boxW := min(c.w-4, 56)
	scopeOn := f >= bootDone+10 && sy+boxH+3 < c.h
	if scopeOn {
		bx := (c.w - boxW) / 2
		frame(c, bx, sy, boxW, boxH, dim)
		for x := bx + 4; x < bx+boxW-1; x += 6 {
			for y := sy + 1; y < sy+boxH-1; y++ {
				c.put(x, y, '·', hex("#0a3a18"))
			}
		}
		trace := func(t float64) func(int) int {
			return func(col int) int {
				fx := float64(col)
				v := 0.55*math.Sin(fx*0.22-t*0.35) + 0.3*math.Sin(fx*0.07+t*0.12) + 0.1*math.Sin(fx*0.9-t*0.8)
				return max(0, min(9, int(math.Round(4.5-v*4.4))))
			}
		}
		ghost, live := trace(ff-3), trace(ff)
		for col := 1; col < boxW-1; col++ {
			c.plot(bx+col, (sy+1)*2+ghost(col), hex("#1c7a3a"))
		}
		for col := 1; col < boxW-1; col++ {
			c.plot(bx+col, (sy+1)*2+live(col), bright)
		}
	}

	ty := sy + boxH + 1
	if !scopeOn {
		ty = wy + 7
	}
	tag := []rune("your mail, your rules.")
	if start := bootDone + 16; f >= start && ty < c.h {
		n := min(len(tag), (f-start)/2)
		tx := (c.w - len(tag)) / 2
		c.text(tx, ty, string(tag[:n]), lerp(dim, green, flicker))
		cursorX, cursorY = tx+n, ty
		if n == len(tag) && ty+2 < c.h {
			c.centerText(ty+2, "[ Support Tidemail ]  [ Repository ]  [ Issues ]", lerp(dim, green, 0.7))
		}
	}
	if (f/4)%2 == 0 {
		c.put(cursorX, cursorY, '█', bright)
	}
}

func frame(c *canvas, x0, y0, w, h int, fg rgb) {
	for x := x0; x < x0+w; x++ {
		c.put(x, y0, '─', fg)
		c.put(x, y0+h-1, '─', fg)
	}
	for y := y0; y < y0+h; y++ {
		c.put(x0, y, '│', fg)
		c.put(x0+w-1, y, '│', fg)
	}
	c.put(x0, y0, '╭', fg)
	c.put(x0+w-1, y0, '╮', fg)
	c.put(x0, y0+h-1, '╰', fg)
	c.put(x0+w-1, y0+h-1, '╯', fg)
}

// ── 3. Plasma ──────────────────────────────────────────────────────────────

func plasma(c *canvas, f int) {
	ff := float64(f)
	t := ff * 0.07
	fade := ease(ff, 0, 25)
	cx, cy := float64(c.w)/2, float64(c.h)
	// Inigo Quilez cosine palette, dimmed so the card text stays readable.
	pal := func(v float64) rgb {
		ch := func(o float64) uint8 { return uint8((0.5 + 0.5*math.Cos(6.2832*(v+o))) * 230 * fade) }
		return rgb{ch(0), ch(0.33), ch(0.67)}
	}
	val := func(x, py float64) float64 {
		v := math.Sin(x*0.08+t) + math.Sin(py*0.09-t*0.8) + math.Sin(x*0.05+py*0.07+t*0.6) +
			math.Sin(math.Hypot(x-cx, py-cy)*0.11-t*1.3)
		return v/8 + 0.5 + t*0.05
	}
	for y := range c.h {
		for x := range c.w {
			c.set(x, y, '▀', pal(val(float64(x), float64(2*y))), pal(val(float64(x), float64(2*y+1))))
		}
	}

	big := c.w >= wordW+10
	cardW := wordW + 8
	wordRows := 5
	if !big {
		cardW, wordRows = min(c.w-2, 34), 1
	}
	cardH := 2 + 1 + wordRows + 1 + 1 + 1 + 1 + 1 + 1
	x0, y0 := (c.w-cardW)/2, (c.h-cardH)/2
	// Frosted glass: the plasma still glows faintly through the card.
	for y := y0; y < y0+cardH; y++ {
		for x := x0; x < x0+cardW; x++ {
			p := c.at(x, y)
			if p == nil {
				continue
			}
			avg := lerp(p.fg, p.bg, 0.5)
			glass := lerp(avg, hex("#07060d"), 0.84)
			edge := y == y0 || y == y0+cardH-1 || x == x0 || x == x0+cardW-1
			ch := ' '
			if edge {
				switch {
				case y == y0 && x == x0:
					ch = '╭'
				case y == y0 && x == x0+cardW-1:
					ch = '╮'
				case y == y0+cardH-1 && x == x0:
					ch = '╰'
				case y == y0+cardH-1 && x == x0+cardW-1:
					ch = '╯'
				case y == y0 || y == y0+cardH-1:
					ch = '─'
				default:
					ch = '│'
				}
			}
			c.set(x, y, ch, lerp(avg, hex("#ffffff"), 0.55), glass)
		}
	}

	wy := y0 + 2
	if big {
		wx := (c.w - wordW) / 2
		for row := range 5 {
			for col := range wordW {
				if wordmark[row][col] {
					c.put(wx+col, wy+row, '█', lerp(pal(float64(col)/float64(wordW)*0.8+t*0.15), hex("#ffffff"), 0.3))
				}
			}
		}
	} else {
		c.centerText(wy, "T I D E M A I L", pal(t*0.15))
	}
	ty := wy + wordRows + 1
	c.centerText(ty, "your mail, your rules", hex("#e6e4f2"))
	line := version + "   ❤"
	vx := c.centerText(ty+1, line, hex("#9a98b4"))
	c.put(vx+len([]rune(line))-1, ty+1, '❤', lerp(hex("#b0174f"), hex("#ff6b8b"), 0.5+0.5*math.Sin(ff*0.22)))
	links := "Support Tidemail · Repository · Issues"
	if !big {
		links = "Support · Repo · Issues"
	}
	c.centerText(ty+3, links, hex("#cfd0e6"))
}

// ── 4. Support: Lighthouse ─────────────────────────────────────────────────

// lighthouse: the Ocean night sky and tide, with a striped lighthouse on a
// rock whose beam sweeps across the sky. When the beam swings toward you,
// the lamp flares.
func lighthouse(c *canvas, f int) {
	ff := float64(f)
	waveRows := max(4, c.h/4)
	waveTop := c.h - waveRows
	tower := c.w >= 72 && waveTop >= 14
	cx := c.w / 2
	if tower {
		cx = (c.w + 14) / 2
	}
	y0 := max(0, (waveTop-supportBlockH)/2)
	inContent := func(x, y int) bool {
		return y >= y0-2 && y <= y0+supportBlockH && x >= cx-24 && x <= cx+24
	}
	nightSky(c, ff, waveTop, inContent)
	shootingStar(c, f, waveTop, inContent)

	// Tower geometry: lamp near the top, body down to the waterline.
	towerH := min(waveTop+1, 15)
	tx, ty := 3, waveTop+1-towerH
	lx, ly := tx+4, ty+1
	angle := ff * 0.045
	dir := math.Cos(angle)
	flare := 1 - math.Abs(dir)
	on := ease(ff, 4, 20)

	if tower {
		// Beam: a widening cone toward wherever the light is pointing.
		reach := dir * float64(c.w) * 0.95
		sign := 1
		if reach < 0 {
			sign = -1
		}
		for dx := 1; dx <= int(math.Abs(reach)); dx++ {
			x := lx + sign*dx
			spread := 0.6 + float64(dx)*0.09
			fall := 1 - float64(dx)/math.Abs(reach)*0.75
			for y := ly - int(spread); y <= ly+int(spread); y++ {
				p := c.at(x, y)
				if p == nil || y >= waveTop {
					continue
				}
				t := 1 - math.Abs(float64(y-ly))/(spread+0.5)
				in := t * fall * 0.5 * on
				p.bg = lerp(p.bg, hex("#fff0b3"), in)
				if p.ch != ' ' {
					p.fg = lerp(p.fg, hex("#ffffff"), in)
				}
			}
		}
		// Glow around the lamp, strongest when the beam faces you.
		for y := ly - 3; y <= ly+3; y++ {
			for x := lx - 6; x <= lx+6; x++ {
				d := math.Hypot(float64(x-lx)*0.5, float64(y-ly))
				if p := c.at(x, y); p != nil && d < 3.5 && y < waveTop {
					p.bg = lerp(p.bg, hex("#ffd56b"), (1-d/3.5)*(0.15+0.5*flare)*on)
				}
			}
		}
		red, white, dark, rock := hex("#c0392b"), hex("#efe9dc"), hex("#2b2f3a"), hex("#3a3f4b")
		art := []string{
			"    ▲    ",
			"   ▐█▌   ",
			"  ▀▀▀▀▀  ",
		}
		body := towerH - len(art) - 2
		for k := range body {
			switch {
			case k < body/3:
				art = append(art, "   ███   ")
			case k < 2*body/3:
				art = append(art, "  ▐███▌  ")
			default:
				art = append(art, "  █████  ")
			}
		}
		art = append(art, " ▟█████▙ ", "▟███████▙")
		for r, row := range art {
			for i, ch := range []rune(row) {
				if ch == ' ' {
					continue
				}
				var col rgb
				switch {
				case r == 0:
					col = hex("#8e2b22")
				case r == 1:
					col = lerp(hex("#ffd56b"), hex("#ffffff"), flare)
				case r == 2:
					col = dark
				case r >= len(art)-2:
					col = rock
				default:
					col = red
					if ((r-3)/2)%2 == 1 {
						col = white
					}
				}
				c.put(tx+i, ty+r, ch, col)
			}
		}
	}

	sky := func(x, y int) rgb {
		if p := c.at(x, y); p != nil {
			return p.bg
		}
		return skyBot
	}
	supportBlock(c, cx, y0, ff, nightSupport, ease(ff, 18, 36), sky)
	oceanWaves(c, ff, waveTop, waveRows, ease(ff, 0, 30))
}

// ── 5. Support: Message in a bottle ────────────────────────────────────────

// bottle: a bottle drifts in on the tide; once it arrives a parchment
// scroll unrolls up out of it with the Support content written on it.
func bottle(c *canvas, f int) {
	ff := float64(f)
	waveRows := max(4, c.h/4)
	waveTop := c.h - waveRows
	cardW := min(c.w-4, 54)
	cardH := supportBlockH + 3
	cardBottom := waveTop - 2
	cardTop := max(0, cardBottom-cardH+1)
	cx := c.w / 2
	inContent := func(x, y int) bool {
		return y >= cardTop-1 && y <= cardBottom+1 && x >= cx-cardW/2-2 && x <= cx+cardW/2+2
	}
	nightSky(c, ff, waveTop, inContent)
	if c.w > 70 {
		moon(c, ff, c.w-11, 6)
	}
	shootingStar(c, f, waveTop, inContent)
	tide := ease(ff, 0, 25)
	oceanWaves(c, ff, waveTop, waveRows, tide)

	// The bottle rides the front wave's crest in from the right.
	arrive := ease(ff, 4, 60)
	bx := int(float64(c.w+4) - float64(c.w+4-(cx-4))*arrive)
	h := waveHeight(ff, bx+3, 2, waveRows, tide)
	by := waveTop + waveRows - int(math.Ceil(h)) - 1
	if arrive >= 1 {
		by = min(by, waveTop+waveRows-2)
	}
	glass, cork := hex("#5fd3a6"), hex("#a0703f")
	for i, ch := range []rune("▬▐▓▓▓▓▌") {
		col := glass
		if i == 0 {
			col = cork
		}
		if p := c.at(bx+i, by); p != nil {
			c.put(bx+i, by, ch, col)
		}
	}

	// Scroll unrolls upward from just above the bottle.
	open := ease(ff, 62, 88)
	if open <= 0 {
		return
	}
	paper, edge, ink := hex("#eadcb3"), hex("#a6895a"), hex("#3a2a16")
	shown := int(math.Round(open * float64(cardH)))
	top := cardBottom - shown + 1
	x0 := cx - cardW/2
	for y := top; y <= cardBottom; y++ {
		for x := x0; x < x0+cardW; x++ {
			p := c.at(x, y)
			if p == nil {
				continue
			}
			roll := y == top || y == cardBottom
			switch {
			case roll && (x == x0 || x == x0+cardW-1):
				c.set(x, y, '●', edge, p.bg)
			case roll:
				c.set(x, y, '█', edge, p.bg)
			default:
				// Faint parchment grain.
				bg := lerp(paper, edge, frand(x*31+y*17)*0.12)
				c.set(x, y, ' ', ink, bg)
			}
		}
	}
	pal := supportPalette{
		heading: hex("#7a2416"), text: ink, muted: hex("#6b5636"),
		edge: hex("#8a6a3a"), label: ink, accent: hex("#2a6f97"), accentFg: hex("#f6efdc"),
		heartLo: hex("#a11a3c"), heartHi: hex("#e0365c"), steam: hex("#9c8a6a"),
	}
	// Only draw content in the unrolled part of the scroll.
	clip := newCanvas(c.w, c.h)
	copy(clip.cells, c.cells)
	supportBlock(clip, cx, cardTop+2, ff, pal, ease(ff, 80, 96), func(x, y int) rgb {
		if p := c.at(x, y); p != nil {
			return p.bg
		}
		return paper
	})
	for y := top + 1; y < cardBottom; y++ {
		for x := x0 + 1; x < x0+cardW-1; x++ {
			if p, q := c.at(x, y), clip.at(x, y); p != nil && q != nil {
				*p = *q
			}
		}
	}
}

// ── 6. Support: Café ───────────────────────────────────────────────────────

// cafe: a cozy café at night. String lights sway along the top, a big mug
// steams (and every so often the steam drifts into a heart), and a Pac-Man
// munches dots along the counter, a nod to the current Support page.
func cafe(c *canvas, f int) {
	ff := float64(f)
	for y := range c.h {
		bg := lerp(hex("#140c07"), hex("#26170d"), float64(y)/float64(max(1, c.h-1)))
		for x := range c.w {
			c.set(x, y, ' ', bg, bg)
		}
	}
	// String lights.
	bulbs := []rgb{hex("#ffcf6b"), hex("#ff8f6b"), hex("#ffe6a8"), hex("#ffb36b")}
	// String lights: a drooping wire in half-block pixels, bulbs hanging off it.
	for x := range c.w {
		sway := 0.4 * math.Sin(ff*0.05)
		py := 2 + int(math.Round((3.2+sway)*math.Sin(math.Pi*float64(x%40)/40)))
		c.plot(x, py, hex("#4a3424"))
		if x%5 == 2 {
			i := x / 5
			tw := 0.55 + 0.45*math.Sin(ff*0.15+frand(i)*6.28)
			b := bulbs[i%len(bulbs)]
			y := py/2 + 1
			c.put(x, y, '●', lerp(hex("#26170d"), b, tw*ease(ff, frand(i)*15, frand(i)*15+8)))
			if p := c.at(x, y+1); p != nil {
				p.bg = lerp(p.bg, b, 0.08*tw)
			}
		}
	}

	counter := c.h - 2
	mug := c.w >= 84 && c.h >= 24
	cx := c.w / 2
	if mug {
		cx = c.w/2 + 12
	}
	y0 := max(4, (counter-supportBlockH)/2+1)
	pal := supportPalette{
		heading: hex("#ffb86b"), text: hex("#f1e4d3"), muted: hex("#b39a84"),
		edge: hex("#7a5a3e"), label: hex("#f1e4d3"), accent: hex("#ffb86b"), accentFg: hex("#1a0f08"),
		heartLo: hex("#b0174f"), heartHi: hex("#ff6b8b"), steam: hex("#e8dccb"),
	}

	if mug {
		mx, my := 6, counter-7
		cream, shade, coffee := hex("#eadfcd"), hex("#bfae95"), hex("#6b3e26")
		art := []string{
			"▄▄▄▄▄▄▄▄▄▄▄   ",
			"█▓▓▓▓▓▓▓▓▓█▄▄ ",
			"███████████  █",
			"███████████  █",
			"███████████▀▀ ",
			" ▀▀▀▀▀▀▀▀▀    ",
		}
		for r, row := range art {
			for i, ch := range []rune(row) {
				if ch == ' ' {
					continue
				}
				col := cream
				if ch == '▓' {
					col = coffee
				} else if i >= 9 && r >= 2 && r <= 4 && i <= 10 {
					col = shade
				}
				c.put(mx+i, my+r, ch, col)
			}
		}
		c.put(mx+5, my+1, '♥', cream) // latte art
		// Saucer.
		for i := -2; i < 16; i++ {
			c.put(mx+i, my+6, '▀', hex("#d8ccb8"))
		}

		// Steam: wisps most of the time, a heart every ~12s.
		heart := ease(float64(f%120), 70, 85) * (1 - ease(float64(f%120), 105, 118))
		sx := mx + 5
		for k := range 3 {
			for s := range 7 {
				px := sx - 2 + k*2 + int(math.Round(math.Sin(ff*0.25+float64(s)*0.9+float64(k)*2.1)*1.2))
				py := my - 1 - s
				if p := c.at(px, py); p != nil && p.ch == ' ' {
					a := (1 - float64(s)/7) * 0.7 * (1 - heart) * ease(ff, 10, 25)
					c.put(px, py, []rune("~∽∿")[(f/3+s+k)%3], lerp(p.bg, pal.steam, a))
				}
			}
		}
		if heart > 0 {
			hcx, hcy := float64(sx), float64((my-5)*2)
			for py := int(hcy) - 7; py <= int(hcy)+7; py++ {
				for x := sx - 6; x <= sx+6; x++ {
					hx := (float64(x) - hcx) / 5.5
					hy := -(float64(py) - hcy) / 6
					t := hx*hx + hy*hy - 1
					v := t*t*t - hx*hx*hy*hy*hy
					if v <= 0 {
						if p := c.at(x, py/2); p != nil {
							c.plot(x, py, lerp(p.bg, pal.steam, heart*0.55))
						}
					}
				}
			}
		}
	}

	bgAt := func(x, y int) rgb {
		if p := c.at(x, y); p != nil {
			return p.bg
		}
		return hex("#26170d")
	}
	supportBlock(c, cx, y0, ff, pal, ease(ff, 14, 30), bgAt)

	// Counter edge with Pac-Man munching dots along it.
	for x := range c.w {
		c.set(x, counter, '▔', hex("#5a3e28"), bgAt(x, counter))
		if p := c.at(x, counter+1); p != nil {
			p.bg = hex("#1a0f08")
		}
	}
	period := c.w + 10
	pac := (f/2)%period - 5
	for x := 1; x < c.w; x += 3 {
		if x > pac {
			c.put(x, counter+1, '·', hex("#ffd9a0"))
		}
	}
	glyph := '𜱭'
	if (f/2)%2 == 1 {
		glyph = '●'
	}
	c.put(pac, counter+1, glyph, hex("#f2cf45"))
}

// ── program ────────────────────────────────────────────────────────────────

type scene struct {
	name, page string
	draw       func(*canvas, int)
}

var scenes = []scene{
	{"Ocean night", "About", oceanNight},
	{"Retro boot", "About", retroBoot},
	{"Plasma", "About", plasma},
	{"Lighthouse", "Support", lighthouse},
	{"Message in a bottle", "Support", bottle},
	{"Café", "Support", cafe},
}

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

type model struct{ w, h, f, scene int }

func (m model) Init() tea.Cmd { return tick() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.f++
		return m, tick()
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyMsg:
		switch k := msg.String(); k {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "1", "2", "3", "4", "5", "6":
			m.scene, m.f = int(k[0]-'1'), 0
		case "right", "l", "tab", "n":
			m.scene, m.f = (m.scene+1)%len(scenes), 0
		case "left", "h", "shift+tab", "p":
			m.scene, m.f = (m.scene+len(scenes)-1)%len(scenes), 0
		case "r":
			m.f = 0
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.w < 24 || m.h < 10 {
		return "terminal too small for the preview"
	}
	c := newCanvas(m.w, m.h-1)
	scenes[m.scene].draw(c, m.f)
	return c.String() + "\n" + statusBar(m.w, m.scene)
}

func statusBar(w, active int) string {
	const base = "\x1b[48;2;18;20;28m\x1b[38;2;140;150;170m"
	const hi = "\x1b[48;2;79;216;255m\x1b[38;2;5;10;20m"
	var b strings.Builder
	used := 0
	add := func(s, style string) bool {
		n := len([]rune(s))
		if used+n > w {
			return false
		}
		b.WriteString(style + s + base)
		used += n
		return true
	}
	b.WriteString(base)
	page := ""
	for i, s := range scenes {
		if s.page != page {
			page = s.page
			add(" "+page+" ", base)
		}
		style := base
		if i == active {
			style = hi
		}
		add(fmt.Sprintf(" %d ", i+1), style)
	}
	add("  "+scenes[active].name, "\x1b[48;2;18;20;28m\x1b[38;2;220;228;240m")
	add("   ←/→ switch  r replay  q quit", base)
	if used < w {
		b.WriteString(strings.Repeat(" ", w-used))
	}
	return b.String() + "\x1b[0m"
}

func main() {
	if _, err := tea.NewProgram(model{}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
