package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPluginCommandHelp(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"validate", "--help"},
		{"test", "--help"},
		{"scaffold", "--help"},
	} {
		var out, errOut bytes.Buffer
		if code := Run(args, &out, &errOut); code != 0 {
			t.Fatalf("Run(%q) = %d, stderr=%q", args, code, errOut.String())
		}
		if out.Len() == 0 {
			t.Fatalf("Run(%q) produced no help", args)
		}
	}
}

func TestPluginScaffoldBuildValidateTest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("scaffold executable checks use Unix permissions")
	}
	root := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	var out, errOut bytes.Buffer
	if code := Run([]string{"scaffold", "invoice-tagger"}, &out, &errOut); code != 0 {
		t.Fatalf("scaffold failed: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	pluginDir := filepath.Join(root, "invoice-tagger")
	for _, name := range []string{"plugin.toml", "main.go", "go.mod", "README.md", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(pluginDir, name)); err != nil {
			t.Fatalf("scaffold did not create %s: %v", name, err)
		}
	}

	cache := filepath.Join(root, "go-cache")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", "invoice-tagger", ".")
	cmd.Dir = pluginDir
	cmd.Env = append(os.Environ(), "GOCACHE="+cache)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated plugin did not build: %v\n%s", err, output)
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"validate", pluginDir}, &out, &errOut); code != 0 {
		t.Fatalf("validate failed: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"test", pluginDir}, &out, &errOut); code != 0 {
		t.Fatalf("test failed: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	data, err := os.ReadFile(filepath.Join(pluginDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(data)
	for _, want := range []string{"go build", "tidemail plugin validate .", "tidemail plugin test .", "XDG_CONFIG_HOME"} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing %q", want)
		}
	}

	if code := Run([]string{"scaffold", "invoice-tagger"}, &out, &errOut); code == 0 {
		t.Fatal("scaffold overwrote an existing target")
	}
}

func TestValidateJSONAndFixture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test plugin uses a Unix executable")
	}
	dir := t.TempDir()
	manifest := `id = "fixture"
name = "Fixture"
version = "0.1.0"
api = 1
command = "./fixture"

[permissions]
message_metadata = true
annotations = true
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
read request
id=$(printf '%s' "$request" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
method=$(printf '%s' "$request" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
if [ "$method" = "ping" ]; then data='{"message":"pong"}'; else data='{"annotations":[]}'; fi
printf '{"api":1,"type":"response","request_id":"%s","ok":true,"data":%s}\n' "$id" "$data"
`
	if err := os.WriteFile(filepath.Join(dir, "fixture"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run([]string{"validate", "--json", dir}, &out, &errOut); code != 0 {
		t.Fatalf("JSON validate failed: %d %q", code, errOut.String())
	}
	var report validationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.Valid {
		t.Fatalf("bad validation JSON: err=%v report=%+v", err, report)
	}
	fixture := filepath.Join(dir, "metadata.json")
	if err := os.WriteFile(fixture, []byte(`{"id": 4, "subject": "fixture"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"test", "--json", "--metadata", fixture, dir}, &out, &errOut); code != 0 {
		t.Fatalf("JSON test failed: %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	var testReportValue testReport
	if err := json.Unmarshal(out.Bytes(), &testReportValue); err != nil || !testReportValue.Valid {
		t.Fatalf("bad test JSON: err=%v report=%+v", err, testReportValue)
	}
}

// reportScript asks for one aggregate query in round 1, then reports the
// received count it got back.
const reportScript = `#!/bin/sh
read request
id=$(printf '%s' "$request" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
method=$(printf '%s' "$request" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
case "$method" in
ping) data='{"message":"pong"}' ;;
*)
  case "$request" in
  *'"round":1,'*) data='{"queries":{"vol":{"method":"analytics.volume","range":"all","group_by":"month"}},"state":"s1"}' ;;
  *) received=$(printf '%s' "$request" | sed -n 's/.*"results":{"vol":{[^}]*"received":\([0-9]*\).*/\1/p')
     data="{\"report\":\"received $received\"}" ;;
  esac ;;
esac
printf '{"api":1,"type":"response","request_id":"%s","ok":true,"data":%s}\n' "$id" "$data"
`

func writeReportPlugin(t *testing.T, capabilities string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := "id = \"reporter\"\nname = \"Reporter\"\napi = 1\ncommand = \"./run\"\n" + capabilities + "\n[permissions]\nanalytics_read = true\nmessages_query = true\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(reportScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReportPluginValidateAndTest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test plugin uses a Unix executable")
	}
	// No real TideMail data directory is ever opened.
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "must-not-be-created"))
	dir := writeReportPlugin(t, `capabilities = ["report.run"]`)

	var out, errOut bytes.Buffer
	if code := Run([]string{"validate", "--json", dir}, &out, &errOut); code != 0 {
		t.Fatalf("validate: %d %q", code, errOut.String())
	}
	var v validationReport
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || strings.Join(v.Plugin.Permissions, ",") != "messages query,analytics" || strings.Join(v.Plugin.Capabilities, ",") != "report.run" {
		t.Fatalf("validate JSON = %s", out.String())
	}

	out.Reset()
	if code := Run([]string{"test", "--json", dir}, &out, &errOut); code != 0 {
		t.Fatalf("test: %d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	var rep testReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil || !rep.Valid {
		t.Fatalf("test JSON = %s", out.String())
	}
	last := rep.Checks[len(rep.Checks)-1]
	if last.Name != "report.run" || last.Detail != "2 rounds, 1 query" {
		t.Fatalf("report check = %+v", last)
	}
	if _, err := os.Stat(os.Getenv("XDG_DATA_HOME")); !os.IsNotExist(err) {
		t.Fatal("plugin test touched the TideMail data directory")
	}

	// A custom synthetic mailbox.
	fixture := filepath.Join(t.TempDir(), "mail.json")
	data := `{"me":["me@x.example"],"accounts":[{"name":"A","mailboxes":[{"name":"INBOX","messages":[
		{"from":"a@y.example","subject":"one","date":"2026-01-02T00:00:00Z"}]}]}]}`
	if err := os.WriteFile(fixture, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Run([]string{"test", "--report-fixture", fixture, dir}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "✓ report.run") {
		t.Fatalf("fixture test: %d %q %q", code, out.String(), errOut.String())
	}
	// Bodies cannot be smuggled into a fixture.
	if err := os.WriteFile(fixture, []byte(strings.Replace(data, `"subject"`, `"body_text":"x","subject"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Run([]string{"test", "--report-fixture", fixture, dir}, &out, &errOut); code == 0 || !strings.Contains(out.String(), "body_text") {
		t.Fatalf("unknown fixture field accepted: %d %q", code, out.String())
	}

	// --report-fixture on a plugin without report.run fails clearly.
	plain := writeReportPlugin(t, "")
	out.Reset()
	if code := Run([]string{"test", "--report-fixture", fixture, plain}, &out, &errOut); code == 0 || !strings.Contains(out.String(), "does not declare capability report.run") {
		t.Fatalf("missing capability: %d %q", code, out.String())
	}
}
