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
