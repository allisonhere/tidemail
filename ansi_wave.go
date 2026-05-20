//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	width      = 88
	height     = 28
	frameDelay = 95 * time.Millisecond
)

var ramp = []rune(" .,:;-~+=*#%@")

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func colorRGB(r, g, b int, s string) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, s)
}

func hideCursor() { fmt.Print("\x1b[?25l") }
func showCursor() { fmt.Print("\x1b[?25h") }
func clearHome()  { fmt.Print("\x1b[2J\x1b[H") }

func main() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)

	hideCursor()
	defer showCursor()
	clearHome()

	t := 0.0

	for {
		select {
		case <-sigc:
			fmt.Print("\x1b[0m\x1b[2J\x1b[H")
			return
		default:
		}

		var b strings.Builder
		b.WriteString("\x1b[H")

		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				xf := float64(x)
				yf := float64(y)

				// Layered wave field for smoother, more natural motion.
				base := 11.5 +
					3.6*math.Sin((xf*0.16)-(t*0.75)) +
					1.8*math.Sin((xf*0.08)+(t*0.42)) +
					0.7*math.Sin((xf*0.035)-(t*0.23))

				ripple := 0.8 * math.Sin((xf*0.28)+(yf*0.65)-(t*1.1))
				surface := base + ripple

				depth := surface - yf
				intensity := clamp((depth+5.5)/11.0, 0, 1)

				var ch rune
				if depth > 1.35 && depth < 2.5 {
					ch = '≈'
				} else if depth > 0.45 && depth <= 1.35 {
					ch = '~'
				} else if depth > -0.15 && depth <= 0.45 {
					ch = '-'
				} else if intensity <= 0 {
					ch = ' '
				} else {
					idx := int(clamp(intensity*float64(len(ramp)-1), 0, float64(len(ramp)-1)))
					ch = ramp[idx]
				}

				// Water palette from deep blue to bright crest.
				var r, g, bl int
				switch {
				case depth > 1.35:
					r, g, bl = 240, 248, 255 // crest white-blue
				case depth > 0.45:
					r, g, bl = 170, 225, 255
				default:
					deep := clamp((yf/float64(height))*0.85+0.15, 0, 1)
					r = int(10 + 15*deep)
					g = int(90 + 85*deep)
					bl = int(150 + 95*deep)
				}

				b.WriteString(colorRGB(r, g, bl, string(ch)))
			}
			b.WriteByte('\n')
		}

		// Small title line
		b.WriteString(colorRGB(210, 230, 255, " gentle ocean ansi wave  •  ctrl+c to quit"))

		fmt.Print(b.String())
		time.Sleep(frameDelay)
		t += 0.22
	}
}
