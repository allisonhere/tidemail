package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
	tea "github.com/charmbracelet/bubbletea"
)

func TestQuitFailuresNamesItemsAndFirstCause(t *testing.T) {
	var f quitFailures
	f.total = 5
	boom := errors.New("connection reset")
	failed := []db.Message{{Subject: "A"}, {Subject: "B"}, {Subject: "C"}, {Subject: "D"}}
	f.collect(DestructiveActionResultMsg{Kind: destructiveDelete, Failed: failed, RemoteErr: boom, Err: boom})
	err := f.error("message(s), left unchanged in their folders")
	// The line as main.go prints it, so the prefix and the text read as one.
	line := fmt.Sprintf("pending message action failed: %v", err)
	want := `pending message action failed: 4 of 5 message(s), left unchanged in their folders: delete "A", delete "B", delete "C" and 1 more: connection reset`
	if line != want {
		t.Fatalf("got  %s\nwant %s", line, want)
	}
	if !errors.Is(err, boom) {
		t.Fatal("the first cause should stay wrapped")
	}
}

func TestQuitFailuresNilWhenNothingFailed(t *testing.T) {
	var f quitFailures
	f.total = 2
	if err := f.error("message(s)"); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestQuitFailuresPrefersServerErrorForFailedMessages(t *testing.T) {
	var f quitFailures
	f.total = 2
	cacheErr, remoteErr := errors.New("disk full"), errors.New("NO [TRYCREATE]")
	// Batch 1's cache write failed first, then batch 2 failed on the server.
	f.collect(DestructiveActionResultMsg{Kind: destructiveMove, Failed: []db.Message{{Subject: "B"}}, Err: cacheErr, RemoteErr: remoteErr, CacheErr: cacheErr})
	err := f.error("message(s)")
	if !errors.Is(err, remoteErr) || strings.Contains(err.Error(), "disk full") {
		t.Fatalf("got %v", err)
	}
}

func TestQuitFailuresCacheOnlyFailure(t *testing.T) {
	var f quitFailures
	f.total = 1
	diskFull := errors.New("disk full")
	f.collect(DestructiveActionResultMsg{Kind: destructiveDelete, Err: diskFull, CacheErr: diskFull})
	err := f.error("message(s)")
	if err == nil || !strings.HasPrefix(err.Error(), "delete done on the server, local cache not updated") || !errors.Is(err, diskFull) {
		t.Fatalf("got %v", err)
	}
}

func TestQuotedSubject(t *testing.T) {
	if got := quotedSubject("  "); got != `"(no subject)"` {
		t.Fatalf("empty: %s", got)
	}
	if got := quotedSubject("a\x1b[31mb"); strings.ContainsRune(got, '\x1b') {
		t.Fatalf("control character not escaped: %s", got)
	}
	long := strings.Repeat("é", 100)
	if got := quotedSubject(long); len([]rune(got)) != maxQuitSubjectRunes+2 || !strings.HasSuffix(got, `…"`) {
		t.Fatalf("long subject not cut to %d runes: %s", maxQuitSubjectRunes, got)
	}
}

func sentResult(result MessageSentMsg) *sendRuntime {
	return &sendRuntime{command: func() tea.Msg { return result }}
}

func TestFlushPendingSendsReport(t *testing.T) {
	refused := errors.New("550 mailbox unavailable")
	recordErr := errors.New("record sent status: database is locked")
	m := Model{pendingSends: []pendingSend{
		{Msg: smtp.OutgoingMessage{Subject: "Delivered"}, Runtime: sentResult(MessageSentMsg{})},
		{Msg: smtp.OutgoingMessage{Subject: "Refused"}, Runtime: sentResult(MessageSentMsg{Err: refused})},
		{Msg: smtp.OutgoingMessage{Subject: "Untidy"}, Runtime: sentResult(MessageSentMsg{CleanupErr: recordErr})},
	}}
	err := m.FlushPendingSends()
	if err == nil {
		t.Fatal("expected an error")
	}
	got := err.Error()
	for _, want := range []string{"2 of 3 message(s)", `"Refused"`, `"Untidy" (sent; cleanup failed)`, "550 mailbox unavailable"} {
		if !strings.Contains(got, want) {
			t.Errorf("error %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, `"Refused" (sent`) || strings.Contains(got, "Delivered") {
		t.Errorf("error %q mislabels a message", got)
	}
}

func TestFlushWaitsForCommittingActionAndReportsIt(t *testing.T) {
	runtime := &destructiveActionRuntime{done: make(chan struct{})}
	m := Model{pendingDestructiveActions: []pendingDestructiveAction{{ID: 1, Kind: destructiveArchive, Committing: true, Runtime: runtime, Entries: make([]pendingDestructiveEntry, 1)}}}
	boom := errors.New("timeout")
	runtime.result = DestructiveActionResultMsg{Kind: destructiveArchive, Failed: []db.Message{{Subject: "X"}}, RemoteErr: boom, Err: boom}
	close(runtime.done)
	err := m.FlushPendingDestructiveActions()
	if err == nil || !strings.Contains(err.Error(), `1 of 1 message(s)`) || !strings.Contains(err.Error(), `archive "X"`) {
		t.Fatalf("got %v", err)
	}
}

func TestPendingSummary(t *testing.T) {
	var m Model
	if got := m.PendingSummary(); got != "" {
		t.Fatalf("empty model: %q", got)
	}
	m.pendingSends = []pendingSend{{}}
	m.pendingDestructiveActions = []pendingDestructiveAction{{Entries: make([]pendingDestructiveEntry, 2)}}
	got := m.PendingSummary()
	for _, want := range []string{"1 unsent message(s)", "sent when due", "2 archive/move/delete"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "next launch") {
		t.Errorf("summary %q promises a time it cannot know", got)
	}
}
