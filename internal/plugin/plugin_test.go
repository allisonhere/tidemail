package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Test plugins are this test binary, symlinked into a temporary plugin
// directory under a helper name. TestMain picks the behavior from the name the
// binary was started as. Plugins run with a scrubbed environment, so the usual
// GO_WANT_HELPER_PROCESS variable would never reach them.
var helpers = map[string]func(){
	"tidemail-plugin-pong":       helperPong,
	"tidemail-plugin-badjson":    func() { readRequest(); fmt.Print(`{"api": 1, "type": `) },
	"tidemail-plugin-wrongid":    helperWrongID,
	"tidemail-plugin-exit1":      helperExit1,
	"tidemail-plugin-hang":       func() { readRequest(); time.Sleep(time.Minute) },
	"tidemail-plugin-introspect": helperIntrospect,
	"tidemail-plugin-huge":       func() { readRequest(); fmt.Print(strings.Repeat(" ", maxResponseBytes+1)) },
	"tidemail-plugin-trailing":   helperTrailing,
	"tidemail-plugin-errresp":    helperErrResp,
	"tidemail-plugin-silent":     func() { readRequest() },
	"tidemail-plugin-echo":       helperEcho,
	"tidemail-plugin-marker":     helperMarker,
	"tidemail-plugin-hangpid":    helperHangPID,
	"tidemail-plugin-leak":       helperLeak,
	"tidemail-plugin-tester":     helperTester,
	"tidemail-plugin-concurrent": helperConcurrent,
	"tidemail-plugin-annotate":   helperAnnotate(`[{"key":"needs_reply","value":"true","confidence":0.94},{"key":"urgency","value":"high"}]`),
	"tidemail-plugin-badconf":    helperAnnotate(`[{"key":"urgency","value":"high","confidence":2.0}]`),
	"tidemail-plugin-noanns":     helperAnnotate(`[]`),
	// If a shell ever interpreted this name, it would create PWNED files.
	shellName: helperPong,
}

const shellName = "hello; touch PWNED $(touch PWNED2) `touch PWNED3`"

func TestMain(m *testing.M) {
	if helper, ok := helpers[filepath.Base(os.Args[0])]; ok {
		helper()
		os.Exit(0)
	}
	// Under -race every helper would sleep a second at exit
	// (atexit_sleep_ms). Let GORACE through to helpers to turn that off.
	envAllowlist = append(envAllowlist, "GORACE")
	os.Setenv("GORACE", strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
	os.Exit(m.Run())
}

func readRequest() Request {
	var req Request
	_ = json.NewDecoder(os.Stdin).Decode(&req)
	return req
}

func respond(req Request, data any) {
	raw, _ := json.Marshal(data)
	_ = json.NewEncoder(os.Stdout).Encode(Response{
		API: APIVersion, Type: TypeResponse, RequestID: req.RequestID, OK: true, Data: raw,
	})
}

func helperPong() {
	req := readRequest()
	if req.Method != MethodPing {
		_ = json.NewEncoder(os.Stdout).Encode(Response{
			API: APIVersion, Type: TypeResponse, RequestID: req.RequestID,
			Error: &Error{Code: "unknown_method", Message: req.Method},
		})
		return
	}
	respond(req, PingResult{Message: "pong"})
}

func helperWrongID() {
	req := readRequest()
	req.RequestID = "not-" + req.RequestID
	respond(req, PingResult{Message: "pong"})
}

func helperExit1() {
	readRequest()
	fmt.Fprint(os.Stderr, "boom\x1b[31m went\nthe plugin\x07")
	os.Exit(1)
}

func helperIntrospect() {
	req := readRequest()
	cwd, _ := os.Getwd()
	respond(req, map[string]any{"env": os.Environ(), "cwd": cwd})
}

// helperEcho returns the method and data it received.
func helperEcho() {
	req := readRequest()
	respond(req, map[string]any{"method": req.Method, "data": req.Data})
}

// helperAnnotate answers with the given annotations JSON plus a display
// field.
func helperAnnotate(annotations string) func() {
	return func() {
		req := readRequest()
		respond(req, map[string]json.RawMessage{
			"summary":     json.RawMessage(`"looks like a notification"`),
			"annotations": json.RawMessage(annotations),
		})
	}
}

// helperMarker leaves a file in its working directory to prove it ran.
func helperMarker() {
	req := readRequest()
	_ = os.WriteFile(markerFile, nil, 0o644)
	respond(req, PingResult{Message: "pong"})
}

const markerFile = "RAN"

// helperLeak echoes its secret on stdout inside a valid response, and on
// stderr when asked to fail.
func helperLeak() {
	req := readRequest()
	secret := os.Getenv("TIDEMAIL_SECRET_API_KEY")
	if req.Method == "fail" {
		fmt.Fprint(os.Stderr, "auth failed for key "+secret)
		os.Exit(1)
	}
	respond(req, map[string]any{"echo": "key=" + secret, "settings": req.Settings})
}

// helperConcurrent records itself in ../running while it works and reports
// how many plugin processes (and how many of its own plugin) were running.
func helperConcurrent() {
	req := readRequest()
	cwd, _ := os.Getwd()
	own := filepath.Base(cwd)
	dir := filepath.Join(cwd, "..", "running")
	_ = os.MkdirAll(dir, 0o755)
	marker := filepath.Join(dir, own+"-"+strconv.Itoa(os.Getpid()))
	_ = os.WriteFile(marker, nil, 0o644)
	time.Sleep(120 * time.Millisecond)
	entries, _ := os.ReadDir(dir)
	sameOwn := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), own+"-") {
			sameOwn++
		}
	}
	_ = os.Remove(marker)
	respond(req, map[string]int{"global": len(entries), "own": sameOwn})
}

// helperTester answers plugin.test.
func helperTester() {
	req := readRequest()
	respond(req, map[string]any{"ok": true, "message": "connection ok (" + req.Method + ")"})
}

// helperHangPID records its PID in its working directory, then hangs.
func helperHangPID() {
	readRequest()
	_ = os.WriteFile("PID", []byte(strconv.Itoa(os.Getpid())), 0o644)
	time.Sleep(time.Minute)
}

func helperTrailing() {
	req := readRequest()
	respond(req, PingResult{Message: "pong"})
	respond(req, PingResult{Message: "again"})
}

func helperErrResp() {
	req := readRequest()
	_ = json.NewEncoder(os.Stdout).Encode(Response{
		API: APIVersion, Type: TypeResponse, RequestID: req.RequestID,
		Error: &Error{Code: "busy", Message: "try later"},
	})
}

// installPlugin creates root/dirName with a manifest for id whose command is
// a symlink to this test binary. The command's base name selects the helper.
func installPlugin(t *testing.T, root, dirName, id, command string, extraTOML ...string) string {
	t.Helper()
	dir := filepath.Join(root, dirName)
	exePath := filepath.Join(dir, command)
	if err := os.MkdirAll(filepath.Dir(exePath), 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, exePath); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, dir, id, command, extraTOML...)
	return dir
}

func writeManifest(t *testing.T, dir, id, command string, extraTOML ...string) {
	t.Helper()
	manifest := fmt.Sprintf("id = %s\nname = \"Test %s\"\nversion = \"0.1.0\"\napi = 1\ncommand = %s\n",
		strconv.Quote(id), id, strconv.Quote(command)) + strings.Join(extraTOML, "\n")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// managerWith installs one plugin with the given helper command and returns a
// manager that discovered it.
func managerWith(t *testing.T, command string) *Manager {
	t.Helper()
	root := t.TempDir()
	installPlugin(t, root, "p", "p", command)
	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Plugin("p"); !ok {
		t.Fatalf("plugin not discovered: %v", m.Errors())
	}
	return m
}

func TestParseManifestValid(t *testing.T) {
	m, err := ParseManifest([]byte(`
id = "hello"
name = "Hello Plugin"
version = "0.1.0"
api = 1
command = "tidemail-plugin-hello"

[permissions]
message_metadata = true
message_body = false
network = false
annotations = true
`))
	if err != nil {
		t.Fatal(err)
	}
	want := Manifest{
		ID: "hello", Name: "Hello Plugin", Version: "0.1.0", API: 1, Command: "tidemail-plugin-hello",
		Permissions: Permissions{MessageMetadata: true, Annotations: true},
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %+v, want %+v", m, want)
	}
}

func TestParseManifestPermissionsDefaultOff(t *testing.T) {
	m, err := ParseManifest([]byte("id = \"a\"\nname = \"A\"\napi = 1\ncommand = \"a\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Permissions != (Permissions{}) {
		t.Fatalf("permissions = %+v, want all false", m.Permissions)
	}
}

func TestParseManifestRejects(t *testing.T) {
	base := map[string]string{"id": `"a"`, "name": `"A"`, "api": "1", "command": `"a"`}
	tests := []struct {
		name  string
		set   map[string]string
		extra string
		want  string
	}{
		{name: "missing id", set: map[string]string{"id": ""}, want: "id is required"},
		{name: "missing name", set: map[string]string{"name": ""}, want: "name is required"},
		{name: "blank name", set: map[string]string{"name": `"  "`}, want: "name is required"},
		{name: "missing command", set: map[string]string{"command": ""}, want: "command is required"},
		{name: "missing api", set: map[string]string{"api": ""}, want: "unsupported api version 0"},
		{name: "future api", set: map[string]string{"api": "2"}, want: "unsupported api version 2"},
		{name: "display-name id", set: map[string]string{"id": `"Hello World"`}, want: "id \"Hello World\""},
		{name: "absolute command", set: map[string]string{"command": `"/bin/sh"`}, want: "relative path inside"},
		{name: "escaping command", set: map[string]string{"command": `"../other/bin"`}, want: "relative path inside"},
		{name: "unknown key", extra: "\n[permisions]\nnetwork = true\n", want: "unknown keys: permisions"},
		{name: "bad toml", extra: "id = \n", want: "parse manifest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			for _, key := range []string{"id", "name", "api", "command"} {
				v := base[key]
				if override, ok := tt.set[key]; ok {
					v = override
				}
				if v != "" {
					fmt.Fprintf(&b, "%s = %s\n", key, v)
				}
			}
			b.WriteString(tt.extra)
			_, err := ParseManifest([]byte(b.String()))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestDiscoverMissingDirectory(t *testing.T) {
	m, err := Discover(filepath.Join(t.TempDir(), "plugins"))
	if err != nil {
		t.Fatalf("missing directory should not be an error: %v", err)
	}
	if len(m.Plugins()) != 0 || len(m.Errors()) != 0 {
		t.Fatalf("got plugins %v errors %v, want none", m.Plugins(), m.Errors())
	}
}

func TestNilManagerHasNoPlugins(t *testing.T) {
	var m *Manager
	if m.Plugins() != nil || m.Errors() != nil {
		t.Fatal("nil manager should report nothing")
	}
	if _, err := m.Call(context.Background(), "x", MethodPing, nil); !errors.Is(err, ErrUnknownPlugin) {
		t.Fatalf("err = %v, want ErrUnknownPlugin", err)
	}
}

func TestDiscoverIsDeterministic(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		installPlugin(t, root, name, name, "tidemail-plugin-pong")
	}
	// Entries that are not plugin directories are ignored.
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".cache"), 0o755); err != nil {
		t.Fatal(err)
	}

	var first []string
	for range 3 {
		m, err := Discover(root)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, p := range m.Plugins() {
			ids = append(ids, p.Manifest.ID)
		}
		if want := []string{"alpha", "bravo", "charlie"}; !reflect.DeepEqual(ids, want) {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
		if len(m.Errors()) != 0 {
			t.Fatalf("unexpected errors: %v", m.Errors())
		}
		if first == nil {
			first = ids
		} else if !reflect.DeepEqual(ids, first) {
			t.Fatalf("order changed between runs: %v vs %v", ids, first)
		}
	}
}

func TestDiscoverDuplicateIDsDisableEveryCopy(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "one", "same", "tidemail-plugin-pong")
	installPlugin(t, root, "two", "same", "tidemail-plugin-pong")
	installPlugin(t, root, "three", "other", "tidemail-plugin-pong")

	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Plugin("same"); ok {
		t.Fatal("duplicated id should not be callable")
	}
	if _, ok := m.Plugin("other"); !ok {
		t.Fatal("sibling plugin should still be discovered")
	}
	errs := m.Errors()
	if len(errs) != 2 {
		t.Fatalf("errors = %v, want one per duplicate", errs)
	}
	for _, e := range errs {
		if !strings.Contains(e.Error(), `duplicate plugin id "same"`) {
			t.Fatalf("error = %v", e)
		}
	}
}

func TestDiscoverReportsBrokenPluginsAndKeepsSiblings(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "good", "good", "tidemail-plugin-pong")
	// No manifest.
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Malformed manifest.
	if err := os.Mkdir(filepath.Join(root, "garbled"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "garbled", ManifestFile), []byte("id = = ="), 0o644); err != nil {
		t.Fatal(err)
	}
	// Command file missing.
	writeManifest(t, filepath.Join(root, "missingexe"), "missingexe", "nope")
	// Command not executable.
	writeManifest(t, filepath.Join(root, "noexec"), "noexec", "run")
	if err := os.WriteFile(filepath.Join(root, "noexec", "run"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if ids := len(m.Plugins()); ids != 1 {
		t.Fatalf("plugins = %v, want only good", m.Plugins())
	}
	want := map[string]string{
		"empty":      "missing plugin.toml",
		"garbled":    "parse manifest",
		"missingexe": `command "nope"`,
		"noexec":     "not executable",
	}
	errs := m.Errors()
	if len(errs) != len(want) {
		t.Fatalf("errors = %v", errs)
	}
	for _, e := range errs {
		name := filepath.Base(e.Dir)
		if !strings.Contains(e.Error(), want[name]) {
			t.Errorf("%s: error %q, want %q", name, e.Error(), want[name])
		}
	}
}

func TestPingSucceeds(t *testing.T) {
	m := managerWith(t, "tidemail-plugin-pong")
	msg, err := m.Ping(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "pong" {
		t.Fatalf("ping = %q, want pong", msg)
	}
}

func TestCallUnknownMethodReturnsPluginError(t *testing.T) {
	m := managerWith(t, "tidemail-plugin-pong")
	resp, err := m.Call(context.Background(), "p", "frobnicate", json.RawMessage(`{}`))
	var perr *Error
	if !errors.As(err, &perr) || perr.Code != "unknown_method" {
		t.Fatalf("err = %v, want plugin error unknown_method", err)
	}
	if resp.OK {
		t.Fatal("response should not be ok")
	}
}

func TestCallUnknownPlugin(t *testing.T) {
	m := managerWith(t, "tidemail-plugin-pong")
	if _, err := m.Call(context.Background(), "missing", MethodPing, nil); !errors.Is(err, ErrUnknownPlugin) {
		t.Fatalf("err = %v, want ErrUnknownPlugin", err)
	}
}

func TestCallFailures(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"tidemail-plugin-wrongid", "does not match request"},
		{"tidemail-plugin-badjson", "malformed response"},
		{"tidemail-plugin-trailing", "unexpected data after the response"},
		{"tidemail-plugin-silent", "plugin wrote no response"},
		{"tidemail-plugin-huge", "response exceeds"},
		{"tidemail-plugin-errresp", "busy: try later"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			m := managerWith(t, tt.command)
			_, err := m.Ping(context.Background(), "p")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestCallNonZeroExitSanitizesStderr(t *testing.T) {
	m := managerWith(t, "tidemail-plugin-exit1")
	_, err := m.Ping(context.Background(), "p")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "exit status 1") || !strings.Contains(msg, "stderr: boom[31m went the plugin") {
		t.Fatalf("err = %q", msg)
	}
	if strings.ContainsAny(msg, "\x1b\x07\n") {
		t.Fatalf("control characters leaked into error: %q", msg)
	}
}

func TestCallTimeout(t *testing.T) {
	m := managerWith(t, "tidemail-plugin-hang")
	m.Timeout = 200 * time.Millisecond
	start := time.Now()
	_, err := m.Ping(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "timed out after 200ms") {
		t.Fatalf("err = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestCallRespectsCallerCancellation(t *testing.T) {
	m := managerWith(t, "tidemail-plugin-hang")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Ping(ctx, "p"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRelativeExecutableResolvesInPluginDir(t *testing.T) {
	root := t.TempDir()
	dir := installPlugin(t, root, "nested", "nested", "bin/tidemail-plugin-pong")
	// Run discovery from somewhere else so the process cwd cannot help.
	t.Chdir(t.TempDir())

	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := m.Plugin("nested")
	if !ok {
		t.Fatalf("not discovered: %v", m.Errors())
	}
	if want := filepath.Join(dir, "bin", "tidemail-plugin-pong"); p.Executable != want {
		t.Fatalf("executable = %q, want %q", p.Executable, want)
	}
	if msg, err := m.Ping(context.Background(), "nested"); err != nil || msg != "pong" {
		t.Fatalf("ping = %q, %v", msg, err)
	}
}

func TestBareCommandIsNotLookedUpOnPath(t *testing.T) {
	root := t.TempDir()
	// "sh" exists on $PATH but not in the plugin directory.
	writeManifest(t, filepath.Join(root, "p"), "p", "sh")
	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Plugin("p"); ok {
		t.Fatal("command should resolve only inside the plugin directory")
	}
}

func TestCommandIsNotInterpretedByShell(t *testing.T) {
	root := t.TempDir()
	dir := installPlugin(t, root, "p", "p", shellName)
	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if msg, err := m.Ping(context.Background(), "p"); err != nil || msg != "pong" {
		t.Fatalf("ping = %q, %v: %v", msg, err, m.Errors())
	}
	for _, name := range []string{"PWNED", "PWNED2", "PWNED3"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("%s was created: command went through a shell", name)
		}
	}
}

func TestPluginGetsScrubbedEnvironmentAndOwnWorkingDir(t *testing.T) {
	t.Setenv("TIDEMAIL_GOOGLE_CLIENT_SECRET", "sentinel-secret")
	t.Setenv("ANTHROPIC_API_KEY", "sentinel-key")
	m := managerWith(t, "tidemail-plugin-introspect")

	resp, err := m.Call(context.Background(), "p", "introspect", nil)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Env []string `json:"env"`
		Cwd string   `json:"cwd"`
	}
	if err := json.Unmarshal(resp.Data, &got); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, k := range envAllowlist {
		allowed[k] = true
	}
	for _, kv := range got.Env {
		key, _, _ := strings.Cut(kv, "=")
		if !allowed[key] || strings.Contains(kv, "sentinel") {
			t.Errorf("plugin saw %q", kv)
		}
	}
	p, _ := m.Plugin("p")
	if got.Cwd != p.Dir {
		t.Fatalf("cwd = %q, want %q", got.Cwd, p.Dir)
	}
}

func TestDecodeResponseValidatesEnvelope(t *testing.T) {
	req := Request{API: APIVersion, Type: TypeRequest, RequestID: "abc", Method: MethodPing}
	tests := []struct {
		name, body, want string
	}{
		{"wrong api", `{"api":2,"type":"response","request_id":"abc","ok":true}`, "response api 2"},
		{"wrong type", `{"api":1,"type":"request","request_id":"abc","ok":true}`, `response type "request"`},
		{"ok with error", `{"api":1,"type":"response","request_id":"abc","ok":true,"error":{"code":"x"}}`, "ok=true and an error"},
		{"not ok without error", `{"api":1,"type":"response","request_id":"abc","ok":false}`, "ok=false and no error"},
		{"not an object", `[1,2,3]`, "malformed response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeResponse([]byte(tt.body), req)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
	resp, err := decodeResponse([]byte(`{"api":1,"type":"response","request_id":"abc","ok":true,"data":{"message":"pong"}}`+"\n\n"), req)
	if err != nil || !resp.OK {
		t.Fatalf("valid response rejected: %v", err)
	}
}

func TestNewRequestIDsAreUnique(t *testing.T) {
	a, err := NewRequest(MethodPing, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewRequest(MethodPing, nil)
	if a.RequestID == "" || a.RequestID == b.RequestID {
		t.Fatalf("request ids %q and %q", a.RequestID, b.RequestID)
	}
	if err := a.validate(); err != nil {
		t.Fatal(err)
	}
}

const metadataPermission = "[permissions]\nmessage_metadata = true\n"

func TestMessageMetadataSendsPayload(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "p", "p", "tidemail-plugin-echo", metadataPermission)
	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	meta := MessageMetadata{
		ID: 42, MessageID: "<a@b>", From: "Ann <ann@example.com>", Subject: "Hi",
		Date: "2026-09-26T10:00:00Z", Starred: true, Flags: []string{"\\Seen"},
		AccountName: "Work", MailboxName: "INBOX",
	}
	result, err := m.MessageMetadata(context.Background(), "p", meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp := result.Response
	var got struct {
		Method string          `json:"method"`
		Data   MessageMetadata `json:"data"`
	}
	if err := json.Unmarshal(resp.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Method != MethodMessageMetadata || !reflect.DeepEqual(got.Data, meta) {
		t.Fatalf("plugin received %+v", got)
	}
}

func TestMessageMetadataRequiresPermission(t *testing.T) {
	root := t.TempDir()
	dir := installPlugin(t, root, "p", "p", "tidemail-plugin-marker")
	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 1, Subject: "secret"}, nil)
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, markerFile)); statErr == nil {
		t.Fatal("plugin without message_metadata was started")
	}
	// The same plugin does run for methods that need no permission.
	if _, err := m.Ping(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, markerFile)); statErr != nil {
		t.Fatal("marker helper did not run for ping; the check above proves nothing")
	}
}

func TestCallRefusesPermissionGatedMethod(t *testing.T) {
	root := t.TempDir()
	dir := installPlugin(t, root, "p", "p", "tidemail-plugin-marker", metadataPermission)
	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Call(context.Background(), "p", MethodMessageMetadata, json.RawMessage(`{"subject":"x"}`)); err == nil {
		t.Fatal("Call should refuse message.metadata")
	}
	if _, statErr := os.Stat(filepath.Join(dir, markerFile)); statErr == nil {
		t.Fatal("plugin was started through Call")
	}
}

func TestMessageMetadataUnknownPlugin(t *testing.T) {
	var m *Manager
	if _, err := m.MessageMetadata(context.Background(), "x", MessageMetadata{}, nil); !errors.Is(err, ErrUnknownPlugin) {
		t.Fatalf("err = %v, want ErrUnknownPlugin", err)
	}
}

func TestPermissionNames(t *testing.T) {
	if got := (Permissions{}).Names(); got != nil {
		t.Fatalf("no permissions = %v", got)
	}
	got := Permissions{MessageMetadata: true, Network: true}.Names()
	if want := []string{"metadata", "network"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestManifestEvents(t *testing.T) {
	base := "id = \"a\"\nname = \"A\"\napi = 1\ncommand = \"a\"\n"
	m, err := ParseManifest([]byte(base + "events = [\"message.received\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !m.WantsEvent(EventMessageReceived) {
		t.Fatal("declared event not recognized")
	}
	m, err = ParseManifest([]byte(base))
	if err != nil || m.WantsEvent(EventMessageReceived) {
		t.Fatalf("no declaration must mean no events: %v %v", m.Events, err)
	}
	for _, bad := range []string{`events = ["message.sent"]`, `events = ["message.received", "message.received"]`, `events = "message.received"`} {
		if _, err := ParseManifest([]byte(base + bad + "\n")); err == nil {
			t.Errorf("%s: expected rejection", bad)
		}
	}
}
