package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Every scene must render at every size across its whole intro without
// panicking or spilling a line past the terminal width.
func TestSmokeAllScenes(t *testing.T) {
	sizes := [][2]int{{24, 10}, {60, 20}, {100, 30}, {160, 50}}
	for i := range scenes {
		for _, sz := range sizes {
			for f := 0; f < 400; f += 7 {
				m := model{w: sz[0], h: sz[1], f: f, scene: i}
				for n, line := range strings.Split(m.View(), "\n") {
					if w := ansi.StringWidth(line); w > sz[0] {
						t.Fatalf("%s at %v frame %d line %d: width %d", scenes[i].name, sz, f, n, w)
					}
				}
			}
		}
	}
}
