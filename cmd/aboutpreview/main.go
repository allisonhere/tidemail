// Command aboutpreview is a throwaway side-by-side preview of three candidate
// animated About screens for TideMail. Nothing in the app uses it.
//
//	go run ./cmd/aboutpreview
//
// 1/2/3 or ←/→ switch concepts, r replays the intro, q quits.
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
	fmt.Sscanf(s, "#%02x%02x%02x", &c.r, &c.g, &c.b)
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

func (c *canvas) text(x, y int, s string, fg rgb) {
	for i, r := range []rune(s) {
		c.put(x+i, y, r, fg)
	}
}

func (c *canvas) centerText(y int, s string, fg rgb) int {
	x := (c.w - len([]rune(s))) / 2
	c.text(x, y, s, fg)
	return x
}

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

// ── 1. Ocean night ─────────────────────────────────────────────────────────

func oceanNight(c *canvas, f int) {
	ff := float64(f)
	skyTop, skyBot := hex("#02040d"), hex("#0d2048")
	sky := func(y int) rgb { return lerp(skyTop, skyBot, float64(y)/float64(max(1, c.h-1))) }
	for y := range c.h {
		bg := sky(y)
		for x := range c.w {
			c.set(x, y, ' ', bg, bg)
		}
	}

	waveRows := max(4, c.h/4)
	waveTop := c.h - waveRows
	y0 := max(0, (waveTop-12)/2) // content block: wordmark through links
	contentW := max(wordW, 48)
	inContent := func(x, y int) bool {
		return y >= y0-1 && y <= y0+12 && x >= (c.w-contentW)/2-3 && x <= (c.w+contentW)/2+3
	}

	// Stars fade in at staggered times, then twinkle at their own pace.
	for i := range c.w * waveTop / 16 {
		x := int(hash(i*3+1) % uint32(c.w))
		y := int(hash(i*3+2) % uint32(max(1, waveTop-1)))
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

	// Moon: a soft glow, then a crescent drawn in half-block pixels.
	moon := ease(ff, 8, 35)
	if moon > 0 && c.w > 50 {
		mx, my := float64(c.w-11), 6.0
		for y := 0; y < 8; y++ {
			for x := c.w - 20; x < c.w-1; x++ {
				d := math.Hypot(float64(x)-mx, float64(y*2+1)-my)
				if p := c.at(x, y); p != nil && d < 9 {
					p.bg = lerp(p.bg, hex("#1f3a78"), (1-d/9)*0.55*moon)
				}
			}
		}
		for py := 0; py < 14; py++ {
			for x := c.w - 16; x < c.w-5; x++ {
				in := math.Hypot(float64(x)-mx, float64(py)-my) < 3.6
				bite := math.Hypot(float64(x)-mx-1.8, float64(py)-my+1.2) < 3.1
				if in && !bite {
					p := c.at(x, py/2)
					if p != nil {
						c.plot(x, py, lerp(p.bg, hex("#f7edcb"), moon))
					}
				}
			}
		}
	}

	// A shooting star now and then once the intro has finished.
	if f > 60 {
		n, cyc := (f-60)/150, (f-60)%150
		if cyc < 16 {
			sx := c.w/6 + int(frand(n*13+1)*float64(c.w)*0.5)
			sy := 1 + int(frand(n*13+2)*float64(max(1, waveTop/4)))
			for k := 0; k < 8; k++ {
				pos := cyc - k
				x, y := sx+pos*3, sy+pos
				if pos < 0 || y >= waveTop-1 || inContent(x, y) {
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
	}

	// Content block, vertically centered in the sky.
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

func oceanWaves(c *canvas, ff float64, top, rows int, tide float64) {
	layer := [3]rgb{hex("#0b3556"), hex("#11658a"), hex("#1aa0c8")}
	deep, foam := hex("#04121f"), hex("#e4fbff")
	t := ff * 0.11
	R := float64(rows) * tide
	heightAt := func(x, l int) float64 {
		fx := float64(x)
		switch l {
		case 0:
			return R * (0.86 + 0.08*math.Sin(fx*0.11+t*0.7) + 0.04*math.Sin(fx*0.31-t*1.3))
		case 1:
			return R * (0.62 + 0.11*math.Sin(fx*0.17-t*0.9+1.7) + 0.05*math.Sin(fx*0.41+t*1.5))
		default:
			return R * (0.38 + 0.14*math.Sin(fx*0.23+t*1.2+3.1) + 0.06*math.Sin(fx*0.53-t*1.9))
		}
	}
	blocks := []rune(" ▁▂▃▄▅▆▇█")
	for x := range c.w {
		hs := [3]float64{heightAt(x, 0), heightAt(x, 1), heightAt(x, 2)}
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
			// Surface cell: foam on the crest, the layer behind fills the gap.
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

// ── program ────────────────────────────────────────────────────────────────

var concepts = []string{"Ocean night", "Retro boot", "Plasma"}

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

type model struct{ w, h, f, concept int }

func (m model) Init() tea.Cmd { return tick() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.f++
		return m, tick()
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "1", "2", "3":
			m.concept, m.f = int(msg.String()[0]-'1'), 0
		case "right", "l", "tab", "n":
			m.concept, m.f = (m.concept+1)%len(concepts), 0
		case "left", "h", "shift+tab", "p":
			m.concept, m.f = (m.concept+len(concepts)-1)%len(concepts), 0
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
	switch m.concept {
	case 0:
		oceanNight(c, m.f)
	case 1:
		retroBoot(c, m.f)
	default:
		plasma(c, m.f)
	}
	return c.String() + "\n" + statusBar(m.w, m.concept)
}

func statusBar(w, active int) string {
	const base = "\x1b[48;2;18;20;28m\x1b[38;2;140;150;170m"
	var b strings.Builder
	used := 1
	b.WriteString(base + " ")
	for i, name := range concepts {
		s := fmt.Sprintf(" %d %s ", i+1, name)
		if used+len(s)+1 > w {
			s = fmt.Sprintf(" %d ", i+1)
			if used+len(s)+1 > w {
				break
			}
		}
		if i == active {
			b.WriteString("\x1b[48;2;79;216;255m\x1b[38;2;5;10;20m")
		}
		b.WriteString(s + base + " ")
		used += len(s) + 1
	}
	hint := "  ←/→ switch   r replay   q quit"
	if used+len([]rune(hint)) <= w {
		b.WriteString(hint)
		used += len([]rune(hint))
	}
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
