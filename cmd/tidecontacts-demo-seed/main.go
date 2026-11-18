// Command tidecontacts-demo-seed initializes fictional TideMail data in the
// caller's explicitly isolated XDG profile. It never resolves a live profile.
package main

import (
	"fmt"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := seed(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func seed() error {
	if os.Getenv("TIDECONTACTS_DEMO") != "1" || os.Getenv("XDG_CONFIG_HOME") == "" || os.Getenv("XDG_DATA_HOME") == "" {
		return fmt.Errorf("requires the isolated TideContacts demo launcher")
	}
	configRoot := os.Getenv("XDG_CONFIG_HOME")
	dataRoot := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(configRoot) || !filepath.IsAbs(dataRoot) {
		return fmt.Errorf("demo paths must be absolute")
	}
	if _, err := os.Stat(filepath.Join(configRoot, "tidemail", "config.toml")); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(configRoot, "tidemail"), 0700); err != nil {
		return err
	}
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{ID: "contacts-demo", Name: "Demo", User: "me@example.com", From: "Demo <me@example.com>", Provider: "custom", SyncMinutes: -1}}
	if err := config.Save(cfg); err != nil {
		return err
	}
	d, err := db.Open()
	if err != nil {
		return err
	}
	defer d.Close()
	_, err = d.Exec(`INSERT INTO accounts(id,name) VALUES(1,'Demo'); INSERT INTO mailboxes(id,account_id,name) VALUES(1,1,'INBOX')`)
	if err != nil {
		return err
	}
	for i := 1; i <= 6; i++ {
		_, err = d.Exec(`INSERT INTO messages(mailbox_id,uid,message_id,subject,from_addr,to_addr,date) VALUES(1,?,?,?,?,?,?)`, i, fmt.Sprintf("<demo%d@example.com>", i), "Fictional planning conversation", fmt.Sprintf("person%d@example.com", i), "me@example.com", time.Now().Add(-time.Duration(i)*time.Hour).Unix())
		if err != nil {
			return err
		}
	}
	return nil
}
