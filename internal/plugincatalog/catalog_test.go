package plugincatalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/plugin"
)

func trustAll(string) bool { return true }

const manifestTOML = `id = "demo"
name = "Demo"
version = "1.2.0"
api = 1
command = "tidemail-demo"
description = "A demo plugin"

[permissions]
message_metadata = true
annotations = true
`

type file struct {
	name, body string
	typeflag   byte
	link       string
}

func tarball(t *testing.T, files ...file) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		flag := f.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		hdr := &tar.Header{Name: f.name, Typeflag: flag, Mode: 0o644, Size: int64(len(f.body)), Linkname: f.link}
		if flag != tar.TypeReg {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if flag == tar.TypeReg {
			if _, err := tw.Write([]byte(f.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func goodTarball(t *testing.T) []byte {
	return tarball(t, file{name: "plugin.toml", body: manifestTOML}, file{name: "tidemail-demo", body: "#!/bin/sh\necho hi\n"}, file{name: "README.md", body: "demo"})
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// server serves a tarball at /demo.tar.gz and returns an entry pointing to it.
func server(t *testing.T, archive []byte) (*Client, Entry) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/demo.tar.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	entry := Entry{
		ID: "demo", Name: "Demo", Description: "A demo plugin", Version: "1.2.0", API: 1,
		Permissions: plugin.Permissions{MessageMetadata: true, Annotations: true},
		Assets:      map[string]Asset{Platform(): {URL: srv.URL + "/demo.tar.gz", SHA256: sum(archive)}},
	}
	return &Client{Trusted: trustAll}, entry
}

func listing(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func TestInstallNewPlugin(t *testing.T) {
	c, entry := server(t, goodTarball(t))
	root := filepath.Join(t.TempDir(), "plugins")
	p, err := c.Install(context.Background(), entry, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Manifest.ID != "demo" || p.Dir != filepath.Join(root, "demo") {
		t.Fatalf("installed: %+v", p)
	}
	info, err := os.Stat(p.Executable)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("command is not executable: %v %v", info, err)
	}
	if readme, _ := os.Stat(filepath.Join(p.Dir, "README.md")); readme == nil || readme.Mode().Perm()&0o111 != 0 {
		t.Error("only the command may be executable")
	}
	if names := listing(t, root); len(names) != 1 {
		t.Errorf("staging left behind: %v", names)
	}
	// Discovery sees it like any hand-installed plugin.
	m, err := plugin.Discover(root)
	if err != nil || len(m.Errors()) > 0 || len(m.Plugins()) != 1 {
		t.Fatalf("discover: %v %v", err, m.Errors())
	}
}

func TestInstallReplacesInPlaceAndKeepsOldOnFailure(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "my-demo-dir") // the directory name is not the id
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "OLD"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, entry := server(t, goodTarball(t))
	bad := entry
	bad.Assets = map[string]Asset{Platform(): {URL: entry.Assets[Platform()].URL, SHA256: strings.Repeat("0", 64)}}
	if _, err := c.Install(context.Background(), bad, root, old); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(old, "OLD")); err != nil {
		t.Fatal("a failed update must leave the installed copy alone")
	}

	if _, err := c.Install(context.Background(), entry, root, old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(old, "OLD")); err == nil {
		t.Error("the old files should be gone")
	}
	if _, err := os.Stat(filepath.Join(old, "plugin.toml")); err != nil {
		t.Error("the new files should be in the old directory")
	}
	if names := listing(t, root); len(names) != 1 || names[0] != "my-demo-dir" {
		t.Errorf("root = %v", names)
	}
}

func TestInstallRefusesAnythingUnsafeOrInconsistent(t *testing.T) {
	cases := []struct {
		name    string
		files   []file
		mutate  func(*Entry)
		wantErr string
	}{
		{"path traversal", []file{{name: "../evil", body: "x"}, {name: "plugin.toml", body: manifestTOML}}, nil, "unsafe file name"},
		{"nested path", []file{{name: "sub/plugin.toml", body: manifestTOML}}, nil, "unsafe file name"},
		{"symlink", []file{{name: "tidemail-demo", typeflag: tar.TypeSymlink, link: "/bin/sh"}, {name: "plugin.toml", body: manifestTOML}}, nil, "not a plain file"},
		{"hidden file", []file{{name: ".hidden", body: "x"}}, nil, "unsafe file name"},
		{"no manifest", []file{{name: "tidemail-demo", body: "x"}}, nil, "not valid"},
		{"missing command", []file{{name: "plugin.toml", body: manifestTOML}}, nil, "no command"},
		{"wrong version", nil, func(e *Entry) { e.Version = "9.9.9" }, "version 1.2.0"},
		{"wrong id", nil, func(e *Entry) { e.ID = "other" }, "not \"other\""},
		{"extra permissions", nil, func(e *Entry) { e.Permissions = plugin.Permissions{MessageMetadata: true} }, "different permissions"},
		{"too many files", manyFiles(t), nil, "too many files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			archive := goodTarball(t)
			if c.files != nil {
				archive = tarball(t, c.files...)
			}
			client, entry := server(t, archive)
			if c.mutate != nil {
				c.mutate(&entry)
			}
			root := t.TempDir()
			_, err := client.Install(context.Background(), entry, root, "")
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want %q", err, c.wantErr)
			}
			if names := listing(t, root); len(names) != 0 {
				t.Errorf("a refused install left %v", names)
			}
		})
	}
}

func manyFiles(t *testing.T) []file {
	files := []file{{name: "plugin.toml", body: manifestTOML}, {name: "tidemail-demo", body: "x"}}
	for i := range 20 {
		files = append(files, file{name: "f" + string(rune('a'+i)), body: "x"})
	}
	return files
}

func TestInstallNeedsABuildForThisPlatform(t *testing.T) {
	c, entry := server(t, goodTarball(t))
	entry.Assets = map[string]Asset{"plan9-mips": entry.Assets[Platform()]}
	if _, err := c.Install(context.Background(), entry, t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), ErrNoBuild.Error()) {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallRefusesUntrustedHostsAndRedirects(t *testing.T) {
	// Default trust: a plain http test server is not GitHub.
	_, entry := server(t, goodTarball(t))
	if _, err := (&Client{}).Install(context.Background(), entry, t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("untrusted host: %v", err)
	}
	// A trusted first hop must not be allowed to bounce to an untrusted host.
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(goodTarball(t)) }))
	defer evil.Close()
	hop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, evil.URL, http.StatusFound) }))
	defer hop.Close()
	c := &Client{Trusted: func(u string) bool { return strings.HasPrefix(u, hop.URL) }}
	entry.Assets = map[string]Asset{Platform(): {URL: hop.URL, SHA256: sum(goodTarball(t))}}
	if _, err := c.Install(context.Background(), entry, t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("redirect: %v", err)
	}
}

func TestRemove(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("not removed")
	}

	// Never delete outside the plugins directory, or the directory itself.
	outside := t.TempDir()
	for _, bad := range []string{outside, root, filepath.Join(root, "a", "b"), filepath.Join(root, ".install-x")} {
		if err := Remove(bad, root); err == nil {
			t.Errorf("Remove(%q) should be refused", bad)
		}
	}

	// A symlinked plugin directory is unlinked, and its target survives.
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Remove(link, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "keep")); err != nil {
		t.Error("removing a symlinked plugin deleted its target")
	}
}

func catalogJSON(t *testing.T, mutate func(*Catalog)) []byte {
	t.Helper()
	cat := Catalog{Schema: 1, Plugins: []Entry{
		{ID: "zeta", Name: "Zeta", Description: "z", Version: "0.1.0", API: 1, Assets: map[string]Asset{Platform(): {URL: "https://github.com/x/y/releases/download/v1/z.tar.gz", SHA256: strings.Repeat("a", 64)}}},
		{ID: "alpha", Name: "Alpha", Description: "a", Version: "1.0.0", API: 1, Permissions: plugin.Permissions{Network: true}, Assets: map[string]Asset{}},
		{ID: "future", Name: "Future", Version: "1.0.0", API: 2},
	}}
	if mutate != nil {
		mutate(&cat)
	}
	b, err := json.Marshal(cat)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fetchFrom(t *testing.T, status int, body []byte, trusted func(string) bool) (Catalog, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	return (&Client{URL: srv.URL, Trusted: func(u string) bool { return strings.HasPrefix(u, srv.URL) || (trusted != nil && trusted(u)) }}).Fetch(context.Background())
}

func TestFetch(t *testing.T) {
	cat, err := fetchFrom(t, 200, catalogJSON(t, nil), func(u string) bool { return strings.HasPrefix(u, "https://github.com/") })
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Plugins) != 2 || cat.Plugins[0].ID != "alpha" || cat.Plugins[1].ID != "zeta" {
		t.Fatalf("plugins (sorted, API-2 entry dropped): %+v", cat.Plugins)
	}
	if !cat.Plugins[0].Permissions.Network || cat.Plugins[0].Available() || !cat.Plugins[1].Available() {
		t.Errorf("entries: %+v", cat.Plugins)
	}
}

func TestFetchRejectsBadCatalogs(t *testing.T) {
	github := func(u string) bool { return strings.HasPrefix(u, "https://github.com/") }
	for name, c := range map[string]struct {
		status int
		body   []byte
		want   string
	}{
		"not found":      {404, []byte("nope"), "HTTP 404"},
		"not json":       {200, []byte("<html>"), "not valid JSON"},
		"newer schema":   {200, catalogJSON(t, func(c *Catalog) { c.Schema = 2 }), "schema"},
		"duplicate id":   {200, catalogJSON(t, func(c *Catalog) { c.Plugins[1].ID = "zeta" }), "twice"},
		"bad id":         {200, catalogJSON(t, func(c *Catalog) { c.Plugins[0].ID = "../zeta" }), "zeta"},
		"escape in name": {200, catalogJSON(t, func(c *Catalog) { c.Plugins[0].Name = "Z\x1b[31m" }), "control characters"},
		"bad version":    {200, catalogJSON(t, func(c *Catalog) { c.Plugins[0].Version = "latest" }), "MAJOR.MINOR.PATCH"},
		"bad sha": {200, catalogJSON(t, func(c *Catalog) {
			a := c.Plugins[0].Assets[Platform()]
			a.SHA256 = "xyz"
			c.Plugins[0].Assets[Platform()] = a
		}), "sha256"},
		"foreign host": {200, catalogJSON(t, func(c *Catalog) {
			a := c.Plugins[0].Assets[Platform()]
			a.URL = "https://evil.example/z.tar.gz"
			c.Plugins[0].Assets[Platform()] = a
		}), "trusted host"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fetchFrom(t, c.status, c.body, github); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	// The catalog itself must come from a trusted host.
	if _, err := (&Client{URL: "https://evil.example/catalog.json"}).Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("untrusted catalog URL: %v", err)
	}
}
