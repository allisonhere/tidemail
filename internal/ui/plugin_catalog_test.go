package ui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/plugincatalog"
)

func catalogTarball(t *testing.T, version string) []byte {
	t.Helper()
	manifest := "id = \"demo\"\nname = \"Demo\"\nversion = \"" + version + "\"\napi = 1\ncommand = \"tidemail-demo\"\n\n[permissions]\nmessage_metadata = true\nnetwork = true\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{{"plugin.toml", manifest}, {"tidemail-demo", "#!/bin/sh\n"}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(f.body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// catalogModel is a model whose plugin catalog is a local server offering one
// plugin, "demo", at the given version, and an empty plugins directory.
func catalogModel(t *testing.T, version string) (Model, *string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("plugin install needs a Unix executable bit")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	mux := http.NewServeMux()
	served := version
	mux.HandleFunc("/demo.tar.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(catalogTarball(t, served)) })
	mux.HandleFunc("/catalog.json", func(w http.ResponseWriter, r *http.Request) {
		sum := sha256.Sum256(catalogTarball(t, served))
		_ = json.NewEncoder(w).Encode(plugincatalog.Catalog{Schema: 1, Plugins: []plugincatalog.Entry{{
			ID: "demo", Name: "Demo", Description: "Does demo things", Version: served, API: 1,
			Permissions: plugin.Permissions{MessageMetadata: true, Network: true},
			Assets:      map[string]plugincatalog.Asset{plugincatalog.Platform(): {URL: "http://" + r.Host + "/demo.tar.gz", SHA256: hex.EncodeToString(sum[:])}},
		}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	m, _ := newMailboxListModel(7, 1)
	m.LoadPlugins() // an empty (missing) plugins directory
	m.catalog.client = &plugincatalog.Client{URL: srv.URL + "/catalog.json", Trusted: func(string) bool { return true }}
	return m, &served
}

// press sends a key and runs the returned command to completion, feeding each
// resulting message back, so async fetch and install behave as in the app.
func pressCatalog(t *testing.T, m Model, key string) Model {
	t.Helper()
	var msg tea.Msg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	return settleCatalog(t, m, msg)
}

func settleCatalog(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(Model)
	for range 6 {
		if cmd == nil {
			break
		}
		out := cmd()
		switch out.(type) {
		case CatalogLoadedMsg, CatalogChangedMsg:
			next, cmd = m.Update(out)
			m = next.(Model)
		default:
			return m
		}
	}
	return m
}

func openCatalog(t *testing.T, m Model) Model {
	t.Helper()
	m.overlay = overlayNone
	next, cmd := m.executeCommand("plugin-catalog")
	m = next.(Model)
	if cmd == nil || m.overlay != overlayPluginCatalog {
		t.Fatalf("the catalog did not open: overlay=%v", m.overlay)
	}
	next, _ = m.Update(cmd())
	return next.(Model)
}

func TestBrowseAvailablePluginsWithNoneInstalled(t *testing.T) {
	m, _ := catalogModel(t, "1.0.0")
	if !slices.Contains(commandIDs(m), "plugin-catalog") {
		t.Fatal("browse command missing although no plugin is installed")
	}
	m = openCatalog(t, m)
	view := m.renderPluginCatalog(78, newManagerChrome(78, m.styles.Theme, m.styles.PlainUI))
	for _, want := range []string{"Demo", "1.0.0", "Does demo things", "install"} {
		if !strings.Contains(view, want) {
			t.Errorf("catalog view lacks %q:\n%s", want, view)
		}
	}
}

func TestInstallUpdateAndRemoveFromTheCatalog(t *testing.T) {
	m, served := catalogModel(t, "1.0.0")
	m = openCatalog(t, m)

	// i asks first, and shows what the plugin will be able to do.
	m = pressCatalog(t, m, "i")
	if m.catalog.confirm == nil {
		t.Fatal("install should ask for confirmation")
	}
	view := m.renderPluginCatalog(78, newManagerChrome(78, m.styles.Theme, m.styles.PlainUI))
	for _, want := range []string{"Install Demo 1.0.0?", "Network", "not sandboxed"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirm lacks %q:\n%s", want, view)
		}
	}
	if _, ok := m.plugins.manager.Plugin("demo"); ok {
		t.Fatal("installed before the user confirmed")
	}

	m = pressCatalog(t, m, "enter")
	if _, ok := m.plugins.manager.Plugin("demo"); !ok {
		t.Fatalf("not installed (live reload failed): %q", m.catalog.message)
	}
	if !m.pluginsVisible() {
		t.Error("plugin commands should appear once a plugin is installed")
	}
	dir, _ := os.UserConfigDir()
	if _, err := os.Stat(filepath.Join(dir, "tidemail", "plugins", "demo", "plugin.toml")); err != nil {
		t.Errorf("files not in the plugins directory: %v", err)
	}

	// A newer release offers an update.
	*served = "1.1.0"
	m = pressCatalog(t, m, "r")
	rows := m.catalogRowsList()
	if len(rows) != 1 || !rows[0].installed {
		t.Fatalf("rows: %+v", rows)
	}
	if !rows[0].updateOffered {
		t.Fatal("1.1.0 over 1.0.0 should offer an update")
	}
	m = pressCatalog(t, m, "u")
	if m.catalog.confirm == nil || m.catalog.confirm.action != catalogUpdate {
		t.Fatal("u should ask before updating")
	}
	m = pressCatalog(t, m, "enter")
	if p, ok := m.plugins.manager.Plugin("demo"); !ok || p.Manifest.Version != "1.1.0" {
		t.Fatalf("not updated: %+v (%q)", p.Manifest, m.catalog.message)
	}
	if m.catalogRowsList()[0].updateOffered {
		t.Error("an up-to-date plugin should not offer an update")
	}

	m = pressCatalog(t, m, "x")
	if m.catalog.confirm == nil || m.catalog.confirm.action != catalogRemove {
		t.Fatal("x should ask before removing")
	}
	m = pressCatalog(t, m, "enter")
	if _, ok := m.plugins.manager.Plugin("demo"); ok {
		t.Fatal("still installed after remove")
	}
	if _, err := os.Stat(filepath.Join(dir, "tidemail", "plugins", "demo")); err == nil {
		t.Error("directory still on disk")
	}
}

func TestCatalogChangesWaitForARunningPlugin(t *testing.T) {
	m, _ := catalogModel(t, "1.0.0")
	m = openCatalog(t, m)
	m.plugins.running = "other"
	m = pressCatalog(t, m, "i")
	m = pressCatalog(t, m, "enter")
	if _, ok := m.plugins.manager.Plugin("demo"); ok || !strings.Contains(m.catalog.message, "running") {
		t.Fatalf("installed while a plugin runs: msg=%q", m.catalog.message)
	}
}

func TestCatalogFailuresAreShownAndHarmless(t *testing.T) {
	m, _ := catalogModel(t, "1.0.0")
	m.catalog.client = &plugincatalog.Client{URL: "http://127.0.0.1:1/catalog.json", Trusted: func(string) bool { return true }}
	m = openCatalog(t, m)
	view := m.renderPluginCatalog(78, newManagerChrome(78, m.styles.Theme, m.styles.PlainUI))
	if !strings.Contains(view, "Couldn't load the plugin list") || !strings.Contains(view, "r to try again") {
		t.Errorf("fetch failure not explained:\n%s", view)
	}
	if next := pressCatalog(t, m, "esc"); next.overlay == overlayPluginCatalog {
		t.Error("esc should leave the catalog even after a failure")
	}
}

func TestBOpensTheCatalogFromThePluginList(t *testing.T) {
	m, _ := catalogModel(t, "1.0.0")
	m.overlay = overlayPlugins
	next, cmd := m.handlePluginListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = next.(Model)
	if m.overlay != overlayPluginCatalog || cmd == nil || m.catalog.origin != overlayPlugins {
		t.Fatalf("overlay=%v origin=%v", m.overlay, m.catalog.origin)
	}
	m = pressCatalog(t, m, "esc")
	if m.overlay != overlayPlugins {
		t.Errorf("esc should return to the plugin list, got %v", m.overlay)
	}
}

func TestSettingsPluginsPaneOffersBrowseWhenNothingIsInstalled(t *testing.T) {
	m, _ := catalogModel(t, "1.0.0")
	m.settings = newSettings(m.cfg, m.settingsUpdateState())
	m.settings.pluginsAvailable = m.pluginsVisible()
	m.settings.setActiveSection(ssPlugins)
	m.settings.setFocusedPane(settingsPaneDetail)
	m.overlay = overlaySettings

	if got := m.renderSettingsPluginPane(60, 20, newManagerChrome(60, m.styles.Theme, m.styles.PlainUI)); !strings.Contains(got, "Press b to browse") {
		t.Errorf("empty Plugins pane doesn't point at the catalog:\n%s", got)
	}
	next, cmd := m.handleSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = next.(Model)
	if m.overlay != overlayPluginCatalog || m.catalog.origin != overlaySettings || cmd == nil {
		t.Fatalf("overlay=%v origin=%v", m.overlay, m.catalog.origin)
	}
	if m = pressCatalog(t, m, "esc"); m.overlay != overlaySettings || m.settings.activeSection != ssPlugins {
		t.Errorf("esc should return to Settings → Plugins, got overlay %v", m.overlay)
	}
}
