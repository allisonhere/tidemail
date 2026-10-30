package ui

import (
	"math"
	"time"
)

// Settings → Support, "Café": a cozy café at night. String lights sway along
// the top, a big mug steams (and every so often the steam drifts into a
// heart), and along the counter a Pac-Man chases a runaway coffee cup. The
// Support copy and the sponsor buttons sit in the middle. Plain (VT52) themes
// keep the original dotted-frame Support page.

const (
	settingsSupportCafePeriod = 100 * time.Millisecond // smoother than the plain page's 240ms
	cafeBuyLabel              = "Sponsor"
	cafeCopyLabel             = "Copy link"
	cafeChaseGap              = 8 // Pac-Man stays this far behind the cup
	cafeSteamHeartEvery       = 120
)

var (
	cafeBgTop     = sceneHex("#140c07")
	cafeBgBot     = sceneHex("#26170d")
	cafeWire      = sceneHex("#4a3424")
	cafeHeading   = sceneHex("#ffb86b")
	cafeText      = sceneHex("#f1e4d3")
	cafeMuted     = sceneHex("#b39a84")
	cafeEdge      = sceneHex("#7a5a3e")
	cafeAccent    = sceneHex("#ffb86b")
	cafeAccentFg  = sceneHex("#1a0f08")
	cafeHeartLo   = sceneHex("#b0174f")
	cafeHeartHi   = sceneHex("#ff6b8b")
	cafeSteam     = sceneHex("#e8dccb")
	cafeCup       = sceneHex("#f2e6d0")
	cafeCream     = sceneHex("#eadfcd")
	cafeMugShade  = sceneHex("#bfae95")
	cafeCoffee    = sceneHex("#6b3e26")
	cafeSaucer    = sceneHex("#d8ccb8")
	cafeCounter   = sceneHex("#5a3e28")
	cafeFloor     = sceneHex("#1a0f08")
	cafeDot       = sceneHex("#ffd9a0")
	cafePacYellow = sceneFromColor(supportPacmanYellow)
	cafeBulbs     = []sceneRGB{sceneHex("#ffcf6b"), sceneHex("#ff8f6b"), sceneHex("#ffe6a8"), sceneHex("#ffb36b")}
)

// supportPulsePeriod is the Support page's animation tick: the café wants a
// smooth frame rate, the plain chase keeps its original pace.
func (s Settings) supportPulsePeriod() time.Duration {
	if ThemeUsesASCII(s.themeName) {
		return settingsSupportPulsePeriod
	}
	return settingsSupportCafePeriod
}

type cafeLayout struct {
	stack                                     bool
	lights, counter, mug                      bool
	cx, y0                                    int
	linkY, openY, copyY, urlY, thanksY, heart int
}

func cafeLayoutFor(w, h int) (cafeLayout, int) {
	var l cafeLayout
	l.stack = w < len(cafeBuyLabel)+len(cafeCopyLabel)+12
	contentH := 16
	if l.stack {
		contentH += 4
	}
	l.lights = h >= contentH+8
	l.counter = h >= contentH+4
	h = max(h, contentH)
	top, bottom := 0, h
	if l.lights {
		top = 4
	}
	if l.counter {
		bottom = h - 2
	}
	l.mug = w >= 84 && bottom-top >= 18
	l.cx = w / 2
	if l.mug {
		l.cx = w/2 + 10
	}
	l.y0 = top + max(0, (bottom-top-contentH)/2)
	l.linkY = l.y0 + 6
	l.openY, l.copyY = l.y0+8, l.y0+8
	l.urlY = l.y0 + 12
	if l.stack {
		l.copyY = l.y0 + 12
		l.urlY = l.y0 + 16
	}
	l.thanksY = l.urlY + 2
	l.heart = l.thanksY + 1
	return l, h
}

func (s Settings) renderSupportCafeSection(width int) settingsSectionBody {
	w := max(1, width)
	h := s.detailHeight
	if h <= 0 {
		h = 24
	}
	l, h := cafeLayoutFor(w, h)
	c := newSceneCanvas(w, h)
	f := float64(s.supportPulseFrame)

	for y := range h {
		bg := sceneLerp(cafeBgTop, cafeBgBot, float64(y)/float64(max(1, h-1)))
		for x := range w {
			c.set(x, y, ' ', bg, bg)
		}
	}
	if l.lights {
		cafeLights(c, f)
	}
	if l.mug {
		cafeMug(c, f, 6, h-3-6)
	}

	fade := sceneEase(f, 4, 16)
	text := func(y int, s string, col sceneRGB, bold bool) int {
		x := c.textAt(l.cx, y, s, col)
		for i := range len([]rune(s)) {
			if p := c.at(x+i, y); p != nil {
				p.fg, p.bold = sceneLerp(p.bg, col, fade), bold
			}
		}
		return x
	}
	text(l.y0, "Support Tidemail", cafeHeading, true)
	text(l.y0+2, "Tidemail is free and open source.", cafeText, false)
	text(l.y0+3, "If it makes email a little better for you,", cafeMuted, false)
	text(l.y0+4, "you can support continued development.", cafeMuted, false)

	// "☕ GitHub Sponsors": the cup is double width, so it's placed by hand and the
	// name follows it; steam curls off the cup into the blank row above.
	link := supportLinks[0]
	name := " " + link.Name
	kx := l.cx - (2+len([]rune(name)))/2
	if p := c.at(kx, l.linkY); p != nil {
		c.putWide(kx, l.linkY, '☕', sceneLerp(p.bg, cafeCup, fade))
	}
	for i, r := range []rune(name) {
		if p := c.at(kx+2+i, l.linkY); p != nil {
			p.ch, p.fg, p.bold = r, sceneLerp(p.bg, cafeHeading, fade), true
		}
	}
	cafeSteamWisps(c, f, kx, l.linkY-1, 1, fade)

	open, copyF := supportOpenField(0), supportCopyField(0)
	if l.stack {
		cafeButton(c, l.cx, l.openY, cafeBuyLabel, s.focusedField == open, fade)
		cafeButton(c, l.cx, l.copyY, cafeCopyLabel, s.focusedField == copyF, fade)
	} else {
		bw1, bw2 := len(cafeBuyLabel)+4, len(cafeCopyLabel)+4
		x := l.cx - (bw1+2+bw2)/2
		cafeButton(c, x+bw1/2, l.openY, cafeBuyLabel, s.focusedField == open, fade)
		cafeButton(c, x+bw1+2+bw2/2, l.copyY, cafeCopyLabel, s.focusedField == copyF, fade)
	}
	text(l.urlY, link.URL, cafeMuted, false)
	text(l.thanksY, "Thank you for supporting Tidemail -allie", cafeMuted, false)
	pulse := 0.5 + 0.5*math.Sin(f*0.22)
	text(l.heart, "♥", sceneLerp(cafeHeartLo, cafeHeartHi, pulse), true)

	if l.counter {
		cafeChase(c, s.supportPulseFrame, h-2)
	}

	return settingsSectionBody{
		lines: c.lines(),
		anchors: map[settingsField]int{
			open:  l.openY,
			copyF: l.copyY,
		},
	}
}

// cafeLights hangs a drooping wire of twinkling bulbs across the top.
func cafeLights(c *sceneCanvas, f float64) {
	sway := 0.4 * math.Sin(f*0.05)
	for x := range c.w {
		py := 2 + int(math.Round((3.2+sway)*math.Sin(math.Pi*float64(x%40)/40)))
		c.plot(x, py, cafeWire)
		if x%5 != 2 {
			continue
		}
		i := x / 5
		tw := 0.55 + 0.45*math.Sin(f*0.15+sceneRand(i)*6.28)
		b := cafeBulbs[i%len(cafeBulbs)]
		y := py/2 + 1
		if p := c.at(x, y); p != nil {
			c.put(x, y, '●', sceneLerp(p.bg, b, tw*sceneEase(f, sceneRand(i)*15, sceneRand(i)*15+8)))
		}
		if p := c.at(x, y+1); p != nil {
			p.bg = sceneLerp(p.bg, b, 0.08*tw)
		}
	}
}

// cafeMug draws a big mug of coffee (with latte art) on a saucer at (mx,
// my), with steam curling up off it. About every 12 seconds the steam drifts
// into a heart for a moment.
func cafeMug(c *sceneCanvas, f float64, mx, my int) {
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
			col := cafeCream
			switch {
			case ch == '▓':
				col = cafeCoffee
			case i >= 9 && i <= 10 && r >= 2 && r <= 4:
				col = cafeMugShade
			}
			c.put(mx+i, my+r, ch, col)
		}
	}
	c.put(mx+5, my+1, '♥', cafeCream) // latte art
	for i := -2; i < 16; i++ {
		c.put(mx+i, my+6, '▀', cafeSaucer)
	}

	cycle := float64(int(f) % cafeSteamHeartEvery)
	heart := sceneEase(cycle, 70, 85) * (1 - sceneEase(cycle, 105, 118))
	sx := mx + 5
	for k := range 3 {
		for s := range 7 {
			px := sx - 2 + k*2 + int(math.Round(math.Sin(f*0.25+float64(s)*0.9+float64(k)*2.1)*1.2))
			py := my - 1 - s
			if p := c.at(px, py); p != nil && p.ch == ' ' {
				a := (1 - float64(s)/7) * 0.7 * (1 - heart) * sceneEase(f, 10, 25)
				c.put(px, py, []rune("~∽∿")[(int(f)/3+s+k)%3], sceneLerp(p.bg, cafeSteam, a))
			}
		}
	}
	if heart <= 0 {
		return
	}
	hcx, hcy := float64(sx), float64((my-5)*2)
	for py := int(hcy) - 7; py <= int(hcy)+7; py++ {
		for x := sx - 6; x <= sx+6; x++ {
			hx := (float64(x) - hcx) / 5.5
			hy := -(float64(py) - hcy) / 6
			t := hx*hx + hy*hy - 1
			if t*t*t-hx*hx*hy*hy*hy <= 0 {
				if p := c.at(x, py/2); p != nil {
					c.plot(x, py, sceneLerp(p.bg, cafeSteam, heart*0.55))
				}
			}
		}
	}
}

// cafeSteamWisps curls two short wisps up from a cup at (x, y).
func cafeSteamWisps(c *sceneCanvas, f float64, x, y, height int, fade float64) {
	for k := range 2 {
		for s := range height {
			px := x + k + int(math.Round(math.Sin(f*0.3+float64(s)*1.3+float64(k)*2.4)*0.8))
			py := y - s
			if p := c.at(px, py); p != nil && p.ch == ' ' {
				c.put(px, py, []rune("~∽∿")[(int(f)/3+s+k)%3], sceneLerp(p.bg, cafeSteam, fade*(0.75-float64(s)*0.3)))
			}
		}
	}
}

// cafeButton draws a rounded button centered on cx. The focused one is
// filled with the accent so keyboard focus reads at a glance, and is drawn
// at full strength even while the page fades in.
func cafeButton(c *sceneCanvas, cx, y int, label string, focused bool, fade float64) {
	bw := len([]rune(label)) + 4
	x0 := cx - bw/2
	if focused {
		fade = 1
	}
	for dy := range 3 {
		for dx := range bw {
			p := c.at(x0+dx, y+dy)
			if p == nil {
				continue
			}
			edge := sceneLerp(p.bg, cafeEdge, fade)
			if focused {
				edge = cafeAccent
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
			switch {
			case ch != 0:
				c.put(x0+dx, y+dy, ch, edge)
			case focused:
				p.ch, p.bg = ' ', cafeAccent
			default:
				p.ch = ' '
			}
		}
	}
	for i, r := range label {
		if p := c.at(x0+2+i, y+1); p != nil {
			if focused {
				p.ch, p.fg, p.bold = r, cafeAccentFg, true
			} else {
				p.ch, p.fg = r, sceneLerp(p.bg, cafeText, fade)
			}
		}
	}
}

// cafeChaseAt returns where Pac-Man and the cup are on a counter of the
// given width at a frame. The cup always runs cafeChaseGap columns ahead,
// so Pac-Man never quite catches it.
func cafeChaseAt(frame, width int) (pac, cup int) {
	period := width + cafeChaseGap + 6
	pac = (frame/2)%period - cafeChaseGap - 3
	return pac, pac + cafeChaseGap
}

// cafeChase draws the counter edge at row y and, on the row below, Pac-Man
// chomping dots as he chases a runaway coffee cup.
func cafeChase(c *sceneCanvas, frame, y int) {
	for x := range c.w {
		if p := c.at(x, y); p != nil {
			c.put(x, y, '▔', cafeCounter)
		}
		if p := c.at(x, y+1); p != nil {
			p.bg = cafeFloor
		}
	}
	pac, cup := cafeChaseAt(frame, c.w)
	for x := 1; x < c.w; x += 3 {
		if x > pac && (x < cup || x > cup+1) {
			c.put(x, y+1, '·', cafeDot)
		}
	}
	c.putWide(cup, y+1, '☕', cafeCup)
	glyph := '𜱭' // facing right, mouth open
	if (frame/2)%2 == 1 {
		glyph = '●'
	}
	c.put(pac, y+1, glyph, cafePacYellow)
}
