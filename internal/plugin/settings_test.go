package plugin

import (
	"reflect"
	"strings"
	"testing"
)

const settingsBase = "id = \"s\"\nname = \"S\"\napi = 1\ncommand = \"s\"\n"

const smartSettings = `
capabilities = ["plugin.test"]

[[settings]]
key = "mode"
label = "Classification mode"
type = "select"
default = "hybrid"
options = ["local", "hybrid", "jev"]

[[settings]]
key = "jev_enabled"
label = "TypeSafe / Jev"
type = "bool"
default = true

[[settings]]
key = "jev_api_key"
label = "TypeSafe API key"
type = "secret"
help = "Sent only to api.typesafe.ai."

[[settings]]
key = "quiet"
label = "Quiet"
type = "bool"
`

func TestManifestSettingsParse(t *testing.T) {
	m, err := ParseManifest([]byte(settingsBase + smartSettings))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Settings) != 4 || !m.HasCapability(CapabilityTest) {
		t.Fatalf("manifest = %+v", m)
	}
	mode, _ := m.Setting("mode")
	if mode.Type != SettingSelect || mode.DefaultValue() != "hybrid" || len(mode.Options) != 3 {
		t.Fatalf("mode = %+v", mode)
	}
	jev, _ := m.Setting("jev_enabled")
	if jev.DefaultValue() != true {
		t.Fatalf("jev_enabled default = %v", jev.DefaultValue())
	}
	quiet, _ := m.Setting("quiet")
	if quiet.DefaultValue() != false {
		t.Fatal("bool without default should default to false")
	}
	key, _ := m.Setting("jev_api_key")
	if key.Type != SettingSecret || key.DefaultValue() != nil || key.Help == "" {
		t.Fatalf("secret = %+v", key)
	}
}

func TestManifestSettingsRejects(t *testing.T) {
	tests := []struct {
		name, toml, want string
	}{
		{"duplicate key", "[[settings]]\nkey=\"a\"\nlabel=\"A\"\ntype=\"bool\"\n[[settings]]\nkey=\"a\"\nlabel=\"B\"\ntype=\"bool\"\n", "duplicate key"},
		{"bad key", "[[settings]]\nkey=\"Mode\"\nlabel=\"A\"\ntype=\"bool\"\n", "key must be"},
		{"unsupported type", "[[settings]]\nkey=\"a\"\nlabel=\"A\"\ntype=\"number\"\n", "unsupported type"},
		{"select default not an option", "[[settings]]\nkey=\"a\"\nlabel=\"A\"\ntype=\"select\"\noptions=[\"x\",\"y\"]\ndefault=\"z\"\n", "default must be one of the options"},
		{"select without options", "[[settings]]\nkey=\"a\"\nlabel=\"A\"\ntype=\"select\"\n", "select needs"},
		{"duplicate option", "[[settings]]\nkey=\"a\"\nlabel=\"A\"\ntype=\"select\"\noptions=[\"x\",\"x\"]\n", "must be unique"},
		{"secret default", "[[settings]]\nkey=\"a\"\nlabel=\"A\"\ntype=\"secret\"\ndefault=\"hunter2\"\n", "cannot have a default"},
		{"bool default wrong type", "[[settings]]\nkey=\"a\"\nlabel=\"A\"\ntype=\"bool\"\ndefault=\"yes\"\n", "bool default"},
		{"missing label", "[[settings]]\nkey=\"a\"\ntype=\"bool\"\n", "label is required"},
		{"long label", "[[settings]]\nkey=\"a\"\nlabel=\"" + strings.Repeat("x", MaxSettingLabelLen+1) + "\"\ntype=\"bool\"\n", "label is required"},
		{"escape in label", "[[settings]]\nkey=\"a\"\nlabel=\"A\\u001b[31m\"\ntype=\"bool\"\n", "label is required"},
		{"unknown field", "[[settings]]\nkey=\"a\"\nlabel=\"A\"\ntype=\"bool\"\nplaceholder=\"x\"\n", "unknown keys"},
		{"unknown capability", "capabilities = [\"plugin.run\"]\n", "unknown capability"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(settingsBase + tt.toml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestResolveSettings(t *testing.T) {
	m, err := ParseManifest([]byte(settingsBase + smartSettings))
	if err != nil {
		t.Fatal(err)
	}
	// Nothing stored: defaults.
	got := ResolveSettings(m.Settings, nil)
	want := map[string]any{"mode": "hybrid", "jev_enabled": true, "quiet": false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %v", got)
	}
	// Stored values win when valid; bad ones fall back; secrets and stale or
	// foreign keys never appear.
	got = ResolveSettings(m.Settings, map[string]any{
		"mode":        "jev",
		"jev_enabled": "yes", // wrong type
		"quiet":       true,
		"jev_api_key": "leaked?",
		"stale":       "x",
	})
	want = map[string]any{"mode": "jev", "jev_enabled": true, "quiet": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved = %v", got)
	}
	got = ResolveSettings(m.Settings, map[string]any{"mode": "removed-option"})
	if got["mode"] != "hybrid" {
		t.Fatalf("invalid stored select should fall back, got %v", got["mode"])
	}
}

func TestSecretEnvVar(t *testing.T) {
	if got := SecretEnvVar("jev_api_key"); got != "TIDEMAIL_SECRET_JEV_API_KEY" {
		t.Fatalf("env var = %q", got)
	}
}
