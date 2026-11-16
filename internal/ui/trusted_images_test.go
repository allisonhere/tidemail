package ui

import (
	"strings"
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

// Regression: trust is per message. When the reader shows several messages
// (a thread with several senders), automatic loading may only grant consent to
// messages whose own sender is trusted, never to the others in view.
func TestTrustedAutoLoadIsPerSender(t *testing.T) {
	trustedMsg := db.Message{ID: 7, From: "news@example.com", BodyHTML: `<p>hi</p><img src="https://example.com/a.png" width="40" height="40">`}
	otherMsg := db.Message{ID: 8, From: "spam@other.test", BodyHTML: `<p>re</p><img src="https://tracker.test/b.png" width="40" height="40">`}
	for _, rep := range []db.Message{trustedMsg, otherMsg} {
		t.Run("representative "+rep.From, func(t *testing.T) {
			m := trustedImagesModel(t, trustedMsg.From, []string{"news@example.com"})
			m.messages = []db.Message{trustedMsg, otherMsg}
			m.filteredMessages = []db.Message{trustedMsg, otherMsg}
			m.setViewportThread(messageThread{Key: "t", Representative: rep, Messages: []db.Message{trustedMsg, otherMsg}, Count: 2})
			m.contentMessageID = rep.ID

			if urls := m.remoteImageURLsFor(m.trustedMessagesInView()); len(urls) > 0 && urls[0] == "https://tracker.test/b.png" {
				t.Fatalf("untrusted message's images selected for loading: %v", urls)
			}
			next, _ := m.Update(struct{}{})
			nm := next.(Model)
			if nm.images.isAllowed(otherMsg.ID) {
				t.Fatal("untrusted sender's message got image consent from a trusted one")
			}
			if rep.ID == trustedMsg.ID && !nm.images.isAllowed(trustedMsg.ID) {
				t.Fatal("trusted sender's message was not loaded")
			}
		})
	}
}

// Regression: with a whole domain trusted, I on one of its senders used to
// report "removed" while the sender stayed trusted through the domain. It now
// excludes that sender and says so; the rest of the domain stays trusted.
func TestTrustImagesKeyExcludesSenderFromTrustedDomain(t *testing.T) {
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}}
	m := trustedImagesModel(t, "News <news@example.com>", []string{"@example.com"})

	next, _ := m.handleMainKey(key)
	nm := next.(Model)
	want := []string{"@example.com", "!news@example.com"}
	if got := nm.cfg.Display.TrustedImageSenders; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("trusted = %v, want %v", got, want)
	}
	if !strings.Contains(nm.statusMsg, "excluded") || !strings.Contains(nm.statusMsg, "@example.com") {
		t.Fatalf("status should explain the exclusion, got %q", nm.statusMsg)
	}
	reopened, _ := nm.Update(struct{}{})
	if reopened.(Model).images.isAllowed(7) {
		t.Fatal("excluded sender's images still load automatically")
	}

	// The rest of the domain still loads.
	other := trustedImagesModel(t, "deals@example.com", nm.cfg.Display.TrustedImageSenders)
	loaded, _ := other.Update(struct{}{})
	if !loaded.(Model).images.isAllowed(7) {
		t.Fatal("excluding one sender untrusted the whole domain")
	}

	// I again lifts the exclusion.
	again, _ := nm.handleMainKey(key)
	an := again.(Model)
	if got := an.cfg.Display.TrustedImageSenders; len(got) != 1 || got[0] != "@example.com" {
		t.Fatalf("trusted after second I = %v, want [@example.com]", got)
	}
	if !an.images.isAllowed(7) {
		t.Fatal("lifting the exclusion did not load the images")
	}
}
