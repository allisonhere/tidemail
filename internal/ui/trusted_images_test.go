package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/db"
)

func trustedImagesModel(t *testing.T, from string, trusted []string) Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m, _ := graphicsModel(t)
	m.cfg.Display.TrustedImageSenders = trusted
	m.filteredMessages = []db.Message{{ID: 7, From: from, BodyHTML: `<p>hi</p><img src="https://example.com/a.png" width="40" height="40">`}}
	m.focused = paneContent
	m.contentMessageID = 7
	m.contentLineCount = 5
	return m
}

func TestTrustImagesKeyToggles(t *testing.T) {
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}}
	tests := []struct {
		name        string
		from        string
		trusted     []string
		wantTrusted []string
		wantAllowed bool
	}{
		{"adds and loads", `"News" <News@Example.com>`, nil, []string{"news@example.com"}, true},
		{"removes when trusted", "news@example.com", []string{"NEWS@example.com"}, []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := trustedImagesModel(t, tt.from, tt.trusted)
			next, _ := m.handleMainKey(key)
			nm := next.(Model)
			got := nm.cfg.Display.TrustedImageSenders
			if len(got) != len(tt.wantTrusted) || (len(got) > 0 && got[0] != tt.wantTrusted[0]) {
				t.Fatalf("trusted = %v, want %v", got, tt.wantTrusted)
			}
			if nm.images.isAllowed(7) != tt.wantAllowed {
				t.Fatalf("allowed = %v, want %v", nm.images.isAllowed(7), tt.wantAllowed)
			}
		})
	}
}

func TestTrustedSenderAutoLoadsOnOpen(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		trusted []string
		want    bool
	}{
		{"trusted exact", "news@example.com", []string{"news@example.com"}, true},
		{"trusted case-insensitive", "News <NEWS@EXAMPLE.COM>", []string{"news@example.com"}, true},
		{"trusted domain", "a@example.com", []string{"@example.com"}, true},
		{"untrusted stays blocked", "spam@other.test", []string{"news@example.com"}, false},
		{"empty list stays blocked", "news@example.com", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := trustedImagesModel(t, tt.from, tt.trusted)
			next, _ := m.Update(struct{}{})
			nm := next.(Model)
			if got := nm.images.isAllowed(7); got != tt.want {
				t.Fatalf("auto-load allowed = %v, want %v", got, tt.want)
			}
		})
	}
}
