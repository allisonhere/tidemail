package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestRoundGlyphPillsSelection(t *testing.T) {
	cfg := config.DefaultConfig()
	s := newSettings(cfg, settingsUpdateState{})
	s.tagPickerIdx = tagAppearanceIndex(config.TagStyleGlyphPills, config.TagEndsRound)
	if tagAppearanceOptions[s.tagPickerIdx].label != "Glyph Pills (Round)" {
		t.Fatal("round glyph option missing")
	}
	s.applyTagAppearanceSelection()
	cfg = s.ApplyTo(cfg)
	if cfg.Display.TagStyle != config.TagStyleGlyphPills || cfg.Display.TagEnds != config.TagEndsRound {
		t.Fatalf("saved style/ends: %s/%s", cfg.Display.TagStyle, cfg.Display.TagEnds)
	}
	if reopened := newSettings(cfg, settingsUpdateState{}); reopened.tagPickerIdx != s.tagPickerIdx {
		t.Fatal("selection did not survive reopening settings")
	}
	for _, tc := range []struct{ category, glyph string }{
		{"github", "⚙"}, {"newsletter", "✉"}, {"custom", "●"},
	} {
		t.Run(tc.category, func(t *testing.T) {
			tags := []annotationTag{{kind: tagCategory, category: tc.category}}
			chips := layoutTags(tags, cfg.Display.TagStyle, cfg.Display.TagEnds, true, 100)
			out := renderChipsWith(chips, tagExampleColor, "#000000", lipgloss.NewStyle())
			if got := ansi.Strip(out); got != pillCapLeft+tc.glyph+pillCapRight {
				t.Fatalf("rendered %q", got)
			}
			for budget := 0; budget < 8; budget++ {
				chips = layoutTags(tags, cfg.Display.TagStyle, cfg.Display.TagEnds, true, budget)
				if chipsWidth(chips) > budget {
					t.Fatalf("overflow at budget %d", budget)
				}
			}
		})
	}
	preview := ansi.Strip(renderTagExample(cfg.Display.TagStyle, cfg.Display.TagEnds, true, "#000000"))
	if preview != pillCapLeft+"⚙"+pillCapRight {
		t.Fatalf("preview = %q", preview)
	}
	if fallback := renderTagExample(cfg.Display.TagStyle, cfg.Display.TagEnds, false, "#000000"); !strings.Contains(fallback, "G") {
		t.Fatal("missing ASCII fallback")
	}
}
