package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tide/internal/config"
)

func TestEmptyAccountsHintUsesUppercaseAccountManagerShortcut(t *testing.T) {
	for _, tt := range []struct {
		name  string
		model Model
	}{
		{
			name: "plain",
			model: Model{
				cfg:    config.DefaultConfig(),
				styles: Styles{PlainUI: true},
			},
		},
		{
			name: "icons",
			model: Model{
				cfg:    config.DefaultConfig(),
				styles: BuildStyles(CatppuccinMocha, "comfortable"),
			},
		},
		{
			name: "no icons",
			model: Model{
				cfg: func() config.Config {
					cfg := config.DefaultConfig()
					cfg.Display.Icons = false
					return cfg
				}(),
				styles: BuildStyles(CatppuccinMocha, "comfortable"),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.model.emptyAccountsHint()
			if !strings.Contains(got, "press M to add accounts") {
				t.Fatalf("expected uppercase account shortcut, got %q", got)
			}
			if strings.Contains(got, "press m to add accounts") {
				t.Fatalf("expected no lowercase account shortcut, got %q", got)
			}
		})
	}
}
