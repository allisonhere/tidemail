package main

import (
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

func TestSeedBuildsReadableSandbox(t *testing.T) {
	dir := t.TempDir()
	// seed redirects the app's config and data paths into the sandbox.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")
	if err := seed(dir); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(cfg.Accounts))
	}
	for _, a := range cfg.Accounts {
		if a.IMAPHost != "" {
			t.Fatalf("demo account %q must have no mail server", a.Name)
		}
	}

	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	accounts, err := database.ListAccounts()
	if err != nil || len(accounts) != 2 {
		t.Fatalf("db accounts = %d, err %v", len(accounts), err)
	}
	total := 0
	for _, a := range accounts {
		boxes, err := database.ListMailboxes(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range boxes {
			msgs, err := database.ListMessages(b.ID)
			if err != nil {
				t.Fatal(err)
			}
			total += len(msgs)
		}
	}
	if want := len(personalMessages()) + len(workMessages()); total != want {
		t.Fatalf("messages = %d, want %d", total, want)
	}
}
