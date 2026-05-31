package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestSaveConfigSurfacesError(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	orig := configSave
	configSave = func(config.Config) error { return fmt.Errorf("disk full") }
	defer func() { configSave = orig }()

	m.saveConfig()
	if !m.statusErr {
		t.Fatal("expected the status line to be flagged as an error")
	}
	if !strings.Contains(m.statusMsg, "disk full") {
		t.Fatalf("expected the save error surfaced on the status line, got %q", m.statusMsg)
	}
}

func TestSaveConfigSuccessNoError(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	orig := configSave
	configSave = func(config.Config) error { return nil }
	defer func() { configSave = orig }()

	m.saveConfig()
	if m.statusErr {
		t.Fatalf("did not expect an error status on success, got %q", m.statusMsg)
	}
}

func TestValidateAccountForConnect(t *testing.T) {
	base := config.AccountConfig{Name: "Acct", IMAPHost: "imap.x", User: "u", IMAPPort: 993, SMTPPort: 587}
	if got := validateAccountForConnect(base); got != "" {
		t.Fatalf("valid config rejected: %q", got)
	}
	cases := []struct {
		name string
		mut  func(*config.AccountConfig)
	}{
		{"missing name", func(c *config.AccountConfig) { c.Name = "" }},
		{"missing imap host", func(c *config.AccountConfig) { c.IMAPHost = "" }},
		{"missing user", func(c *config.AccountConfig) { c.User = "" }},
		{"imap port too high", func(c *config.AccountConfig) { c.IMAPPort = 70000 }},
		{"smtp port zero", func(c *config.AccountConfig) { c.SMTPPort = 0 }},
		{"imap port negative", func(c *config.AccountConfig) { c.IMAPPort = -1 }},
	}
	for _, tc := range cases {
		cfg := base
		tc.mut(&cfg)
		if got := validateAccountForConnect(cfg); got == "" {
			t.Errorf("%s: expected a validation error, got none", tc.name)
		}
	}
}
