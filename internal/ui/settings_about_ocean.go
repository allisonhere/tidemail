package ui

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

var (
	oceanSkyTop   = sceneHex("#02040d")
	oceanSkyBot   = sceneHex("#0d2048")
	oceanStar     = sceneHex("#eef4ff")
	oceanMoon     = sceneHex("#f7edcb")
	oceanGlow     = sceneHex("#1f3a78")
	oceanWordA    = sceneHex("#4fd8ff")
	oceanWordB    = sceneHex("#a07bff")
	oceanWhite    = sceneHex("#ffffff")
	oceanTagline  = sceneHex("#a9bde0")
	oceanDim      = sceneHex("#5f7398")
	oceanSignoff  = sceneHex("#8ea2c6")
	oceanHeartLo  = sceneHex("#b0174f")
	oceanHeartHi  = sceneHex("#ff6b8b")
	oceanHalo     = sceneHex("#ffe3a3")
	oceanBtnEdge  = sceneHex("#3d6aa8")
	oceanLabel    = sceneHex("#d2def5")
	oceanAccent   = sceneHex("#4fd8ff")
	oceanAccentFg = sceneHex("#04121f")
	oceanDeep     = sceneHex("#04121f")
	oceanFoam     = sceneHex("#e4fbff")
	oceanLayers   = [3]sceneRGB{sceneHex("#0b3556"), sceneHex("#11658a"), sceneHex("#1aa0c8")}
)

func oceanPride(i int) sceneRGB {
	return sceneFromColor(aboutPrideColors[((i%len(aboutPrideColors))+len(aboutPrideColors))%len(aboutPrideColors)])
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
	c := newSceneCanvas(w, h)
	f := float64(s.aboutWaveFrame)
	pride := s.aboutRevealActive
	loveIsLove := pride && s.aboutRevealFrame >= settingsAboutRevealStart && s.aboutRevealFrame < settingsAboutRevealEnd

	sky := func(y int) sceneRGB {
		return sceneLerp(oceanSkyTop, oceanSkyBot, float64(y)/float64(max(1, l.waveTop-1)))
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
		reveal := sceneEase(f, 6, oceanIntroEnd) * float64(oceanWordW+6)
		sweep := math.Mod((f-oceanIntroEnd)*0.9, float64(oceanWordW+70)) - 30
		for row := range 5 {
			for col := range oceanWordW {
				if oceanWordmark[row][col] && float64(col) <= reveal {
					if p := c.at(x0+col+1, l.y0+row+1); p != nil {
						c.put(x0+col+1, l.y0+row+1, '█', sceneLerp(p.bg, sceneHex("#000000"), 0.55))
					}
				}
			}
		}
		for row := range 5 {
			for col := range oceanWordW {
				if !oceanWordmark[row][col] || float64(col) > reveal {
					continue
				}
				fg := sceneLerp(oceanWordA, oceanWordB, float64(col)/float64(oceanWordW-1))
				if pride {
					fg = oceanPride(col * len(aboutPrideColors) / oceanWordW)
				}
				fg = sceneLerp(fg, oceanWhite, 0.14*float64(4-row)/4)
				if f > oceanIntroEnd {
					if d := math.Abs(float64(col+row*2) - sweep); d < 5 {
						fg = sceneLerp(fg, oceanWhite, (1-d/5)*0.85)
					}
				}
				if edge := reveal - float64(col); edge < 3 {
					fg = sceneLerp(fg, oceanWhite, 1-edge/3)
				}
				c.put(x0+col, l.y0+row, '█', fg)
			}
		}
	} else {
		title := "T I D E M A I L"
		x := c.centerText(l.y0, title, oceanWordA)
		for i := range len(title) {
			fg := sceneLerp(oceanWordA, oceanWordB, float64(i)/float64(len(title)-1))
			if pride {
				fg = oceanPride(i)
			}
			if p := c.at(x+i, l.y0); p != nil {
				p.fg = sceneLerp(p.bg, fg, sceneEase(f, 6, 20))
				p.bold = true
			}
		}
	}

	textFade := sceneEase(f, 14, 30)
	if loveIsLove {
		x := c.centerText(l.tagY, "LOVE IS LOVE", oceanWhite)
		for i := range len("LOVE IS LOVE") {
			if p := c.at(x+i, l.tagY); p != nil {
				p.fg, p.bold = oceanPride(i), true
			}
		}
	} else {
		c.centerText(l.tagY, "your mail, your rules", sceneLerp(sky(l.tagY), oceanTagline, textFade))
	}
	ver := "terminal information delivery engine"
	if v := strings.TrimSpace(s.update.currentVersion); v != "" {
		ver += " · " + v
	}
	if len([]rune(ver)) > w-2 {
		ver = strings.TrimSpace(s.update.currentVersion)
	}
	c.centerText(l.verY, ver, sceneLerp(sky(l.verY), oceanDim, textFade))
	c.centerText(l.signY, "Thanks for taking a look -allie", sceneLerp(sky(l.signY), oceanSignoff, sceneEase(f, 20, 36)))

	// Heart: always visible so keyboard focus is never invisible mid-intro.
	heartFocused := s.focusedField == sfAboutHeart
	pulse := 0.5 + 0.5*math.Sin(f*0.22)
	heart := sceneLerp(oceanHeartLo, oceanHeartHi, pulse)
	if pride {
		heart = oceanPride(s.aboutRevealFrame / 3)
	}
	if heartFocused || pride {
		x := c.centerText(l.heartY, "✦ ♥ ✦", oceanHalo)
		c.put(x+2, l.heartY, '♥', heart)
		if p := c.at(x+2, l.heartY); p != nil {
			p.bold = true
		}
	} else {
		c.centerText(l.heartY, "♥", heart)
	}

	oceanButtons(c, l.linksY, s.focusedField, sceneEase(f, 20, 36))
	oceanTide(c, f, l.waveTop, l.waveRows, sceneEase(f, 0, 35))

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
func oceanStars(c *sceneCanvas, f float64, skyRows int, inContent func(int, int) bool, pride bool) {
	for i := range c.w * skyRows / 16 {
		x := int(sceneHash(i*3+1) % uint32(c.w))
		y := int(sceneHash(i*3+2) % uint32(max(1, skyRows-1)))
		if inContent(x, y) {
			continue
		}
		start := sceneRand(i) * 25
		b := sceneEase(f, start, start+12) * (0.5 + 0.5*math.Sin(f*(0.06+sceneRand(i+11)*0.14)+sceneRand(i+7)*6.28))
		if b < 0.08 {
			continue
		}
		ch := '·'
		if size := sceneRand(i + 5); size > 0.94 {
			ch = '✦'
		} else if size > 0.82 {
			ch = '+'
		}
		col := oceanStar
		if pride {
			col = oceanPride(i)
		}
		if p := c.at(x, y); p != nil {
			c.put(x, y, ch, sceneLerp(p.bg, col, b))
		}
	}
}

// oceanMoonAt draws a soft glow and a crescent in half-block pixels.
func oceanMoonAt(c *sceneCanvas, f float64, cx, cyPx int) {
	moon := sceneEase(f, 8, 30)
	if moon <= 0 {
		return
	}
	mx, my := float64(cx), float64(cyPx)
	for y := 0; y < 8; y++ {
		for x := cx - 9; x < cx+9; x++ {
			d := math.Hypot(float64(x)-mx, float64(y*2+1)-my)
			if p := c.at(x, y); p != nil && d < 9 {
				p.bg = sceneLerp(p.bg, oceanGlow, (1-d/9)*0.55*moon)
				if p.ch != ' ' {
					p.fg = sceneLerp(p.fg, oceanGlow, (1-d/9)*0.5)
				}
			}
		}
	}
	for py := cyPx - 4; py <= cyPx+4; py++ {
		for x := cx - 4; x <= cx+4; x++ {
			in := math.Hypot(float64(x)-mx, float64(py)-my) < 3.6
			bite := math.Hypot(float64(x)-mx-1.8, float64(py)-my+1.2) < 3.1
			if p := c.at(x, py/2); p != nil && in && !bite {
				c.plot(x, py, sceneLerp(p.bg, oceanMoon, moon))
			}
		}
	}
}

// oceanShootingStar sends one across the sky every ~15s after the intro.
func oceanShootingStar(c *sceneCanvas, f float64, skyRows int, inContent func(int, int) bool) {
	frame := int(f) - oceanIntroEnd - 20
	if frame < 0 {
		return
	}
	n, cyc := frame/150, frame%150
	if cyc >= 16 {
		return
	}
	sx := c.w/6 + int(sceneRand(n*13+1)*float64(c.w)*0.5)
	sy := 1 + int(sceneRand(n*13+2)*float64(max(1, skyRows/4)))
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
			c.put(x, y, ch, sceneLerp(p.bg, oceanWhite, 1-float64(k)/8))
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
func oceanButtons(c *sceneCanvas, y int, focused settingsField, fade float64) {
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
					*p = sceneCell{ch: r, fg: oceanAccentFg, bg: oceanAccent, bold: true}
				} else {
					c.put(x+i, y+1, r, sceneLerp(p.bg, oceanLabel, fade))
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
				border := sceneLerp(p.bg, oceanBtnEdge, fade)
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
					p.ch, p.fg = r, sceneLerp(p.bg, oceanLabel, fade)
				}
			}
		}
		x += bw + 2
	}
}

// oceanTide draws three wave layers rolling in along the bottom rows. Deeper
// cells shade darker, crests catch foam, and where a crest only partly fills
// a cell the layer behind it shows through instead of the sky.
func oceanTide(c *sceneCanvas, f float64, top, rows int, tide float64) {
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
			shade := func(l int) sceneRGB {
				return sceneLerp(oceanLayers[l], oceanDeep, clamp01((hs[l]-fromBottom)/float64(rows))*0.75)
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
			fg = sceneLerp(fg, oceanFoam, foamAmt[front])
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
