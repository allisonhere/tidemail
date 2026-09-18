package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/update"
)

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// manualUpdateModel returns a model that has an update available and cannot
// install it itself, the way a distro-packaged install behaves.
func manualUpdateModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, config.DefaultConfig(), "v1.2.2", false)
	m.updateState = updateStateAvailable
	m.updateInfo = update.ReleaseInfo{Version: "v1.2.3", AssetName: "tidemail-linux-x86_64"}
	m.updateInstall = update.InstallResult{RequiresManual: true, ManualCommand: "yay -S tidemail-bin"}
	if !m.manualUpdateRequired() {
		t.Fatal("expected this model to require a manual update")
	}
	return m
}

// The status bar offers U for both install types now. It used to be hidden when
// an update had to be applied by hand, leaving packaged users no signal at all.
func TestStatusBarOffersUpdateKeyForBothInstallTypes(t *testing.T) {
	manual := manualUpdateModel(t)
	if got := ansi.Strip(manual.statusUpdateActionPart()); !strings.Contains(got, "U update") {
		t.Fatalf("manual install status = %q, want it to offer U", got)
	}

	selfInstall := manualUpdateModel(t)
	selfInstall.updateInstall = update.InstallResult{}
	if selfInstall.manualUpdateRequired() {
		t.Skip("this machine's binary is not self-installable; skipping the writable case")
	}
	if got := ansi.Strip(selfInstall.statusUpdateActionPart()); !strings.Contains(got, "U update") {
		t.Fatalf("self-install status = %q, want it to offer U", got)
	}
}

// U opens the modal instead of being ignored, and must not start a download.
func TestUpdateKeyOpensManualModalWithoutDownloading(t *testing.T) {
	m := manualUpdateModel(t)
	next, _ := m.handleKey(runeKey('U'))
	got := next.(Model)
	if got.overlay != overlayUpdateConfirm {
		t.Fatalf("overlay = %v, want the update modal", got.overlay)
	}
	if got.updateState != updateStateAvailable {
		t.Fatalf("update state = %v, want it untouched", got.updateState)
	}
	if got.downloadedUpdate != nil {
		t.Fatal("pressing U started a download on an install that cannot self-update")
	}
}

// enter must close the modal, never fall through to the install branch.
func TestManualModalEnterClosesWithoutInstalling(t *testing.T) {
	m := manualUpdateModel(t)
	m.overlay = overlayUpdateConfirm

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if got.overlay != overlayNone {
		t.Fatalf("overlay = %v, want closed", got.overlay)
	}
	if got.updateInProgress() || got.updateState == updateStateDownloading {
		t.Fatalf("enter began an install: state=%v", got.updateState)
	}
}

// C (and the lowercase the hint row displays) copies the command.
func TestManualModalCopiesCommand(t *testing.T) {
	for _, key := range []string{"C", "c"} {
		t.Run(key, func(t *testing.T) {
			m := manualUpdateModel(t)
			m.overlay = overlayUpdateConfirm

			orig := clipboardWriteCmd
			copied := ""
			clipboardWriteCmd = func(text string) tea.Cmd {
				copied = text
				return nil
			}
			t.Cleanup(func() { clipboardWriteCmd = orig })

			if _, _ = m.handleKey(runeKey(rune(key[0]))); copied != "yay -S tidemail-bin" {
				t.Fatalf("%s copied %q, want the manual command", key, copied)
			}
		})
	}
}

// Both modals have to survive a narrow terminal: the formatting claim is that
// nothing overflows, so assert it rather than eyeball it.
func TestUpdateModalsFitNarrowTerminals(t *testing.T) {
	const width = 40
	chrome := newManagerChrome(width, CatppuccinMocha, true)

	manual := manualUpdateModel(t)
	manualBody := ansi.Strip(manual.renderUpdateConfirmOverlay(width, chrome))
	for _, want := range []string{"v1.2.3", "yay -S tidemail-bin", "Run this outside TideMail"} {
		if !strings.Contains(manualBody, want) {
			t.Fatalf("manual modal missing %q:\n%s", want, manualBody)
		}
	}
	assertNoLineExceeds(t, "manual modal", manualBody, width)

	install := manualUpdateModel(t)
	install.updateInstall = update.InstallResult{}
	install.updateInfo.Summary = strings.Repeat("a very long release note ", 8)
	installBody := ansi.Strip(install.renderUpdateConfirmOverlay(width, chrome))
	assertNoLineExceeds(t, "install modal", installBody, width)
}

func assertNoLineExceeds(t *testing.T, name, body string, width int) {
	t.Helper()
	for i, line := range strings.Split(body, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("%s line %d is %d cells wide, want <= %d:\n%q", name, i+1, w, width, line)
		}
	}
}
