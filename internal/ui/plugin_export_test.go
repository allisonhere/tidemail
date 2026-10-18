package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/plugin"
)

func exportModel(t *testing.T, r *pluginResult) Model {
	t.Helper()
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "pw"))
	t.Setenv("HOME", t.TempDir())
	m.width, m.height = 120, 40
	m.plugins.result = r
	m.overlay = overlayPluginResult
	return m
}

func viewResult() *pluginResult {
	return &pluginResult{pluginID: "analytics", pluginName: "Analytics", heading: "report: 3 queries", view: &plugin.View{
		Title:  "Mail last 30 days",
		Blocks: []plugin.Block{{Type: plugin.BlockStats, Items: []plugin.ViewItem{{Label: "Received", Value: "1234"}}}},
	}}
}

func TestReportPlainTextBodyAndView(t *testing.T) {
	m := exportModel(t, nil)
	body := m.pluginResultPlainText(&pluginResult{pluginName: "P", body: "line one\x1b[31m red\nline two"})
	if strings.Contains(body, "\x1b") || !strings.Contains(body, "line one") || !strings.Contains(body, "line two") {
		t.Errorf("body text = %q", body)
	}
	view := m.pluginResultPlainText(viewResult())
	if strings.Contains(view, "\x1b") || !strings.Contains(view, "1234") || !strings.Contains(strings.ToLower(view), "received") {
		t.Errorf("view text = %q", view)
	}
	for _, l := range strings.Split(view, "\n") {
		if len([]rune(l)) > reportExportWidth+2 {
			t.Errorf("line too wide (%d): %q", len([]rune(l)), l)
		}
	}
}

func TestReportSaveWritesSafeFile(t *testing.T) {
	m := exportModel(t, viewResult())
	m.plugins.result.pluginID = "../../evil/Plug in"
	next, cmd, ok := m.handleReportExportKey("s")
	if !ok || cmd == nil {
		t.Fatal("s should start a save")
	}
	_ = next
	msg := cmd().(reportSavedMsg)
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	dir := filepath.Join(os.Getenv("HOME"), "Downloads", "tidemail-reports")
	if filepath.Dir(msg.Path) != dir || !strings.HasSuffix(msg.Path, ".txt") || strings.Contains(filepath.Base(msg.Path), "/") {
		t.Errorf("path = %q; want a file directly in %q", msg.Path, dir)
	}
	info, err := os.Stat(msg.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("stat = %v, %v", info, err)
	}
	if again := saveReportCmd("x", "t"); again == nil {
		t.Fatal("nil cmd")
	}
}

func TestReportFilenameNeverEscapes(t *testing.T) {
	for _, id := range []string{"", "..", "../x", "a/b", "A B"} {
		n := reportFilename(id, time.Unix(0, 0))
		if strings.ContainsAny(n, `/\`) || strings.HasPrefix(n, ".") {
			t.Errorf("reportFilename(%q) = %q", id, n)
		}
	}
}

func TestReportPrintUsesSeam(t *testing.T) {
	m := exportModel(t, viewResult())
	var got string
	orig := printReport
	printReport = func(text string) (string, error) { got = text; return "lp", nil }
	defer func() { printReport = orig }()
	_, cmd, ok := m.handleReportExportKey("p")
	if !ok {
		t.Fatal("p should print")
	}
	if msg := cmd().(reportPrintedMsg); msg.Err != nil || !strings.Contains(got, "1234") {
		t.Errorf("msg=%+v printed=%q", msg, got)
	}
}

func TestReportCopyUsesClipboard(t *testing.T) {
	m := exportModel(t, viewResult())
	var copied string
	orig := clipboardWriteCmd
	clipboardWriteCmd = func(text string) tea.Cmd { copied = text; return nil }
	defer func() { clipboardWriteCmd = orig }()
	if _, _, ok := m.handleReportExportKey("c"); !ok || !strings.Contains(copied, "1234") {
		t.Errorf("copied = %q", copied)
	}
}

func TestReportExportIgnoredWhenNotExportable(t *testing.T) {
	card := resultCard{title: "boom"}
	for name, r := range map[string]*pluginResult{
		"nil":     nil,
		"card":    {card: &card, body: "x"},
		"empty":   {},
		"details": {body: "x", showDetails: true},
	} {
		m := exportModel(t, r)
		for _, k := range []string{"c", "s", "p"} {
			if _, cmd, ok := m.handleReportExportKey(k); ok || cmd != nil {
				t.Errorf("%s: key %s should be ignored", name, k)
			}
		}
	}
}

func TestReportKeysReachExportFromOverlay(t *testing.T) {
	m := exportModel(t, viewResult())
	orig := printReport
	printReport = func(string) (string, error) { return "lp", nil }
	defer func() { printReport = orig }()
	_, cmd := m.handlePluginKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if cmd == nil {
		t.Fatal("p in the result overlay should print")
	}
}
