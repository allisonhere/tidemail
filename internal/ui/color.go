package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// hexToRGB parses a #rrggbb color into [0,1] float components.
func hexToRGB(c lipgloss.Color) (r, g, b float64, ok bool) {
	s := strings.TrimPrefix(string(c), "#")
	if len(s) != 6 {
		return
	}
	ri, e1 := strconv.ParseUint(s[0:2], 16, 8)
	gi, e2 := strconv.ParseUint(s[2:4], 16, 8)
	bi, e3 := strconv.ParseUint(s[4:6], 16, 8)
	if e1 != nil || e2 != nil || e3 != nil {
		return
	}
	return float64(ri) / 255, float64(gi) / 255, float64(bi) / 255, true
}

func srgbLinearize(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// luminance returns the WCAG relative luminance of a hex color.
func luminance(c lipgloss.Color) float64 {
	r, g, b, ok := hexToRGB(c)
	if !ok {
		return 0
	}
	return 0.2126*srgbLinearize(r) + 0.7152*srgbLinearize(g) + 0.0722*srgbLinearize(b)
}

// isDark reports whether a color is perceptually dark.
func isDark(c lipgloss.Color) bool {
	return luminance(c) < 0.179
}

// contrastFg returns a light or dark foreground color for legible text on bg.
// Uses pure white/black to guarantee WCAG 4.5:1 contrast on any background.
func contrastFg(bg lipgloss.Color) lipgloss.Color {
	if isDark(bg) {
		return lipgloss.Color("#ffffff")
	}
	return lipgloss.Color("#000000")
}

func contrastRatio(a, b lipgloss.Color) float64 {
	la := luminance(a)
	lb := luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func readableText(preferred, bg lipgloss.Color, minRatio float64) lipgloss.Color {
	if preferred != "" && contrastRatio(preferred, bg) >= minRatio {
		return preferred
	}
	return contrastFg(bg)
}

// accentReadableOn returns a foreground accent that meets minRatio against bg,
// nudging HSL lightness (same hue) before falling back to readableText/contrastFg.
func accentReadableOn(accent, bg lipgloss.Color, minRatio float64) lipgloss.Color {
	if accent == "" {
		return ""
	}
	if _, _, _, ok := hexToRGB(accent); !ok {
		return accent
	}
	if contrastRatio(accent, bg) >= minRatio {
		return accent
	}
	const step = 0.06
	const maxSteps = 12
	cur := accent
	for range maxSteps {
		if isDark(bg) {
			cur = adjustLightness(cur, step)
		} else {
			cur = adjustLightness(cur, -step)
		}
		if contrastRatio(cur, bg) >= minRatio {
			return cur
		}
	}
	return readableText(accent, bg, minRatio)
}

func modalSurface(t Theme) lipgloss.Color {
	if isDark(t.Bg) {
		return adjustLightness(t.Bg, 0.06)
	}
	return adjustLightness(t.Bg, -0.06)
}

func focusLineBg(t Theme) lipgloss.Color {
	bg := t.Bg
	if bg == "" {
		return bg
	}
	const minRatio = 1.5
	if contrastRatio(bg, t.Bg) >= minRatio {
		return bg
	}
	step := 0.04
	if !isDark(t.Bg) {
		step = -step
	}
	cur := bg
	for range 16 {
		next := adjustLightness(cur, step)
		if next == cur {
			break
		}
		cur = next
		if contrastRatio(cur, t.Bg) >= minRatio {
			return cur
		}
	}
	if contrastRatio(t.Selected, t.Bg) >= minRatio {
		return t.Selected
	}
	return adjustLightness(t.Bg, step*16)
}

func mutedText(text, bg lipgloss.Color) lipgloss.Color {
	if isDark(bg) {
		candidate := adjustLightness(text, -0.20)
		if contrastRatio(candidate, bg) >= 3 {
			return candidate
		}
		return adjustLightness(text, -0.12)
	}
	candidate := adjustLightness(text, 0.20)
	if contrastRatio(candidate, bg) >= 3 {
		return candidate
	}
	return adjustLightness(text, 0.12)
}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	hi := math.Max(r, math.Max(g, b))
	lo := math.Min(r, math.Min(g, b))
	l = (hi + lo) / 2
	if hi == lo {
		return 0, 0, l
	}
	d := hi - lo
	if l > 0.5 {
		s = d / (2 - hi - lo)
	} else {
		s = d / (hi + lo)
	}
	switch hi {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	case b:
		h = (r-g)/d + 4
	}
	h /= 6
	return
}

func hue2rgb(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 0.5:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	default:
		return p
	}
}

func hslToRGB(h, s, l float64) (r, g, b float64) {
	if s == 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return hue2rgb(p, q, h+1.0/3), hue2rgb(p, q, h), hue2rgb(p, q, h-1.0/3)
}

// adjustLightness nudges a color's HSL lightness by delta (positive = lighter).
// Returns the original color unchanged on parse failure.
func adjustLightness(c lipgloss.Color, delta float64) lipgloss.Color {
	r, g, b, ok := hexToRGB(c)
	if !ok {
		return c
	}
	h, s, l := rgbToHSL(r, g, b)
	l = math.Max(0, math.Min(1, l+delta))
	nr, ng, nb := hslToRGB(h, s, l)
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
		uint8(math.Round(nr*255)),
		uint8(math.Round(ng*255)),
		uint8(math.Round(nb*255)),
	))
}
