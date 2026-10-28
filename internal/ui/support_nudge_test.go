package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

func nudgeDB(t *testing.T) *db.DB {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestSupportNudgeScheduling(t *testing.T) {
	database := nudgeDB(t)
	now := time.Now()

	if supportNudgeCmd(database, false, now) != nil {
		t.Fatal("a disabled reminder must not schedule")
	}
	if supportNudgeCmd(nil, true, now) != nil {
		t.Fatal("no database, nothing to schedule")
	}
	// First launch only records the date.
	if supportNudgeCmd(database, true, now) != nil {
		t.Fatal("first launch must not schedule")
	}
	if v, _ := database.GetSetting(supportFirstSeenKey); v != strconv.FormatInt(now.Unix(), 10) {
		t.Fatalf("first_seen = %q", v)
	}
	// Six days in: still too early.
	if supportNudgeCmd(database, true, now.Add(6*24*time.Hour)) != nil {
		t.Fatal("must wait a week")
	}
	// Eight days in: scheduled.
	if supportNudgeCmd(database, true, now.Add(8*24*time.Hour)) == nil {
		t.Fatal("expected the note to be scheduled after a week")
	}
	// Once shown it never comes back.
	if err := database.SetSetting(supportNudgedKey, "1"); err != nil {
		t.Fatal(err)
	}
	if supportNudgeCmd(database, true, now.Add(30*24*time.Hour)) != nil {
		t.Fatal("must not repeat")
	}
}

func TestSupportNudgeShownOnceAndRecorded(t *testing.T) {
	database := nudgeDB(t)
	cfg := config.DefaultConfig()
	m := NewModel(database, cfg, "dev", false)

	next, _ := m.Update(SupportNudgeMsg{})
	got := next.(Model)
	if got.statusErr || !strings.Contains(got.statusMsg, "support it in Settings") {
		t.Fatalf("status = %q", got.statusMsg)
	}
	if v, _ := database.GetSetting(supportNudgedKey); v != "1" {
		t.Fatal("shown note was not recorded")
	}
}

func TestSupportNudgeWaitsForOpenDialog(t *testing.T) {
	database := nudgeDB(t)
	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.overlay = overlayHelp
	next, cmd := m.Update(SupportNudgeMsg{})
	if next.(Model).statusMsg != "" || cmd == nil {
		t.Fatalf("dialog open: status=%q cmd=%v", next.(Model).statusMsg, cmd)
	}
	if v, _ := database.GetSetting(supportNudgedKey); v == "1" {
		t.Fatal("an undisplayed note must not be recorded as shown")
	}
}

func TestSupportNudgeRespectsDisabledSetting(t *testing.T) {
	database := nudgeDB(t)
	cfg := config.DefaultConfig()
	cfg.Display.SupportReminder = false
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(SupportNudgeMsg{})
	if next.(Model).statusMsg != "" {
		t.Fatalf("status = %q", next.(Model).statusMsg)
	}
}
