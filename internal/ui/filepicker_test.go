package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPathPickerNavigatesAndPicks(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sigs")
	for _, d := range []string{sub} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(sub, "mine.html")
	if err := os.WriteFile(target, []byte("<p>hi</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	var p pathPicker
	if err := p.open(root); err != nil {
		t.Fatal(err)
	}
	p.jump("s") // lands on the sigs folder, skipping ".."
	if got := p.entries[p.cursor].name; got != "sigs" {
		t.Fatalf("jump landed on %q", got)
	}
	if _, picked, err := p.enter(); err != nil || picked || p.currentDir != sub {
		t.Fatalf("entering a folder: picked=%v dir=%q err=%v", picked, p.currentDir, err)
	}
	p.jump("m")
	path, picked, err := p.enter()
	if err != nil || !picked || path != target {
		t.Fatalf("pick = %q %v %v, want %q", path, picked, err, target)
	}
	if err := p.parent(); err != nil || p.currentDir != root {
		t.Fatalf("parent: dir=%q err=%v", p.currentDir, err)
	}
}

func TestSignatureHTMLRowBrowsesAndFillsPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sig.html")
	if err := os.WriteFile(file, []byte("<p>hi</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	am := newFromFormAccountManager()
	am.mode = amEdit
	am.sigHTMLInput.SetValue(filepath.Join(dir, "old.html"))
	am.focusField(amFieldSignatureHTML)

	alt := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}, Alt: true}
	am, _, _ = am.Update(alt, DefaultKeys)
	if am.mode != amPickSigHTML || am.sigPicker.currentDir != dir {
		t.Fatalf("alt+f: mode=%v dir=%q", am.mode, am.sigPicker.currentDir)
	}
	// esc leaves the typed path alone
	am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if am.mode != amEdit || am.sigHTMLInput.Value() != filepath.Join(dir, "old.html") {
		t.Fatalf("esc: mode=%v value=%q", am.mode, am.sigHTMLInput.Value())
	}
	am, _, _ = am.Update(alt, DefaultKeys)
	am.sigPicker.jump("s")
	am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if am.mode != amEdit || am.sigHTMLInput.Value() != file {
		t.Fatalf("enter: mode=%v value=%q", am.mode, am.sigHTMLInput.Value())
	}
	if am.statusMsg != "" {
		t.Fatalf("choosing a file needs no message, got %q", am.statusMsg)
	}
	if f := am.validateForm(am.buildCfg()); !f.ok() {
		t.Fatalf("chosen file should validate: %q", f.msg)
	}
	am.sigHTMLInput.SetValue(filepath.Join(dir, "nope.html"))
	if f := am.validateForm(am.buildCfg()); f.ok() || f.field != amFieldSignatureHTML {
		t.Fatalf("missing file should fail on the HTML signature row, got %+v", f)
	}
}

// A wrong pick (a key, a config file, a script) is refused in the browser: the
// field keeps its value and the reason shows as an error.
func TestSignatureHTMLPickRefusesWrongFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"notes.txt":  "hello",
		"id_rsa":     "-----BEGIN OPENSSH PRIVATE KEY-----",
		"bad.html":   "<p>hi</p><script>x()</script>",
		"plain.html": "just words",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(dir, "keep.html")
	if err := os.WriteFile(keep, []byte("<p>ok</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	am := newFromFormAccountManager()
	am.mode = amEdit
	am.sigHTMLInput.SetValue(keep)
	am.focusField(amFieldSignatureHTML)
	am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}, Alt: true}, DefaultKeys)

	chrome := newManagerChrome(80, CatppuccinMocha, false)
	for name := range map[string]bool{"notes.txt": true, "id_rsa": true, "bad.html": true, "plain.html": true} {
		for i, e := range am.sigPicker.entries {
			if e.name == name {
				am.sigPicker.cursor = i
			}
		}
		am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
		if am.mode != amPickSigHTML || am.sigHTMLInput.Value() != keep {
			t.Fatalf("%s: mode=%v value=%q; a refused file must leave the browser open and the field alone", name, am.mode, am.sigHTMLInput.Value())
		}
		if !strings.HasPrefix(am.statusMsg, "CANNOT USE THAT FILE") || !am.statusIsFailure(chrome) {
			t.Fatalf("%s: expected an error status, got %q", name, am.statusMsg)
		}
	}
}
