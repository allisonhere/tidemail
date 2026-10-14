package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSmokeAllConcepts(t *testing.T) {
	sizes := [][2]int{{24, 10}, {60, 20}, {100, 30}, {160, 50}}
	for concept := range concepts {
		for _, sz := range sizes {
			for f := 0; f < 400; f += 7 {
				m := model{w: sz[0], h: sz[1], f: f, concept: concept}
				out := m.View()
				for i, line := range strings.Split(out, "\n") {
					if w := ansi.StringWidth(line); w > sz[0] {
						t.Fatalf("concept %d size %v frame %d line %d width %d", concept, sz, f, i, w)
					}
				}
			}
		}
	}
}
