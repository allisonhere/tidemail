package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// memorySettings is a SettingsSource backed by maps, recording which secrets
// were asked for.
type memorySettings struct {
	mu      sync.Mutex
	stored  map[string]map[string]any
	secrets map[string]map[string]string
	asked   []string
}

func (s *memorySettings) StoredSettings(pluginID string) map[string]any {
	return s.stored[pluginID]
}

func (s *memorySettings) Secret(pluginID, key string) (string, bool, error) {
	s.mu.Lock()
	s.asked = append(s.asked, pluginID+"/"+key)
	s.mu.Unlock()
	v, ok := s.secrets[pluginID][key]
	return v, ok, nil
}

const leakManifest = `
[[settings]]
key = "mode"
label = "Mode"
type = "select"
default = "hybrid"
options = ["local", "hybrid"]

[[settings]]
key = "api_key"
label = "API key"
type = "secret"
`

func TestSettingsReachOnlyTheirPlugin(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "a", "a", "tidemail-plugin-leak", leakManifest)
	installPlugin(t, root, "b", "b", "tidemail-plugin-introspect", leakManifest)
	m, err := Discover(root)
	if err != nil || len(m.Errors()) > 0 {
		t.Fatalf("discover: %v %v", err, m.Errors())
	}
	src := &memorySettings{
		stored: map[string]map[string]any{"a": {"mode": "local", "other": "x"}},
		secrets: map[string]map[string]string{
			"a": {"api_key": "sk-alpha-123456", "undeclared": "sk-nope-000000"},
			"b": {"api_key": "sk-bravo-654321"},
		},
	}
	m.Settings = src

	resp, err := m.Call(context.Background(), "a", "echo", nil)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Echo     string         `json:"echo"`
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(resp.Data, &got); err != nil {
		t.Fatal(err)
	}
	// Resolved, declared settings only; never the secret.
	if got.Settings["mode"] != "local" || len(got.Settings) != 1 {
		t.Fatalf("settings = %v", got.Settings)
	}
	// The plugin received its secret, and TideMail masked the echo.
	if got.Echo != "key=********" {
		t.Fatalf("echo = %q", got.Echo)
	}
	if strings.Contains(string(resp.Data), "sk-") {
		t.Fatalf("a secret leaked into the response: %s", resp.Data)
	}

	// Plugin b sees only its own secret variable, never a's.
	resp, err = m.Call(context.Background(), "b", "introspect", nil)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Env []string `json:"env"`
	}
	if err := json.Unmarshal(resp.Data, &env); err != nil {
		t.Fatal(err)
	}
	var secretVars []string
	for _, kv := range env.Env {
		if strings.HasPrefix(kv, "TIDEMAIL_SECRET_") {
			secretVars = append(secretVars, kv)
		}
	}
	if len(secretVars) != 1 || secretVars[0] != "TIDEMAIL_SECRET_API_KEY=********" {
		t.Fatalf("secret env = %v", secretVars)
	}
	if strings.Contains(string(resp.Data), "sk-alpha") {
		t.Fatal("plugin b saw plugin a's secret")
	}
	for _, asked := range src.asked {
		if strings.HasSuffix(asked, "/undeclared") {
			t.Fatal("an undeclared secret was read")
		}
	}
}

func TestSecretsMaskedInErrors(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "a", "a", "tidemail-plugin-leak", leakManifest)
	m, _ := Discover(root)
	m.Settings = &memorySettings{secrets: map[string]map[string]string{"a": {"api_key": "sk-alpha-123456"}}}
	_, err := m.Call(context.Background(), "a", "fail", nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(err.Error(), "sk-alpha") || !strings.Contains(err.Error(), "********") {
		t.Fatalf("error = %q", err)
	}
}

func TestPluginsWithoutSettingsGetNone(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "p", "p", "tidemail-plugin-leak")
	m, _ := Discover(root)
	src := &memorySettings{secrets: map[string]map[string]string{"p": {"api_key": "sk-alpha-123456"}}}
	m.Settings = src
	resp, err := m.Call(context.Background(), "p", "echo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp.Data), `"echo":"key="`) || len(src.asked) != 0 {
		t.Fatalf("a plugin without declared settings got some: %s (asked %v)", resp.Data, src.asked)
	}
}

func TestTestCapability(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "yes", "yes", "tidemail-plugin-tester", "capabilities = [\"plugin.test\"]\n")
	installPlugin(t, root, "no", "no", "tidemail-plugin-tester")
	m, err := Discover(root)
	if err != nil || len(m.Errors()) > 0 {
		t.Fatalf("discover: %v %v", err, m.Errors())
	}
	resp, err := m.Test(context.Background(), "yes")
	if err != nil || !strings.Contains(string(resp.Data), "connection ok (plugin.test)") {
		t.Fatalf("resp = %s, err = %v", resp.Data, err)
	}
	if _, err := m.Test(context.Background(), "no"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}
