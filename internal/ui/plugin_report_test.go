package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

// reportPluginScript answers report.run with a final report straight away.
// The report carries an escape sequence that must never reach the screen.
const reportPluginScript = `#!/bin/sh
read -r line
id=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
printf '{"api":1,"type":"response","request_id":"%s","ok":true,"data":{"report":{"title":"Mail\\u001b[31m stats","received":42}}}\n' "$id"
`

func installReportPlugin(t *testing.T, root, id string, capable bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test plugins need a Unix shell")
	}
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id = \"" + id + "\"\nname = \"Report " + id + "\"\napi = 1\ncommand = \"run\"\n"
	if capable {
		manifest += "capabilities = [\"report.run\"]\n\n[permissions]\nanalytics_read = true\n"
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(reportPluginScript), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestEnterRunsReportOnlyForReportPlugins(t *testing.T) {
	root := t.TempDir()
	installReportPlugin(t, root, "a-plain", false)
	installReportPlugin(t, root, "b-report", true)
	m := newPluginModel(t, root)
	database, err := db.OpenPath(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	m.db = database
	m.overlay = overlayPlugins

	// The plain plugin is selected first: enter does nothing and no hint shows.
	next, cmd := m.handlePluginListKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.plugins.running != "" {
		t.Fatal("enter must not run a plugin without report.run")
	}
	if strings.Contains(m.renderPluginOverlay(), "run report") {
		t.Fatal("run report hint shown for a plugin without report.run")
	}

	m.plugins.listCursor = 1
	if !strings.Contains(m.renderPluginOverlay(), "run report") {
		t.Fatal("run report hint missing")
	}
	next, cmd = m.handlePluginListKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || m.plugins.running != "b-report" {
		t.Fatal("enter should start the report")
	}
	msg, ok := cmd().(pluginReportMsg)
	if !ok || msg.Err != nil {
		t.Fatalf("report msg = %#v", msg)
	}
	next, _ = m.Update(msg)
	m = next.(Model)
	if m.overlay != overlayPluginResult || m.plugins.running != "" || m.plugins.result == nil {
		t.Fatalf("result not shown: overlay %v", m.overlay)
	}
	if m.plugins.result.heading != "report: 1 round, 0 queries" {
		t.Fatalf("heading = %q", m.plugins.result.heading)
	}
	body := m.plugins.result.body
	if !strings.Contains(body, "Received: 42") || strings.Contains(body, "\x1b") {
		t.Fatalf("report body = %q", body)
	}
}
