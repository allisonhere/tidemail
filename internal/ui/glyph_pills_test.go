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

func TestTagIconsSelectionPreviewAndFallback(t *testing.T) {
	cfg := config.DefaultConfig()
	s := newSettings(cfg, settingsUpdateState{})
	if got := s.ApplyTo(cfg).Display.TagStyle; got != cfg.Display.TagStyle {
		t.Fatal("opening settings changed existing tag style")
	}
	s.tagPickerIdx = tagAppearanceIndex(config.TagStyleIcons, config.TagEndsSquare)
	s.applyTagAppearanceSelection()
	cfg = s.ApplyTo(cfg)
	if cfg.Display.TagStyle != config.TagStyleIcons || newSettings(cfg, settingsUpdateState{}).tagPickerIdx != s.tagPickerIdx {
		t.Fatal("tag icon preference did not survive reopening settings")
	}
	for _, icons := range []bool{true, false} {
		want := "#"
		if icons {
			want = "\uf02b"
		}
		preview := ansi.Strip(renderTagExample(cfg.Display.TagStyle, cfg.Display.TagEnds, icons, "#000000"))
		if preview != want {
			t.Fatalf("preview = %q, want %q", preview, want)
		}
		tag := annotationTag{kind: tagMail, category: "Personal", color: "#009900"}
		chips := layoutTags([]annotationTag{tag}, cfg.Display.TagStyle, cfg.Display.TagEnds, icons, 10)
		if len(chips) != 1 || chips[0].text != want || chips[0].color != tag.color || !chips[0].colored || chips[0].filled {
			t.Fatalf("icon rendering: %+v", chips)
		}
		for budget := 0; budget < 5; budget++ {
			if chipsWidth(layoutTags([]annotationTag{tag, tag, tag}, cfg.Display.TagStyle, cfg.Display.TagEnds, icons, budget)) > budget {
				t.Fatalf("icons exceed budget %d", budget)
			}
		}
	}
}
