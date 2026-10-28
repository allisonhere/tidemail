// Command demoseed builds a throwaway TideMail sandbox full of made-up mail, so a
// README demo can be recorded without touching a real inbox.
//
//	go run ./cmd/demoseed /tmp/tidemail-demo
//	XDG_CONFIG_HOME=/tmp/tidemail-demo/config XDG_DATA_HOME=/tmp/tidemail-demo/data tidemail
//
// Nothing here talks to a mail server: the accounts have no IMAP host, so the app
// shows the seeded messages and never syncs.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

type seedMessage struct {
	from, to, subject, text, html string
	ago                           time.Duration
	read, starred, attachment     bool
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: demoseed <sandbox-dir>")
		os.Exit(2)
	}
	if err := seed(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "demoseed:", err)
		os.Exit(1)
	}
	fmt.Println("sandbox ready:", os.Args[1])
}

func seed(dir string) error {
	cfgDir := filepath.Join(dir, "config")
	dataDir := filepath.Join(dir, "data")
	for _, d := range []string{cfgDir, dataDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	// Point the app's path helpers at the sandbox for this process only.
	if err := os.Setenv("XDG_CONFIG_HOME", cfgDir); err != nil {
		return err
	}
	if err := os.Setenv("XDG_DATA_HOME", dataDir); err != nil {
		return err
	}
	if err := os.Setenv("HOME", dir); err != nil {
		return err
	}

	cfg := config.DefaultConfig()
	cfg.Theme = "catppuccin-mocha"
	cfg.Accounts = []config.AccountConfig{
		{ID: "demo-personal", Name: "Personal", Provider: "custom", User: "alex@example.com", From: "Alex Rivera <alex@example.com>", SyncMinutes: -1},
		{ID: "demo-work", Name: "Work", Provider: "custom", User: "alex@acme.example", From: "Alex Rivera <alex@acme.example>", SyncMinutes: -1},
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	database, err := db.Open()
	if err != nil {
		return err
	}
	defer database.Close()

	now := time.Now()
	for _, acc := range []struct {
		id, name, color string
		msgs            []seedMessage
	}{
		{"demo-personal", "Personal", "", personalMessages()},
		{"demo-work", "Work", "", workMessages()},
	} {
		aid, err := database.AddAccount(acc.id, acc.name, acc.color)
		if err != nil {
			return err
		}
		for _, name := range []string{"Sent", "Drafts", "Archive"} {
			if _, err := database.UpsertMailbox(db.Mailbox{AccountID: aid, Name: name, Delimiter: "/"}); err != nil {
				return err
			}
		}
		inbox, err := database.UpsertMailbox(db.Mailbox{AccountID: aid, Name: "INBOX", Delimiter: "/"})
		if err != nil {
			return err
		}
		for i, m := range acc.msgs {
			err := database.UpsertMessage(db.Message{
				MailboxID: inbox, UID: uint32(i + 1),
				MessageID: fmt.Sprintf("<demo-%s-%d@example.invalid>", acc.id, i+1),
				Subject:   m.subject, From: m.from, To: m.to,
				Date: now.Add(-m.ago), BodyText: m.text, BodyHTML: m.html,
				Read: m.read, Starred: m.starred, HasAttachment: m.attachment,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
