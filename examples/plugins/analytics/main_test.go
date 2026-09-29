package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/pluginquery"
)

// The test binary doubles as the plugin: started under the manifest's
// command name, it runs main.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "tidemail-plugin-analytics-example" {
		main()
		return
	}
	os.Setenv("TIDEMAIL_DISABLE_KEYRING", "1")
	os.Exit(m.Run())
}

func TestManifestIsValid(t *testing.T) {
	m, err := plugin.LoadManifest("plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !m.HasCapability(plugin.CapabilityReport) || !m.Permissions.AnalyticsRead || m.Permissions.MessagesQuery || m.Permissions.MessageMetadata {
		t.Fatalf("the example should ask for aggregates only: %+v", m)
	}
}

// TestReportEndToEnd runs the example through TideMail's report driver and
// real query engine against the default synthetic mailbox.
func TestReportEndToEnd(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "analytics-example")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestFile), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A copy, not a symlink: TideMail refuses commands that resolve outside
	// the plugin directory.
	bin, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tidemail-plugin-analytics-example"), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := plugin.LoadPlugin(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	fixture := pluginquery.DefaultFixture(now)
	database, err := db.OpenPath(filepath.Join(root, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := fixture.Load(database); err != nil {
		t.Fatal(err)
	}
	exec := &pluginquery.Executor{DB: database, Me: conversation.MyAddresses(fixture.Me...), Location: time.UTC}
	rc := plugin.ReportContext{Now: fixture.Now, Timezone: "UTC"}
	res, err := plugin.RunReport(context.Background(), p, rc, exec, plugin.ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	if err := json.Unmarshal(res.Report, &text); err != nil {
		t.Fatalf("report is not a string: %s", res.Report)
	}
	for _, want := range []string{"8 received, 2 sent", "github", "Needs You 3", "Waiting on Them 1", "▁"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q:\n%s", want, text)
		}
	}
	if res.Rounds != 2 || res.Queries != 3 {
		t.Fatalf("rounds %d, queries %d", res.Rounds, res.Queries)
	}
}

func TestUnknownMethodAndAPI(t *testing.T) {
	for _, in := range []string{
		`{"api":1,"type":"request","request_id":"r","method":"nope"}`,
		`{"api":2,"type":"request","request_id":"r","method":"ping"}`,
	} {
		resp := handle(mustRequest(t, in))
		if resp.OK || resp.Error == nil || resp.RequestID != "r" {
			t.Fatalf("%s: %+v", in, resp)
		}
	}
}

func mustRequest(t *testing.T, raw string) request {
	t.Helper()
	var req request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	return req
}
