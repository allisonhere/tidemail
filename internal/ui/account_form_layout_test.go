package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// signatureLines returns the rendered rows of the form's signature box.
func signatureLines(t *testing.T, am AccountManager) []string {
	t.Helper()
	lines := strings.Split(am.View(90, 80, BuildStyles(CatppuccinMocha, "compact", "square")), "\n")
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), "Signature") && i+signatureFieldHeight <= len(lines) {
			return lines[i : i+signatureFieldHeight]
		}
	}
	t.Fatal("signature row not rendered")
	return nil
}

func TestSignatureBoxIsFullyPainted(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	for _, c := range []struct {
		name    string
		text    string
		focused bool
	}{
		{"empty", "", false},
		{"empty focused", "", true},
		{"typed", "Alice\nSent from TideMail", false},
		{"typed focused", "Alice\nSent from TideMail", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			am := NewAccountManager(nil)
			am.mode = amAdd
			am.resetForm()
			am.sigArea.SetValue(c.text)
			if c.focused {
				am.focusField(amFieldSignature)
			}
			for n, line := range signatureLines(t, am) {
				if i := unpaintedCell(line); i >= 0 {
					t.Fatalf("signature line %d has a cell with no background at byte %d: %q", n, i, line)
				}
			}
		})
	}
}

func TestEmailRowIsFirstInTheForm(t *testing.T) {
	am := NewAccountManager(nil)
	am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, DefaultKeys)
	if am.mode != amAdd || am.focusedField != amFieldUser {
		t.Fatalf("add form opened in mode %v on field %v, want the login row", am.mode, am.focusedField)
	}
	am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if am.focusedField != amFieldProvider {
		t.Fatalf("tab from the login row reached %v, want Provider", am.focusedField)
	}

	for provider, label := range map[string]string{"Custom": "Email / login", "Gmail": "Email"} {
		am.setProvider(provider)
		view := ansi.Strip(am.View(90, 60, BuildStyles(CatppuccinMocha, "compact", "square")))
		login := strings.Index(view, label+" ")
		prov := strings.Index(view, "Provider")
		if login < 0 || prov < 0 || login > prov {
			t.Fatalf("%s: login row %q at %d, Provider at %d; want login first:\n%s", provider, label, login, prov, view)
		}
		if strings.Count(view, label+" ") != 1 {
			t.Fatalf("%s: login row rendered %d times", provider, strings.Count(view, label+" "))
		}
	}
}

// The status row stays in the same place with or without a message. One blank
// row separates it from the form, and spare rows follow it before the hints.
func TestStatusLineDoesNotMoveTheForm(t *testing.T) {
	chrome := newManagerChrome(74, CatppuccinMocha, false)
	for _, c := range []struct {
		name     string
		provider string
		height   int
	}{
		{"short form, tall window", "Gmail", 40},
		{"tall form, short window", "Custom", 20},
	} {
		t.Run(c.name, func(t *testing.T) {
			am := NewAccountManager(nil)
			am.mode = amAdd
			am.resetForm()
			am.setProvider(c.provider)
			am.focusField(amFieldSyncInterval) // scrolled to the bottom of the form
			without := strings.Split(ansi.Strip(am.viewForm(74, c.height, chrome)), "\n")
			am.statusMsg = "FOUND: mail.example.org:993 / mail.example.org:465"
			lines := strings.Split(ansi.Strip(am.viewForm(74, c.height, chrome)), "\n")

			if want := c.height; len(lines) != want || len(without) != want {
				t.Fatalf("form is %d lines, want %d (window height must not change)", len(lines), want)
			}
			if !strings.Contains(lines[len(lines)-1], "save") {
				t.Fatalf("key hints are not on the last line: %q", lines[len(lines)-1])
			}
			status := -1
			for i, l := range lines {
				if strings.Contains(l, "FOUND:") {
					status = i
				}
			}
			if status < 2 || strings.TrimSpace(lines[status-1]) != "" || strings.TrimSpace(lines[status-2]) == "" {
				t.Fatalf("status line must sit one blank row below the form:\n%s", strings.Join(lines, "\n"))
			}
			if strings.TrimSpace(without[status]) != "" {
				t.Fatalf("empty status row at %d contains %q", status, without[status])
			}
			for i := 0; i < status; i++ {
				if lines[i] != without[i] {
					t.Fatalf("form row %d moved when status appeared", i)
				}
			}
		})
	}
}

func TestSignatureMarkerHintFitsForm(t *testing.T) {
	for _, width := range []int{52, 74} {
		am := NewAccountManager(nil)
		am.mode = amAdd
		am.resetForm()
		am.focusField(amFieldSignature)
		view := ansi.Strip(am.View(width, 80, BuildStyles(CatppuccinMocha, "compact", "square")))
		words := strings.Join(strings.Fields(view), " ")
		if !strings.Contains(words, "A single ⁂ symbol in the compose editor marks where your signature will be inserted when sending. Delete the symbol to send without a signature.") {
			t.Fatalf("signature explanation missing or split inside words at width %d: %s", width, view)
		}
	}
}
