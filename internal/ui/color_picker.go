package ui

// A visual color picker for the Tag colors editor (see tag_colors.go),
// ported from the RGB-sliders / HSL-field picker in zellit
// (github.com/allisonhere/zellit, src/ui/color_picker.rs). Unlike that
// version this is keyboard-only — TideMail has no mouse handling anywhere,
// so drag targets and click-to-focus don't apply here.
//
// Colors always stay live in cp.rgb; every adjustment (slider, field, typed
// number) updates it immediately, mirroring the source's "no separate apply
// step" model. Closing the picker with Confirm writes cp.rgb to config;
// Cancel/Back discards it and leaves the tag color untouched.

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/allisonhere/tidemail/internal/clipboard"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Color math ───────────────────────────────────────────────────────────────

func normalizeHue360(h float64) float64 {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	return h
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round8(v float64) uint8 {
	return uint8(clampFloat(math.Round(v), 0, 255))
}

// hsvToRGB converts h in [0,360), s and v in [0,1] to 8-bit RGB.
func hsvToRGB(h, s, v float64) (uint8, uint8, uint8) {
	h = normalizeHue360(h)
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return round8((r + m) * 255), round8((g + m) * 255), round8((b + m) * 255)
}

// rgbToHSV converts 8-bit RGB to h in [0,360), s and v in [0,1].
func rgbToHSV(r, g, b uint8) (h, s, v float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	maxc := math.Max(rf, math.Max(gf, bf))
	minc := math.Min(rf, math.Min(gf, bf))
	v = maxc
	delta := maxc - minc
	if maxc <= 0 {
		return 0, 0, 0
	}
	s = delta / maxc
	if delta == 0 {
		return 0, s, v
	}
	switch maxc {
	case rf:
		h = 60 * math.Mod((gf-bf)/delta, 6)
	case gf:
		h = 60 * ((bf-rf)/delta + 2)
	default:
		h = 60 * ((rf-gf)/delta + 4)
	}
	return normalizeHue360(h), s, v
}

// hslToRGB8 converts h in [0,360), s and l in [0,1] to 8-bit RGB, building on
// color.go's own [0,1]-hue hslToRGB (used there for theme lightness/contrast
// adjustments) rather than a second HSL implementation.
func hslToRGB8(h, s, l float64) (uint8, uint8, uint8) {
	r, g, b := hslToRGB(normalizeHue360(h)/360, s, l)
	return round8(r * 255), round8(g * 255), round8(b * 255)
}

// rgbToHSL8 converts 8-bit RGB to h in [0,360), s and l in [0,1], building on
// color.go's own rgbToHSL.
func rgbToHSL8(r, g, b uint8) (h, s, l float64) {
	h, s, l = rgbToHSL(float64(r)/255, float64(g)/255, float64(b)/255)
	return h * 360, s, l
}

func parseHexRGB(hex string) (r, g, b uint8, ok bool) {
	norm, valid := config.NormalizeTagHex(hex)
	if !valid {
		return 0, 0, 0, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(norm, "#"), 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(n >> 16), uint8(n >> 8), uint8(n), true
}

// ── State ────────────────────────────────────────────────────────────────────

type colorPickerMode int

const (
	colorPickerModeRGB colorPickerMode = iota
	colorPickerModeHSL
)

type colorPickerFocusKind int

const (
	cpFocusMode        colorPickerFocusKind = iota
	cpFocusRGBSlider                        // RGB mode only; idx 0-2 = R/G/B
	cpFocusField                            // HSL mode only; the 2D hue/saturation field
	cpFocusValueSlider                      // HSL mode only; brightness
	cpFocusHex
	cpFocusRGBField // idx 0-2 = R/G/B
	cpFocusHSLField // idx 0-2 = H/S/L
)

type colorPickerFocus struct {
	kind colorPickerFocusKind
	idx  int
}

// colorPicker is the picker's state for one bg/fg swatch of one tag color
// key. It holds three synchronized views of the same color — raw RGB, an
// HSL decomposition for the numeric H/S/L fields, and an HSV decomposition
// for the 2D field and value slider — because RGB<->HSL<->HSV round-trips
// lose the hue the instant saturation or value hits zero (pure gray or
// black). Each setter re-derives the others but preserves the previous hue
// through that zero, so the field cursor and H field don't jump to 0° the
// moment you drag into black.
type colorPicker struct {
	origin overlayMode
	key    string // tag color key, e.g. "reply" or "category.github"
	field  string // "bg" or "fg"

	mode  colorPickerMode
	focus colorPickerFocus

	rgb [3]uint8

	hue, sat, light float64 // HSL, for the numeric fields

	fieldHue, fieldSat, fieldVal float64 // HSV, for the 2D field + value slider

	editing   bool
	editWhich colorPickerFocus
	input     textinput.Model
	err       string
	message   string

	origRGB [3]uint8
}

func newColorPicker(origin overlayMode, key, field string, r, g, b uint8) colorPicker {
	cp := colorPicker{
		origin: origin,
		key:    key,
		field:  field,
		mode:   colorPickerModeRGB,
		focus:  colorPickerFocus{kind: cpFocusRGBSlider},
	}
	cp.setRGB(r, g, b)
	cp.origRGB = cp.rgb
	return cp
}

func (cp *colorPicker) syncHSLFromRGB() {
	h, s, l := rgbToHSL8(cp.rgb[0], cp.rgb[1], cp.rgb[2])
	if s <= 0.001 {
		h = cp.hue
	}
	cp.hue, cp.sat, cp.light = h, s*100, l*100
}

func (cp *colorPicker) setRGB(r, g, b uint8) {
	cp.rgb = [3]uint8{r, g, b}
	h, s, v := rgbToHSV(r, g, b)
	if s <= 0.001 || v <= 0.001 {
		h = cp.fieldHue
	}
	cp.fieldHue, cp.fieldSat, cp.fieldVal = h, s*100, v*100
	cp.syncHSLFromRGB()
}

func (cp *colorPicker) setHSL(h, s, l float64) {
	h, s, l = normalizeHue360(h), clampFloat(s, 0, 100), clampFloat(l, 0, 100)
	cp.hue, cp.sat, cp.light = h, s, l
	r, g, b := hslToRGB8(h, s/100, l/100)
	cp.rgb = [3]uint8{r, g, b}
	fh, fs, fv := rgbToHSV(r, g, b)
	if fs <= 0.001 || fv <= 0.001 {
		fh = cp.fieldHue
	}
	cp.fieldHue, cp.fieldSat, cp.fieldVal = fh, fs*100, fv*100
}

func (cp *colorPicker) setHSV(h, s, v float64) {
	h, s, v = normalizeHue360(h), clampFloat(s, 0, 100), clampFloat(v, 0, 100)
	cp.fieldHue, cp.fieldSat, cp.fieldVal = h, s, v
	r, g, b := hsvToRGB(h, s/100, v/100)
	cp.rgb = [3]uint8{r, g, b}
	cp.syncHSLFromRGB()
}

func (cp colorPicker) hex() string {
	return fmt.Sprintf("#%02x%02x%02x", cp.rgb[0], cp.rgb[1], cp.rgb[2])
}

func (cp *colorPicker) toggleMode() {
	cp.editing = false
	if cp.mode == colorPickerModeRGB {
		cp.mode = colorPickerModeHSL
		cp.focus = colorPickerFocus{kind: cpFocusField}
	} else {
		cp.mode = colorPickerModeRGB
		cp.focus = colorPickerFocus{kind: cpFocusRGBSlider}
	}
}

func (cp colorPicker) focusOrder() []colorPickerFocus {
	shared := []colorPickerFocus{
		{kind: cpFocusHex},
		{kind: cpFocusRGBField, idx: 0}, {kind: cpFocusRGBField, idx: 1}, {kind: cpFocusRGBField, idx: 2},
		{kind: cpFocusHSLField, idx: 0}, {kind: cpFocusHSLField, idx: 1}, {kind: cpFocusHSLField, idx: 2},
	}
	if cp.mode == colorPickerModeRGB {
		order := []colorPickerFocus{
			{kind: cpFocusMode},
			{kind: cpFocusRGBSlider, idx: 0}, {kind: cpFocusRGBSlider, idx: 1}, {kind: cpFocusRGBSlider, idx: 2},
		}
		return append(order, shared...)
	}
	order := []colorPickerFocus{
		{kind: cpFocusMode},
		{kind: cpFocusField},
		{kind: cpFocusValueSlider},
	}
	return append(order, shared...)
}

func (cp *colorPicker) focusNext(reverse bool) {
	cp.editing = false
	order := cp.focusOrder()
	idx := 0
	for i, f := range order {
		if f == cp.focus {
			idx = i
			break
		}
	}
	if reverse {
		idx = (idx - 1 + len(order)) % len(order)
	} else {
		idx = (idx + 1) % len(order)
	}
	cp.focus = order[idx]
}

func (cp *colorPicker) moveRGBSliderFocus(reverse bool) {
	idx := cp.focus.idx
	if reverse {
		idx = (idx - 1 + 3) % 3
	} else {
		idx = (idx + 1) % 3
	}
	cp.focus = colorPickerFocus{kind: cpFocusRGBSlider, idx: idx}
}

// adjustFocused nudges whatever numeric control has focus by delta; it
// reports false for controls with no numeric meaning (the mode toggle, the
// 2D field itself, the hex field), which the caller leaves untouched.
func (cp *colorPicker) adjustFocused(delta float64) bool {
	switch cp.focus.kind {
	case cpFocusRGBSlider, cpFocusRGBField:
		rgb := cp.rgb
		rgb[cp.focus.idx] = round8(clampFloat(float64(rgb[cp.focus.idx])+delta, 0, 255))
		cp.setRGB(rgb[0], rgb[1], rgb[2])
		return true
	case cpFocusHSLField:
		switch cp.focus.idx {
		case 0:
			cp.setHSL(cp.hue+delta, cp.sat, cp.light)
		case 1:
			cp.setHSL(cp.hue, cp.sat+delta, cp.light)
		case 2:
			cp.setHSL(cp.hue, cp.sat, cp.light+delta)
		}
		return true
	case cpFocusValueSlider:
		cp.setHSV(cp.fieldHue, cp.fieldSat, cp.fieldVal+delta)
		return true
	}
	return false
}

// nudgeField moves the 2D hue/saturation field cursor.
func (cp *colorPicker) nudgeField(deltaHue, deltaSat float64) {
	cp.setHSV(cp.fieldHue+deltaHue, cp.fieldSat+deltaSat, cp.fieldVal)
}

func (cp *colorPicker) startEditFocused() {
	var value string
	switch cp.focus.kind {
	case cpFocusHex:
		value = strings.TrimPrefix(cp.hex(), "#")
	case cpFocusRGBField:
		value = strconv.Itoa(int(cp.rgb[cp.focus.idx]))
	case cpFocusHSLField:
		switch cp.focus.idx {
		case 0:
			value = strconv.Itoa(int(math.Round(cp.hue)))
		case 1:
			value = strconv.Itoa(int(math.Round(cp.sat)))
		case 2:
			value = strconv.Itoa(int(math.Round(cp.light)))
		}
	default:
		return
	}
	in := textinput.New()
	in.CharLimit = 6
	in.SetValue(value)
	in.Focus()
	cp.input = in
	cp.editWhich = cp.focus
	cp.editing = true
	cp.err = ""
}

func (cp *colorPicker) startHexInput() {
	cp.focus = colorPickerFocus{kind: cpFocusHex}
	cp.startEditFocused()
}

func (cp *colorPicker) cancelEdit() {
	cp.editing = false
	cp.err = ""
}

func (cp *colorPicker) commitEdit() bool {
	raw := strings.TrimSpace(cp.input.Value())
	switch cp.editWhich.kind {
	case cpFocusHex:
		r, g, b, ok := parseHexRGB("#" + raw)
		if !ok {
			cp.err = "use rrggbb or rgb hex digits"
			return false
		}
		cp.setRGB(r, g, b)
	case cpFocusRGBField:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 255 {
			cp.err = "0-255"
			return false
		}
		rgb := cp.rgb
		rgb[cp.editWhich.idx] = uint8(n)
		cp.setRGB(rgb[0], rgb[1], rgb[2])
	case cpFocusHSLField:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			cp.err = "numbers only"
			return false
		}
		switch cp.editWhich.idx {
		case 0:
			cp.setHSL(n, cp.sat, cp.light)
		case 1:
			cp.setHSL(cp.hue, n, cp.light)
		case 2:
			cp.setHSL(cp.hue, cp.sat, n)
		}
	}
	cp.editing = false
	cp.err = ""
	return true
}

// displayValue is what a field shows when it isn't the one currently being
// typed into.
func (cp colorPicker) displayValue(f colorPickerFocus) string {
	switch f.kind {
	case cpFocusHex:
		return strings.TrimPrefix(cp.hex(), "#")
	case cpFocusRGBField:
		return strconv.Itoa(int(cp.rgb[f.idx]))
	case cpFocusHSLField:
		switch f.idx {
		case 0:
			return strconv.Itoa(int(math.Round(cp.hue)))
		case 1:
			return strconv.Itoa(int(math.Round(cp.sat)))
		case 2:
			return strconv.Itoa(int(math.Round(cp.light)))
		}
	}
	return ""
}

func (cp *colorPicker) handleLeftRight(dir int, big bool) {
	switch cp.focus.kind {
	case cpFocusMode:
		cp.toggleMode()
	case cpFocusRGBSlider, cpFocusRGBField:
		delta := 5.0
		if big {
			delta = 20
		}
		cp.adjustFocused(float64(dir) * delta)
	case cpFocusField:
		delta := 4.0
		if big {
			delta = 15
		}
		cp.nudgeField(float64(dir)*delta, 0)
	case cpFocusHSLField:
		delta := 2.0
		if big {
			delta = 10
		}
		cp.adjustFocused(float64(dir) * delta)
	}
}

func (cp *colorPicker) handleUpDown(dir int, big bool) {
	switch cp.focus.kind {
	case cpFocusRGBSlider:
		cp.moveRGBSliderFocus(dir > 0)
	case cpFocusField:
		delta := 4.0
		if big {
			delta = 15
		}
		cp.nudgeField(0, float64(dir)*delta)
	case cpFocusValueSlider:
		delta := 4.0
		if big {
			delta = 15
		}
		cp.adjustFocused(float64(dir) * delta)
	case cpFocusRGBField:
		delta := 5.0
		if big {
			delta = 20
		}
		cp.adjustFocused(float64(dir) * delta)
	case cpFocusHSLField:
		delta := 2.0
		if big {
			delta = 10
		}
		cp.adjustFocused(float64(dir) * delta)
	}
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m Model) openColorPicker(origin overlayMode, key, field string, r, g, b uint8) (tea.Model, tea.Cmd) {
	m.colorPicker = newColorPicker(origin, key, field, r, g, b)
	m.overlay = overlayColorPicker
	return m, nil
}

func (m Model) handleColorPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cp := &m.colorPicker
	cp.message = ""
	if cp.editing {
		switch {
		case keyMatches(msg, m.keys.Cancel):
			cp.cancelEdit()
		case keyMatches(msg, m.keys.Confirm):
			cp.commitEdit()
		default:
			var cmd tea.Cmd
			cp.input, cmd = cp.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	switch {
	case keyMatches(msg, m.keys.Cancel, m.keys.Back):
		m.overlay = cp.origin
	case keyMatches(msg, m.keys.Confirm):
		switch cp.focus.kind {
		case cpFocusHex, cpFocusRGBField, cpFocusHSLField:
			cp.startEditFocused()
		case cpFocusMode:
			cp.toggleMode()
		default:
			m.setTagColor(cp.key, cp.field, cp.hex())
			m.overlay = cp.origin
		}
	case msg.String() == "shift+left":
		cp.handleLeftRight(-1, true)
	case msg.String() == "shift+right":
		cp.handleLeftRight(1, true)
	case msg.String() == "shift+up":
		cp.handleUpDown(1, true)
	case msg.String() == "shift+down":
		cp.handleUpDown(-1, true)
	case keyMatches(msg, m.keys.Left):
		cp.handleLeftRight(-1, false)
	case keyMatches(msg, m.keys.Right):
		cp.handleLeftRight(1, false)
	case keyMatches(msg, m.keys.Up):
		cp.handleUpDown(1, false)
	case keyMatches(msg, m.keys.Down):
		cp.handleUpDown(-1, false)
	case msg.Type == tea.KeyTab:
		cp.focusNext(false)
	case msg.Type == tea.KeyShiftTab:
		cp.focusNext(true)
	case msg.String() == "pgup":
		cp.adjustFocused(10)
	case msg.String() == "pgdown":
		cp.adjustFocused(-10)
	case msg.String() == "m":
		cp.toggleMode()
	case msg.String() == "#":
		cp.startHexInput()
	case msg.String() == "y":
		_ = clipboard.Copy(cp.hex())
		cp.message = "copied " + cp.hex()
	}
	return m, nil
}

// ── Render ───────────────────────────────────────────────────────────────────

const (
	cpFieldW = 5
	cpHexW   = 7
)

func (m Model) renderColorPicker() string {
	winW := max(1, min(m.width-4, 76))
	chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
	cp := m.colorPicker
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	muted := base.Foreground(chrome.muted)
	bodyW := max(1, winW-4)

	title := tagColorLabel(cp.key) + " · " + map[string]string{"bg": "background", "fg": "foreground"}[cp.field]
	lines := []string{
		muted.Render(truncate(title, bodyW)),
		"",
		m.renderColorPickerHeader(cp, chrome, bodyW),
		"",
	}
	if cp.mode == colorPickerModeRGB {
		lines = append(lines, m.renderRGBSliders(cp, chrome, bodyW)...)
	} else {
		lines = append(lines, m.renderHSLField(cp, chrome)...)
	}
	lines = append(lines, "")
	lines = append(lines, m.renderColorPickerFields(cp, chrome)...)

	if cp.editing && cp.err != "" {
		lines = append(lines, base.Foreground(chrome.errorFg).Render(truncate(cp.err, bodyW)))
	} else if cp.message != "" {
		lines = append(lines, base.Foreground(chrome.successFg).Render(truncate(cp.message, bodyW)))
	} else {
		lines = append(lines, "")
	}

	body := lipgloss.NewStyle().Background(chrome.baseBg).Width(winW).Padding(1, 2).Render(strings.Join(lines, "\n"))
	pairs := []string{"tab", "next", "↑↓←→", "adjust", "enter", "edit/apply", "m", "mode", "#", "hex", "y", "copy", "esc", "cancel"}
	if cp.editing {
		pairs = []string{"enter", "save", "esc", "cancel"}
	}
	hints := renderSoftHints(winW, chrome, pairs...)
	inner := lipgloss.JoinVertical(lipgloss.Left, body, hints)
	inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
	return renderSoftPanelBox(inner, winW, "tidemail", "color picker", chrome)
}

// renderColorPickerHeader draws the RGB/HSL mode toggle on the left and a
// preview swatch + hex value on the right.
func (m Model) renderColorPickerHeader(cp colorPicker, chrome managerChrome, width int) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	modeFocused := cp.focus.kind == cpFocusMode
	pill := func(label string, active bool) string {
		fg, bg := chrome.muted, chrome.baseBg
		if active {
			fg, bg = chrome.accentFg, chrome.accent
		}
		style := base.Foreground(fg).Background(bg).Bold(active)
		if active && modeFocused {
			style = style.Underline(true)
		}
		return style.Render(" " + label + " ")
	}
	left := pill("RGB", cp.mode == colorPickerModeRGB) + " " + pill("HSL", cp.mode == colorPickerModeHSL)

	swatchColor := lipgloss.Color(cp.hex())
	swatchFg := contrastFg(swatchColor)
	swatch := lipgloss.NewStyle().Background(swatchColor).Foreground(swatchFg).Bold(true).Render("      ")
	hexText := base.Foreground(chrome.text).Bold(cp.focus.kind == cpFocusHex).Render(cp.hex())
	right := swatch + " " + hexText

	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + base.Render(strings.Repeat(" ", gap)) + right
}

func (m Model) renderRGBSliders(cp colorPicker, chrome managerChrome, width int) []string {
	labels := [3]string{"R", "G", "B"}
	barW := max(4, width-10)
	lines := make([]string, 3)
	for i := range 3 {
		focused := cp.focus.kind == cpFocusRGBSlider && cp.focus.idx == i
		rail := softRail(chrome, focused, chrome.baseBg)
		filled := int(math.Round(float64(cp.rgb[i]) / 255 * float64(barW)))
		filled = max(0, min(barW, filled))
		fillColor := [3]lipgloss.Color{"#ff6b6b", "#5ce488", "#5b9dff"}[i]
		bar := lipgloss.NewStyle().Foreground(fillColor).Render(strings.Repeat("█", filled)) +
			lipgloss.NewStyle().Foreground(chrome.muted).Render(strings.Repeat("░", barW-filled))
		label := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Bold(focused).Render(labels[i])
		value := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Render(padLeft(strconv.Itoa(int(cp.rgb[i])), 3))
		lines[i] = rail + label + " " + lipgloss.NewStyle().Background(chrome.baseBg).Render(bar) + " " + value
	}
	return lines
}

const (
	hslFieldW = 34
	hslFieldH = 7
)

func (m Model) renderHSLField(cp colorPicker, chrome managerChrome) []string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	fieldFocused := cp.focus.kind == cpFocusField
	valueFocused := cp.focus.kind == cpFocusValueSlider

	cursorCol := int(math.Round(cp.fieldHue / 360 * float64(hslFieldW-1)))
	cursorRow := int(math.Round((100 - cp.fieldSat) / 100 * float64(hslFieldH-1)))
	markerRow := int(math.Round((100 - cp.fieldVal) / 100 * float64(hslFieldH-1)))

	lines := make([]string, hslFieldH)
	for y := 0; y < hslFieldH; y++ {
		sat := 100 - float64(y)/float64(hslFieldH-1)*100
		var row strings.Builder
		for x := 0; x < hslFieldW; x++ {
			hue := float64(x) / float64(hslFieldW-1) * 359.999
			r, g, b := hsvToRGB(hue, sat/100, cp.fieldVal/100)
			cellBg := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
			if fieldFocused && x == cursorCol && y == cursorRow {
				row.WriteString(lipgloss.NewStyle().Background(cellBg).Foreground(contrastFg(cellBg)).Bold(true).Render("◆"))
			} else if x == cursorCol && y == cursorRow {
				row.WriteString(lipgloss.NewStyle().Background(cellBg).Foreground(contrastFg(cellBg)).Render("○"))
			} else {
				row.WriteString(lipgloss.NewStyle().Background(cellBg).Render(" "))
			}
		}
		sliderCell := "  "
		vr, vg, vb := hsvToRGB(cp.fieldHue, cp.fieldSat/100, (100-float64(y)/float64(hslFieldH-1)*100)/100)
		sliderBg := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", vr, vg, vb))
		sliderStyle := lipgloss.NewStyle().Background(sliderBg)
		if y == markerRow {
			marker := "◀◀"
			if valueFocused {
				sliderStyle = sliderStyle.Foreground(contrastFg(sliderBg)).Bold(true)
			} else {
				sliderStyle = sliderStyle.Foreground(contrastFg(sliderBg))
			}
			sliderCell = marker
		}
		lines[y] = base.Render(" ") + row.String() + base.Render(" ") + sliderStyle.Render(sliderCell)
	}
	return lines
}

func (m Model) renderColorPickerFields(cp colorPicker, chrome managerChrome) []string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)

	field := func(label string, f colorPickerFocus, w int) string {
		focused := cp.focus == f
		var box string
		if cp.editing && cp.editWhich == f {
			box = renderTextInput(cp.input, w, true, false, chrome)
		} else {
			style := base.Foreground(chrome.muted)
			if focused {
				style = base.Foreground(chrome.text).Bold(true)
			}
			box = style.Width(w).Render(padRight(cp.displayValue(f), w))
		}
		lbl := base.Foreground(chrome.muted)
		if focused {
			lbl = base.Foreground(chrome.text)
		}
		return lbl.Render(label+" ") + box
	}

	hexRow := field("hex", colorPickerFocus{kind: cpFocusHex}, cpHexW)
	rgbRow := field("R", colorPickerFocus{kind: cpFocusRGBField, idx: 0}, cpFieldW) + "  " +
		field("G", colorPickerFocus{kind: cpFocusRGBField, idx: 1}, cpFieldW) + "  " +
		field("B", colorPickerFocus{kind: cpFocusRGBField, idx: 2}, cpFieldW)
	hslRow := field("H", colorPickerFocus{kind: cpFocusHSLField, idx: 0}, cpFieldW) + "  " +
		field("S", colorPickerFocus{kind: cpFocusHSLField, idx: 1}, cpFieldW) + "  " +
		field("L", colorPickerFocus{kind: cpFocusHSLField, idx: 2}, cpFieldW)

	return []string{hexRow, rgbRow, hslRow}
}
