package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/plugin"
)

// The test binary doubles as the plugin: started under the manifest's
// command name, it runs main.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "tidemail-plugin-example" {
		main()
		return
	}
	os.Exit(m.Run())
}

func call(t *testing.T, in string) (map[string]any, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(strings.NewReader(in), &out, &errOut)
	var resp map[string]any
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("stdout is not one JSON value: %q", out.String())
		}
	}
	return resp, errOut.String(), code
}

func TestHandlesProtocol(t *testing.T) {
	resp, _, code := call(t, `{"api":1,"type":"request","request_id":"r1","method":"ping","data":{}}`)
	if code != 0 || resp["ok"] != true || resp["request_id"] != "r1" || resp["data"].(map[string]any)["message"] != "pong" {
		t.Fatalf("ping: %v %d", resp, code)
	}
	resp, _, _ = call(t, `{"api":1,"type":"request","request_id":"r2","method":"frobnicate"}`)
	if resp["ok"] != false || resp["error"].(map[string]any)["code"] != "unsupported_method" {
		t.Fatalf("unknown method: %v", resp)
	}
	resp, _, _ = call(t, `{"api":2,"type":"request","request_id":"r3","method":"ping"}`)
	if resp["ok"] != false || resp["error"].(map[string]any)["code"] != "unsupported_api" {
		t.Fatalf("api 2: %v", resp)
	}
	resp, stderr, code := call(t, `not json`)
	if code != 1 || resp != nil || !strings.Contains(stderr, "cannot read request") {
		t.Fatalf("garbage: %v %q %d; diagnostics go to stderr only", resp, stderr, code)
	}
}

func TestClassifyIsDeterministic(t *testing.T) {
	for subject, want := range map[string]int{"Your INVOICE #42": 1, "Invoice attached": 1, "Lunch?": 0, "": 0} {
		for range 2 {
			res := classify(metadata{Subject: subject})
			if len(res.Annotations) != want {
				t.Fatalf("%q: %+v", subject, res)
			}
		}
	}
	// The output passes TideMail's own annotation validation.
	data, _ := json.Marshal(classify(metadata{Subject: "invoice"}))
	anns, err := plugin.ParseAnnotations(data)
	if err != nil || len(anns) != 1 || anns[0].Key != "category" || anns[0].Value != "billing" {
		t.Fatalf("anns = %+v, %v", anns, err)
	}
}

// memStore records what TideMail would store.
type memStore map[int64][]plugin.Annotation

func (s memStore) ReplaceAnnotations(_ string, id int64, anns []plugin.Annotation) error {
	s[id] = anns
	return nil
}

// TestRunsUnderTideMail installs the example exactly as the tutorial says
// and drives it through TideMail's plugin manager.
func TestRunsUnderTideMail(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "example")
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
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(dir, "tidemail-plugin-example")); err != nil {
		t.Skip("cannot symlink the test binary:", err)
	}
	mgr, err := plugin.Discover(root)
	if err != nil || len(mgr.Errors()) > 0 {
		t.Fatalf("discover: %v %v", err, mgr.Errors())
	}
	ctx := context.Background()
	if msg, err := mgr.Ping(ctx, "example"); err != nil || msg != "pong" {
		t.Fatalf("ping = %q, %v", msg, err)
	}
	store := memStore{}
	res, err := mgr.MessageMetadata(ctx, "example", plugin.MessageMetadata{ID: 7, Subject: "Invoice 2026-09"}, store)
	if err != nil || res.Outcome != plugin.AnnotationsStored {
		t.Fatalf("metadata: %+v, %v", res, err)
	}
	if got := store[7]; len(got) != 1 || got[0].Key != "category" || got[0].Value != "billing" {
		t.Fatalf("stored %+v", got)
	}
	// A rerun with a changed subject replaces the set with an empty one.
	if _, err := mgr.MessageMetadata(ctx, "example", plugin.MessageMetadata{ID: 7, Subject: "Thanks"}, store); err != nil || len(store[7]) != 0 {
		t.Fatalf("rerun: %+v, %v", store[7], err)
	}
}
