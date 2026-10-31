package ui

import (
	"math"
	"time"
)

// Settings → Support, "Café": a neon café at night. String lights sway along
// the top, a big mug steams (and every so often the steam drifts into a
// heart), and along the counter Pac-Man chases a runaway coffee cup, then
// catches it and bounces to rest in a caffeine rush. The
// Support copy and the sponsor buttons sit in the middle. Plain (VT52) themes
// keep the original dotted-frame Support page.

const (
	settingsSupportCafePeriod = 100 * time.Millisecond // smoother than the plain page's 240ms
	cafeBuyLabel              = "Buy me a coffee"
	cafeCopyLabel             = "Copy link"
	cafeChaseGap              = 8 // distance before the caffeine catch
	cafeCatchFrames           = 8
	cafeBurstFrames           = 24
	cafeHopFrames             = 100
	cafePauseFrames           = 10
	cafeSweepFrames           = 40
	cafeSteamHeartEvery       = 120
)

// Midnight indigo with cyan, lilac, and pink neon; cool porcelain highlights.
var (
	cafeBgTop     = sceneHex("#090e24")
	cafeBgBot     = sceneHex("#21143d")
	cafeWire      = sceneHex("#555780")
	cafeHeading   = sceneHex("#79e8f2")
	cafeText      = sceneHex("#eef0ff")
	cafeMuted     = sceneHex("#b4b9da")
	cafeEdge      = sceneHex("#9484d6")
	cafeAccent    = sceneHex("#79e8f2")
	cafeAccentFg  = sceneHex("#0d122c")
	cafeHeartLo   = sceneHex("#a855c7")
	cafeHeartHi   = sceneHex("#ff83c8")
	cafeSteam     = sceneHex("#c5d8ff")
	cafeCup       = sceneHex("#e0faff")
	cafeCream     = sceneHex("#e5e6ff")
	cafeMugShade  = sceneHex("#939bd5")
	cafeCoffee    = sceneHex("#514181")
	cafeSaucer    = sceneHex("#b7b0f4")
	cafeCounter   = sceneHex("#7264b5")
	cafeFloor     = sceneHex("#0d122c")
	cafeDot       = sceneHex("#a9a1ff")
	cafePacYellow = sceneFromColor(supportPacmanYellow)
	cafeBulbs     = []sceneRGB{sceneHex("#79e8f2"), sceneHex("#c4a2ff"), sceneHex("#ff83c8"), sceneHex("#8faaff")}
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
// given width at a frame during the ordinary chase.
func cafeChaseAt(frame, width int) (pac, cup int) {
	period := width + cafeChaseGap + 6
	pac = (frame/2)%period - cafeChaseGap - 3
	return pac, pac + cafeChaseGap
}

// Catch at the middle of the counter after the first full lap. The extra
// approach frames let both sprites reach the center without teleporting.
func cafeCatchX(width int) int { return min(width/2, max(0, width-2)) }

func cafeChaseFrames(width int) int {
	return 2*(width+cafeChaseGap+6) + 2*(cafeCatchX(width)+3)
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
	// One complete pass, then catch, burst, damped hops, a pause, and broom cleanup.
	chaseFrames := cafeChaseFrames(c.w)
	cycle := chaseFrames + cafeCatchFrames + cafeBurstFrames + cafeHopFrames + cafePauseFrames + cafeSweepFrames
	frame %= cycle
	if frame >= chaseFrames+cafeCatchFrames {
		effect := frame - chaseFrames - cafeCatchFrames
		if effect < cafeBurstFrames {
			cafeBurst(c, cafeCatchX(c.w), y+1, effect)
		} else if flight := effect - cafeBurstFrames; flight < cafeHopFrames {
			cafeHop(c, flight)
		} else if effect < cafeBurstFrames+cafeHopFrames+cafePauseFrames {
			cafeHop(c, cafeHopFrames-1)
		} else {
			cafeSweep(c, effect-cafeBurstFrames-cafeHopFrames-cafePauseFrames)
		}
		return
	}
	pac, cup := cafeChaseAt(frame, c.w)
	if frame >= chaseFrames {
		cup = cafeCatchX(c.w)
		pac = cup - cafeChaseGap + cafeChaseGap*(frame-chaseFrames)/cafeCatchFrames
	}
	for x := 1; x < c.w; x += 3 {
		if x > pac && (x < cup || x > cup+1) {
			c.put(x, y+1, '·', cafeDot)
		}
	}
	c.putWide(cup, y+1, '☕', cafeCup)
	cafePac(c, pac, y+1, frame)
}

func cafePac(c *sceneCanvas, x, y, frame int) {
	glyph := '𜱭'
	if (frame/2)%2 == 1 {
		glyph = '●'
	}
	cafeEffectPut(c, x, y, glyph, cafePacYellow)
}

// Staggered particle shells: a bright core, expanding shock ring, then
// ballistic sparks that arc downward under gravity instead of blinking out.
func cafeBurst(c *sceneCanvas, x, y, frame int) {
	t := float64(frame) * 0.05 // half-speed particles, still snapped to terminal cells
	for i := range 48 {
		delay := float64(i%3) * 0.1
		age := t - delay
		if age < 0 {
			continue
		}
		angle := -math.Pi * float64(i) / 47
		speed := 7 + sceneRand(i+71)*15
		dx := math.Cos(angle) * speed * age
		dy := math.Sin(angle)*speed*age + 12*age*age
		px, py := x+int(math.Round(dx*2)), y+int(math.Round(dy))
		if c.at(px, py) != nil {
			glyph := '·'
			if age < 0.3 {
				glyph = '✦'
			} else if age < 0.7 {
				glyph = '*'
			}
			col := cafeBulbs[i%len(cafeBulbs)]
			if age < 0.15 {
				col = cafeText
			}
			cafeEffectPut(c, px, py, glyph, sceneLerp(col, sceneHex("#ffffff"), sceneEase(age, 0.3, 1)))
		}
	}
	// A second, faster shock ring breaks away from the particle cloud.
	for i := range 20 {
		angle := -math.Pi * float64(i) / 19
		radius := t * 20
		px := x + int(math.Round(math.Cos(angle)*radius*2))
		py := y + int(math.Round(math.Sin(angle)*radius))
		if c.at(px, py) != nil {
			cafeEffectPut(c, px, py, '·', sceneLerp(cafeAccent, sceneHex("#ffffff"), sceneEase(t, 0.2, 0.9)))
		}
	}
}

type cafeHopState struct {
	x, y, vx, vy float64
	grounded     bool
}

// Replay fixed 10 ms physics steps from launch: gravity, inelastic collisions,
// air drag, and floor friction. Replay keeps rendering deterministic on resize
// and prevents dropped UI frames from changing the trajectory.
func cafeHopAt(frame, width, height int) cafeHopState {
	floor := float64(max(0, height-1))
	right := float64(max(0, width-1))
	gravity := math.Max(1, floor*0.6)
	state := cafeHopState{
		x: float64(cafeCatchX(width)), y: floor,
		vx: right * 0.22, vy: -math.Sqrt(2 * gravity * floor * 0.72),
		grounded: height <= 1,
	}
	const dt = 0.01
	for step := 0; step < max(0, frame)*10; step++ {
		state.vx *= math.Exp(-0.3 * dt)
		state.x += state.vx * dt
		if state.x > right {
			state.x = right
			state.vx = -state.vx * 0.65
		}
		if state.x < 0 {
			state.x = 0
			state.vx = -state.vx * 0.65
		}
		if state.grounded {
			state.vx *= math.Exp(-4 * dt)
			if math.Abs(state.vx) < 0.08 {
				state.vx = 0
			}
			continue
		}
		state.y += state.vy*dt + 0.5*gravity*dt*dt
		state.vy += gravity * dt
		if state.y < 0 {
			state.y = 0
			state.vy = math.Abs(state.vy) * 0.56
		}
		if state.y >= floor && state.vy > 0 {
			state.y = floor
			state.vy = -state.vy * 0.56
			state.vx *= 0.68
			if state.vy*state.vy/(2*gravity) < 0.15 {
				state.vy = 0
				state.grounded = true
			}
		}
	}
	return state
}

func cafeHop(c *sceneCanvas, frame int) {
	state := cafeHopAt(frame, c.w, c.h)
	// A short trail follows the initial hop; the last bounces settle quietly.
	if !state.grounded && frame < 30 {
		for age := 3; age > 0; age-- {
			if frame < age {
				continue
			}
			old := cafeHopAt(frame-age, c.w, c.h)
			x, y := int(math.Round(old.x)), int(math.Round(old.y))
			if p := c.at(x, y); p != nil {
				cafeEffectPut(c, x, y, '·', sceneLerp(p.bg, cafeBulbs[age%len(cafeBulbs)], float64(4-age)/5))
			}
		}
	}
	if state.grounded {
		frame = 0
	}
	cafePac(c, int(math.Round(state.x)), int(math.Round(state.y)), frame)
}

// Effects may cross the heading's double-width coffee symbol. Clear both
// halves before replacing either cell so the rendered row stays aligned.
func cafeEffectPut(c *sceneCanvas, x, y int, glyph rune, col sceneRGB) {
	p := c.at(x, y)
	if p == nil {
		return
	}
	if p.ch == sceneWideCont {
		if left := c.at(x-1, y); left != nil {
			left.ch = ' '
		}
	} else if right := c.at(x+1, y); right != nil && right.ch == sceneWideCont {
		right.ch = ' '
	}
	c.put(x, y, glyph, col)
}

// A two-cell broom enters from the left. Its leading edge pushes the resting
// Pac-Man, and both exit completely before the next chase begins.
func cafeSweepAt(frame, width, height int) (broom, pac int) {
	broom = -2 + int(math.Round(float64(width+4)*float64(frame)/float64(cafeSweepFrames-1)))
	resting := cafeHopAt(cafeHopFrames-1, width, height)
	pac = max(int(math.Round(resting.x)), broom+2)
	return broom, pac
}

func cafeSweep(c *sceneCanvas, frame int) {
	broom, pac := cafeSweepAt(frame, c.w, c.h)
	floor := c.h - 1
	cafePac(c, pac, floor, 0)
	c.putWide(broom, floor, '🧹', cafeText)
	// Tiny puffs sell the sweeping motion without leaving permanent debris.
	if frame%4 < 2 {
		if p := c.at(broom-1, floor); p != nil {
			cafeEffectPut(c, broom-1, floor, '·', sceneLerp(p.bg, cafeText, 0.55))
		}
	}
}
