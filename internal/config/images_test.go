package config

import (
	"bytes"
	"github.com/BurntSushi/toml"
	"testing"
)

func TestImagePreviewConfigPersistence(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Display.ImagePreviews {
		t.Fatal("default should be disabled")
	}
	cfg.Display.ImagePreviews = true
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).Encode(cfg); err != nil {
		t.Fatal(err)
	}
	var restored Config
	if _, err := toml.Decode(out.String(), &restored); err != nil {
		t.Fatal(err)
	}
	if !restored.Display.ImagePreviews {
		t.Fatal("setting lost")
	}
	var legacy Config
	if _, err := toml.Decode("[display]\nreading_width = 80\n", &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Display.ImagePreviews {
		t.Fatal("legacy config enables images")
	}
}
