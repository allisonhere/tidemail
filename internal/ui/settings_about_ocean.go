package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Settings → About, "Ocean night": a night sky with twinkling stars, a
// crescent moon and the odd shooting star, a block-letter TIDEMAIL wordmark
// that draws itself in and then catches a sweeping light, and a tide that
// rolls in along the bottom with foam on the crests. The scene is drawn into
// a cell canvas and emitted as same-style runs, so a frame stays small. The
// heart easter egg turns the wordmark and stars pride colors and spells out
// LOVE IS LOVE. Plain (VT52) themes keep the original About screen.
//
// The wave layering follows the technique in tideftp's about screen: summed
// sines per column give each layer a water height in rows, and block glyphs
// (▁▂▃▄▅▆▇█) give the crest cell sub-cell resolution.

const (
	settingsAboutWavePeriod      = 100 * time.Millisecond // 10fps
	settingsAboutWaveSettleFrame = 400                    // ~40s, then stop ticking
	oceanIntroEnd                = 40                     // wordmark fully drawn
)

type settingsAboutWaveMsg struct{}

func settingsAboutWaveCmd() tea.Cmd {
	return tea.Tick(settingsAboutWavePeriod, func(time.Time) tea.Msg {
		return settingsAboutWaveMsg{}
	})
}

// ── palette ────────────────────────────────────────────────────────────────

type oceanRGB struct{ r, g, b uint8 }

func oceanHex(s string) oceanRGB {
	var c oceanRGB
	fmt.Sscanf(s, "#%02x%02x%02x", &c.r, &c.g, &c.b)
	return c
}

func oceanFromColor(c lipgloss.Color) oceanRGB {
	r, g, b, ok := hexToRGB(c)
	if !ok {
		return oceanRGB{}
	}
	return oceanRGB{uint8(r*255 + 0.5), uint8(g*255 + 0.5), uint8(b*255 + 0.5)}
}

func (c oceanRGB) color() lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b))
}

func oceanLerp(a, b oceanRGB, t float64) oceanRGB {
	t = clamp01(t)
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return oceanRGB{m(a.r, b.r), m(a.g, b.g), m(a.b, b.b)}
}

// oceanEase is a smoothstep from 0 at frame a to 1 at frame b.
func oceanEase(f, a, b float64) float64 {
	if f <= a {
		return 0
	}
	if f >= b {
		return 1
	}
	t := (f - a) / (b - a)
	return t * t * (3 - 2*t)
}

func oceanHash(n int) uint32 {
	x := uint32(n)*0x9E3779B1 + 0x7F4A7C15
	x ^= x >> 16
	x *= 0x85EBCA6B
	x ^= x >> 13
	x *= 0xC2B2AE35
	x ^= x >> 16
	return x
}

func oceanRand(n int) float64 { return float64(oceanHash(n)%100000) / 100000 }

var (
	oceanSkyTop   = oceanHex("#02040d")
	oceanSkyBot   = oceanHex("#0d2048")
	oceanStar     = oceanHex("#eef4ff")
	oceanMoon     = oceanHex("#f7edcb")
	oceanGlow     = oceanHex("#1f3a78")
	oceanWordA    = oceanHex("#4fd8ff")
	oceanWordB    = oceanHex("#a07bff")
	oceanWhite    = oceanHex("#ffffff")
	oceanTagline  = oceanHex("#a9bde0")
	oceanDim      = oceanHex("#5f7398")
	oceanSignoff  = oceanHex("#8ea2c6")
	oceanHeartLo  = oceanHex("#b0174f")
	oceanHeartHi  = oceanHex("#ff6b8b")
	oceanHalo     = oceanHex("#ffe3a3")
	oceanBtnEdge  = oceanHex("#3d6aa8")
	oceanLabel    = oceanHex("#d2def5")
	oceanAccent   = oceanHex("#4fd8ff")
	oceanAccentFg = oceanHex("#04121f")
	oceanDeep     = oceanHex("#04121f")
	oceanFoam     = oceanHex("#e4fbff")
	oceanLayers   = [3]oceanRGB{oceanHex("#0b3556"), oceanHex("#11658a"), oceanHex("#1aa0c8")}
)

func oceanPride(i int) oceanRGB {
	return oceanFromColor(aboutPrideColors[((i%len(aboutPrideColors))+len(aboutPrideColors))%len(aboutPrideColors)])
}

// ── canvas ─────────────────────────────────────────────────────────────────

type oceanCell struct {
	ch     rune
	fg, bg oceanRGB
	bold   bool
}

type oceanCanvas struct {
	w, h  int
	cells []oceanCell
}

func newOceanCanvas(w, h int) *oceanCanvas {
	return &oceanCanvas{w: w, h: h, cells: make([]oceanCell, w*h)}
}

func (c *oceanCanvas) at(x, y int) *oceanCell {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return nil
	}
	return &c.cells[y*c.w+x]
}

func (c *oceanCanvas) set(x, y int, ch rune, fg, bg oceanRGB) {
	if p := c.at(x, y); p != nil {
		*p = oceanCell{ch: ch, fg: fg, bg: bg}
	}
}

// put draws a glyph over whatever background is already there.
func (c *oceanCanvas) put(x, y int, ch rune, fg oceanRGB) {
	if p := c.at(x, y); p != nil {
		p.ch, p.fg = ch, fg
	}
}

func (c *oceanCanvas) centerText(y int, s string, fg oceanRGB) int {
	x := (c.w - len([]rune(s))) / 2
	for i, r := range []rune(s) {
		c.put(x+i, y, r, fg)
	}
	return x
}

// plot sets one half-block pixel: each cell is two pixels tall, drawn as ▀
// with the top pixel in the foreground and the bottom one in the background.
func (c *oceanCanvas) plot(x, py int, col oceanRGB) {
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
func (c *oceanCanvas) lines() []string {
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
				ch := cl.ch
				if ch == 0 {
					ch = ' '
				}
				run.WriteRune(ch)
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

// ── wordmark ───────────────────────────────────────────────────────────────

var oceanFont = map[rune][5]string{
	'T': {"█████", "  █  ", "  █  ", "  █  ", "  █  "},
	'I': {"███", " █ ", " █ ", " █ ", "███"},
	'D': {"████ ", "█   █", "█   █", "█   █", "████ "},
	'E': {"█████", "█    ", "████ ", "█    ", "█████"},
	'M': {"█   █", "██ ██", "█ █ █", "█   █", "█   █"},
	'A': {" ███ ", "█   █", "█████", "█   █", "█   █"},
	'L': {"█    ", "█    ", "█    ", "█    ", "█████"},
}

func oceanWordBitmap(s string) [][]bool {
	rows := make([][]bool, 5)
	for i, r := range s {
		if i > 0 {
			for y := range rows {
				rows[y] = append(rows[y], false)
			}
		}
		g := oceanFont[r]
		for y := range 5 {
			for _, ch := range g[y] {
				rows[y] = append(rows[y], ch == '█')
			}
		}
	}
	return rows
}

var (
	oceanWordmark = oceanWordBitmap("TIDEMAIL")
	oceanWordW    = len(oceanWordmark[0])
)

// ── scene ──────────────────────────────────────────────────────────────────

// oceanLayout places the content block. Big layouts use the block-letter
// wordmark; small ones fall back to spaced text so nothing overlaps the tide.
type oceanLayout struct {
	big                                   bool
	waveRows, waveTop                     int
	y0, tagY, verY, signY, heartY, linksY int
	contentH, contentW                    int
}

func oceanLayoutFor(w, h int) (oceanLayout, int) {
	l := oceanLayout{waveRows: max(3, min(6, h/5))}
	l.big = w >= oceanWordW+4 && h-l.waveRows >= 17
	if l.big {
		l.contentH, l.contentW = 15, max(oceanWordW, 54)
	} else {
		l.contentH, l.contentW = 11, min(w, 40)
	}
	h = max(h, l.contentH+l.waveRows+2)
	l.waveTop = h - l.waveRows
	l.y0 = max(0, (l.waveTop-l.contentH)/2)
	if l.big {
		l.tagY, l.verY, l.signY, l.heartY, l.linksY = l.y0+6, l.y0+7, l.y0+9, l.y0+10, l.y0+12
	} else {
		l.tagY, l.verY, l.signY, l.heartY, l.linksY = l.y0+2, l.y0+3, l.y0+5, l.y0+6, l.y0+8
	}
	return l, h
}

func (s Settings) renderAboutOceanSection(width int, chrome managerChrome) settingsSectionBody {
	w := max(1, width)
	h := s.detailHeight - 2 // prependBackLink adds two rows above
	if h <= 0 {
		h = 24
	}
	l, h := oceanLayoutFor(w, h)
	c := newOceanCanvas(w, h)
	f := float64(s.aboutWaveFrame)
	pride := s.aboutRevealActive
	loveIsLove := pride && s.aboutRevealFrame >= settingsAboutRevealStart && s.aboutRevealFrame < settingsAboutRevealEnd

	sky := func(y int) oceanRGB {
		return oceanLerp(oceanSkyTop, oceanSkyBot, float64(y)/float64(max(1, l.waveTop-1)))
	}
	for y := range h {
		bg := sky(y)
		for x := range w {
			c.set(x, y, ' ', bg, bg)
		}
	}
	inContent := func(x, y int) bool {
		return y >= l.y0-1 && y <= l.y0+l.contentH && x >= (w-l.contentW)/2-2 && x <= (w+l.contentW)/2+2
	}

	oceanStars(c, f, l.waveTop, inContent, pride)
	if w > 60 && l.waveTop > 12 {
		oceanMoonAt(c, f, w-11, 6)
	}
	oceanShootingStar(c, f, l.waveTop, inContent)

	// Wordmark: draws in left to right, then a light sweeps across it.
	if l.big {
		x0 := (w - oceanWordW) / 2
		reveal := oceanEase(f, 6, oceanIntroEnd) * float64(oceanWordW+6)
		sweep := math.Mod((f-oceanIntroEnd)*0.9, float64(oceanWordW+70)) - 30
		for row := range 5 {
			for col := range oceanWordW {
				if oceanWordmark[row][col] && float64(col) <= reveal {
					if p := c.at(x0+col+1, l.y0+row+1); p != nil {
						c.put(x0+col+1, l.y0+row+1, '█', oceanLerp(p.bg, oceanHex("#000000"), 0.55))
					}
				}
			}
		}
		for row := range 5 {
			for col := range oceanWordW {
				if !oceanWordmark[row][col] || float64(col) > reveal {
					continue
				}
				fg := oceanLerp(oceanWordA, oceanWordB, float64(col)/float64(oceanWordW-1))
				if pride {
					fg = oceanPride(col * len(aboutPrideColors) / oceanWordW)
				}
				fg = oceanLerp(fg, oceanWhite, 0.14*float64(4-row)/4)
				if f > oceanIntroEnd {
					if d := math.Abs(float64(col+row*2) - sweep); d < 5 {
						fg = oceanLerp(fg, oceanWhite, (1-d/5)*0.85)
					}
				}
				if edge := reveal - float64(col); edge < 3 {
					fg = oceanLerp(fg, oceanWhite, 1-edge/3)
				}
				c.put(x0+col, l.y0+row, '█', fg)
			}
		}
	} else {
		title := "T I D E M A I L"
		x := c.centerText(l.y0, title, oceanWordA)
		for i := range len(title) {
			fg := oceanLerp(oceanWordA, oceanWordB, float64(i)/float64(len(title)-1))
			if pride {
				fg = oceanPride(i)
			}
			if p := c.at(x+i, l.y0); p != nil {
				p.fg = oceanLerp(p.bg, fg, oceanEase(f, 6, 20))
				p.bold = true
			}
		}
	}

	textFade := oceanEase(f, 14, 30)
	if loveIsLove {
		x := c.centerText(l.tagY, "LOVE IS LOVE", oceanWhite)
		for i := range len("LOVE IS LOVE") {
			if p := c.at(x+i, l.tagY); p != nil {
				p.fg, p.bold = oceanPride(i), true
			}
		}
	} else {
		c.centerText(l.tagY, "your mail, your rules", oceanLerp(sky(l.tagY), oceanTagline, textFade))
	}
	ver := "terminal information delivery engine"
	if v := strings.TrimSpace(s.update.currentVersion); v != "" {
		ver += " · " + v
	}
	if len([]rune(ver)) > w-2 {
		ver = strings.TrimSpace(s.update.currentVersion)
	}
	c.centerText(l.verY, ver, oceanLerp(sky(l.verY), oceanDim, textFade))
	c.centerText(l.signY, "Thanks for taking a look -allie", oceanLerp(sky(l.signY), oceanSignoff, oceanEase(f, 20, 36)))

	// Heart: always visible so keyboard focus is never invisible mid-intro.
	heartFocused := s.focusedField == sfAboutHeart
	pulse := 0.5 + 0.5*math.Sin(f*0.22)
	heart := oceanLerp(oceanHeartLo, oceanHeartHi, pulse)
	if pride {
		heart = oceanPride(s.aboutRevealFrame / 3)
	}
	if heartFocused || pride {
		x := c.centerText(l.heartY, "✦ ❤ ✦", oceanHalo)
		c.put(x+2, l.heartY, '❤', heart)
		if p := c.at(x+2, l.heartY); p != nil {
			p.bold = true
		}
	} else {
		c.centerText(l.heartY, "❤", heart)
	}

	oceanButtons(c, l.linksY, s.focusedField, oceanEase(f, 20, 36))
	oceanTide(c, f, l.waveTop, l.waveRows, oceanEase(f, 0, 35))

	return settingsSectionBody{
		lines: c.lines(),
		anchors: map[settingsField]int{
			sfAboutHeart:   l.heartY,
			sfAboutSupport: l.linksY,
			sfAboutRepo:    l.linksY,
			sfAboutIssues:  l.linksY,
		},
	}
}

// oceanStars fades stars in at staggered times; each then twinkles at its own
// pace. During the heart reveal they take on the pride colors.
func oceanStars(c *oceanCanvas, f float64, skyRows int, inContent func(int, int) bool, pride bool) {
	for i := range c.w * skyRows / 16 {
		x := int(oceanHash(i*3+1) % uint32(c.w))
		y := int(oceanHash(i*3+2) % uint32(max(1, skyRows-1)))
		if inContent(x, y) {
			continue
		}
		start := oceanRand(i) * 25
		b := oceanEase(f, start, start+12) * (0.5 + 0.5*math.Sin(f*(0.06+oceanRand(i+11)*0.14)+oceanRand(i+7)*6.28))
		if b < 0.08 {
			continue
		}
		ch := '·'
		if size := oceanRand(i + 5); size > 0.94 {
			ch = '✦'
		} else if size > 0.82 {
			ch = '+'
		}
		col := oceanStar
		if pride {
			col = oceanPride(i)
		}
		if p := c.at(x, y); p != nil {
			c.put(x, y, ch, oceanLerp(p.bg, col, b))
		}
	}
}

// oceanMoonAt draws a soft glow and a crescent in half-block pixels.
func oceanMoonAt(c *oceanCanvas, f float64, cx, cyPx int) {
	moon := oceanEase(f, 8, 30)
	if moon <= 0 {
		return
	}
	mx, my := float64(cx), float64(cyPx)
	for y := 0; y < 8; y++ {
		for x := cx - 9; x < cx+9; x++ {
			d := math.Hypot(float64(x)-mx, float64(y*2+1)-my)
			if p := c.at(x, y); p != nil && d < 9 {
				p.bg = oceanLerp(p.bg, oceanGlow, (1-d/9)*0.55*moon)
				if p.ch != ' ' {
					p.fg = oceanLerp(p.fg, oceanGlow, (1-d/9)*0.5)
				}
			}
		}
	}
	for py := cyPx - 4; py <= cyPx+4; py++ {
		for x := cx - 4; x <= cx+4; x++ {
			in := math.Hypot(float64(x)-mx, float64(py)-my) < 3.6
			bite := math.Hypot(float64(x)-mx-1.8, float64(py)-my+1.2) < 3.1
			if p := c.at(x, py/2); p != nil && in && !bite {
				c.plot(x, py, oceanLerp(p.bg, oceanMoon, moon))
			}
		}
	}
}

// oceanShootingStar sends one across the sky every ~15s after the intro.
func oceanShootingStar(c *oceanCanvas, f float64, skyRows int, inContent func(int, int) bool) {
	frame := int(f) - oceanIntroEnd - 20
	if frame < 0 {
		return
	}
	n, cyc := frame/150, frame%150
	if cyc >= 16 {
		return
	}
	sx := c.w/6 + int(oceanRand(n*13+1)*float64(c.w)*0.5)
	sy := 1 + int(oceanRand(n*13+2)*float64(max(1, skyRows/4)))
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
			c.put(x, y, ch, oceanLerp(p.bg, oceanWhite, 1-float64(k)/8))
		}
	}
}

type oceanButton struct {
	label string
	short string
	field settingsField
	inner int
}

var oceanLinkButtons = []oceanButton{
	{"Support Tidemail", "Support", sfAboutSupport, 18},
	{"Repository", "Repo", sfAboutRepo, 12},
	{"Issues", "Issues", sfAboutIssues, 10},
}

// oceanButtons draws the three link buttons as rounded boxes, or as one
// compact row when the pane is too narrow. The focused button is filled so
// keyboard focus reads at a glance, and is drawn at full strength even
// while the rest fade in.
func oceanButtons(c *oceanCanvas, y int, focused settingsField, fade float64) {
	total := 0
	for _, b := range oceanLinkButtons {
		total += b.inner + 2
	}
	total += 2 * (len(oceanLinkButtons) - 1)

	if c.w < total+2 {
		parts := make([]string, len(oceanLinkButtons))
		for i, b := range oceanLinkButtons {
			parts[i] = "[" + b.short + "]"
		}
		row := strings.Join(parts, " ")
		x := (c.w - len(row)) / 2
		for _, b := range oceanLinkButtons {
			label := "[" + b.short + "]"
			for i, r := range label {
				p := c.at(x+i, y+1)
				if p == nil {
					continue
				}
				if b.field == focused {
					*p = oceanCell{ch: r, fg: oceanAccentFg, bg: oceanAccent, bold: true}
				} else {
					c.put(x+i, y+1, r, oceanLerp(p.bg, oceanLabel, fade))
				}
			}
			x += len(label) + 1
		}
		return
	}

	x := (c.w - total) / 2
	for _, b := range oceanLinkButtons {
		bw := b.inner + 2
		on := b.field == focused
		for dy := 0; dy < 3; dy++ {
			for dx := 0; dx < bw; dx++ {
				p := c.at(x+dx, y+dy)
				if p == nil {
					continue
				}
				border := oceanLerp(p.bg, oceanBtnEdge, fade)
				if on {
					border = oceanAccent
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
					c.put(x+dx, y+dy, ch, border)
				} else if on {
					p.bg = oceanAccent
				}
			}
		}
		lx := x + 1 + (b.inner-len(b.label))/2
		for i, r := range b.label {
			if p := c.at(lx+i, y+1); p != nil {
				if on {
					p.ch, p.fg, p.bold = r, oceanAccentFg, true
				} else {
					p.ch, p.fg = r, oceanLerp(p.bg, oceanLabel, fade)
				}
			}
		}
		x += bw + 2
	}
}

// oceanTide draws three wave layers rolling in along the bottom rows. Deeper
// cells shade darker, crests catch foam, and where a crest only partly fills
// a cell the layer behind it shows through instead of the sky.
func oceanTide(c *oceanCanvas, f float64, top, rows int, tide float64) {
	t := f * 0.11
	r := float64(rows) * tide
	heightAt := func(x, l int) float64 {
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
	blocks := []rune(" ▁▂▃▄▅▆▇█")
	foamAmt := [3]float64{0.15, 0.3, 0.6}
	for x := range c.w {
		hs := [3]float64{heightAt(x, 0), heightAt(x, 1), heightAt(x, 2)}
		for row := range rows {
			y := top + row
			p := c.at(x, y)
			if p == nil {
				continue
			}
			fromBottom := float64(rows - row)
			shade := func(l int) oceanRGB {
				return oceanLerp(oceanLayers[l], oceanDeep, clamp01((hs[l]-fromBottom)/float64(rows))*0.75)
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
			fg = oceanLerp(fg, oceanFoam, foamAmt[front])
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
