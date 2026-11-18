package mailcore

import (
	"context"
	"strings"
	"testing"
)

func TestZeroUIDCannotTargetNewestMessage(t *testing.T) {
	account := startIMAP(t)
	uid := seed(t, account, "INBOX", "Keep this message", "unchanged", false)
	c := connect(t, account)
	ctx := context.Background()
	actions := map[string]func() error{
		"fetch":       func() error { _, err := c.FetchMessage(ctx, 0); return err },
		"fetch from":  func() error { _, err := c.FetchMessageFrom(ctx, "INBOX", 0); return err },
		"read":        func() error { return c.SetRead(ctx, 0, true) },
		"read in":     func() error { return c.SetReadIn(ctx, "INBOX", 0, true) },
		"flag":        func() error { return c.SetFlagged(ctx, 0, true) },
		"flag in":     func() error { return c.SetFlaggedIn(ctx, "INBOX", 0, true) },
		"move":        func() error { return c.Move(ctx, 0, "Archive") },
		"move from":   func() error { return c.MoveFrom(ctx, "INBOX", 0, "Archive") },
		"delete":      func() error { return c.Delete(ctx, 0) },
		"delete from": func() error { return c.DeleteFrom(ctx, "INBOX", 0) },
	}
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			if err := action(); err == nil || !strings.Contains(err.Error(), "UID") {
				t.Fatalf("expected invalid UID error, got %v", err)
			}
			if flags := flagsOf(t, account, "INBOX", uid); flags[`\seen`] || flags[`\flagged`] {
				t.Fatalf("newest message flags changed: %v", flags)
			}
		})
	}
}
