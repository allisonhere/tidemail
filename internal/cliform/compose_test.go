package cliform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/mailapp"
	"github.com/allisonhere/tidemail/internal/smtp"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func testCompose(t *testing.T, ops operations) composeModel {
	t.Helper()
	a := config.AccountConfig{ID: "a", Name: "Account", User: "me@example.com", Signature: "My signature"}
	cfg := config.Config{Accounts: []config.AccountConfig{a}, Display: config.DisplayConfig{SendDelaySeconds: 10}}
	return newCompose(cfg, a, db.Draft{To: "you@example.com", Subject: "Report", BodyText: "Hello"}, ComposeOptions{Signature: true}, ops, styles(os.Stderr))
}
func press(m composeModel, key tea.KeyType) (composeModel, tea.Cmd) {
	next, cmd := m.Update(tea.KeyMsg{Type: key})
	return next.(composeModel), cmd
}
func apply(m composeModel, msg tea.Msg) (composeModel, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(composeModel), cmd
}
func TestWizardAdvancesAndPreservesEdits(t *testing.T) {
	m := testCompose(t, operations{})
	m, _ = press(m, tea.KeyEnter)
	if m.step != stepTo {
		t.Fatal("account did not advance", m.step)
	}
	m.input.SetValue("changed@example.com")
	m, _ = press(m, tea.KeyEnter)
	if m.step != stepCC || m.draft.To != "changed@example.com" {
		t.Fatal(m.step, m.draft.To)
	}
	m, _ = press(m, tea.KeyShiftTab)
	if m.step != stepTo || m.input.Value() != "changed@example.com" {
		t.Fatal("back lost input")
	}
	m.input.SetValue("invalid")
	m, _ = press(m, tea.KeyEnter)
	if m.step != stepTo || m.err == "" {
		t.Fatal("invalid recipient advanced")
	}
	m.setStep(stepBody)
	before := m.editor.Value()
	m, _ = press(m, tea.KeyEnter)
	if !strings.Contains(m.editor.Value(), "\n") || m.step != stepBody {
		t.Fatal("enter should insert newline", m.editor.Value())
	}
	m, _ = press(m, tea.KeyCtrlS)
	if m.step != stepSignature || m.draft.BodyText == before {
		t.Fatal("body did not persist")
	}
}
func TestWizardDefaultsToDraftAndRequiresExplicitSend(t *testing.T) {
	saves, sends := 0, 0
	ops := operations{save: func(_ config.AccountConfig, d db.Draft) (int64, error) {
		saves++
		if d.BodyText != "Hello" {
			t.Fatal(d)
		}
		return 7, nil
	}, enqueue: func(config.AccountConfig, smtp.OutgoingMessage, db.Draft, time.Time) (int64, error) {
		sends++
		return 8, nil
	}}
	m := testCompose(t, ops)
	m.setStep(stepReview)
	if saves != 0 || sends != 0 {
		t.Fatal("automatic mutation")
	}
	m, cmd := press(m, tea.KeyEnter)
	if m.step != stepLoading || cmd == nil {
		t.Fatal("save was not dispatched")
	}
	m, _ = apply(m, cmd())
	if saves != 1 || sends != 0 || m.outcome.State != "drafted" || m.outcome.ID != 7 {
		t.Fatal(m.outcome, saves, sends)
	}
	m = testCompose(t, ops)
	m.setStep(stepReview)
	m.cursor = 1
	m, cmd = press(m, tea.KeyEnter)
	if sends != 0 || cmd == nil {
		t.Fatal("send must run in a command")
	}
	m, _ = apply(m, cmd())
	if sends != 1 || m.step != stepCountdown {
		t.Fatal(m.step, sends)
	}
}
func TestUndoStopsTimerAndPreservesDraft(t *testing.T) {
	sends := 0
	ops := operations{cancel: func(id int64) (db.Draft, error) {
		if id != 42 {
			t.Fatal(id)
		}
		return db.Draft{ID: 9, To: "you@example.com", Subject: "Report", BodyText: "Hello"}, nil
	}, deliver: func(int64, config.AccountConfig) mailapp.Result { sends++; return mailapp.Result{} }}
	m := testCompose(t, ops)
	m.step = stepCountdown
	m.outcome.ID = 42
	m.due = time.Now().Add(time.Hour)
	m, cmd := press(m, tea.KeyCtrlZ)
	if m.step != stepLoading || cmd == nil {
		t.Fatal(m.step)
	}
	m, unexpected := apply(m, countdownTick(time.Now().Add(2*time.Hour)))
	if unexpected != nil {
		t.Fatal("stale timer sent mail")
	}
	m, _ = apply(m, cmd())
	if m.step != stepReview || m.draft.ID != 9 || sends != 0 {
		t.Fatal(m.step, m.draft, sends)
	}
}
func TestAgentApprovalAndDeliveryStates(t *testing.T) {
	m := testCompose(t, operations{})
	m.opts.Agent = true
	m.setStep(stepReview)
	m.cursor = 1
	m, cmd := press(m, tea.KeyEnter)
	if cmd != nil || m.err != "Agent capability denied: mail.send" {
		t.Fatal("agent send bypass", m.err)
	}
	for _, state := range []string{db.OutboxSent, db.OutboxQueued, db.OutboxFailed, db.OutboxUncertain} {
		m = testCompose(t, operations{})
		m.step = stepSending
		m.outcome.ID = 42
		m, _ = apply(m, delivered{item: db.OutboxItem{State: state, NextAttempt: 123}})
		if m.outcome.State != state || m.step != stepResult {
			t.Fatal(m.outcome)
		}
		if (m.outcome.Err != nil) != (state == db.OutboxFailed || state == db.OutboxUncertain) {
			t.Fatal(m.outcome)
		}
	}
}
func TestCancellationAndSaveFailureKeepContent(t *testing.T) {
	m := testCompose(t, operations{save: func(config.AccountConfig, db.Draft) (int64, error) { return 0, errors.New("disk full") }})
	m.setStep(stepBody)
	m.editor.SetValue("unfinished")
	m, _ = press(m, tea.KeyCtrlC)
	if m.step != stepCancel || m.draft.BodyText != "unfinished" {
		t.Fatal(m.step)
	}
	m, _ = press(m, tea.KeyEnter)
	if m.step != stepBody || m.editor.Value() != "unfinished" {
		t.Fatal("resume lost content")
	}
	m.setStep(stepReview)
	m, cmd := press(m, tea.KeyEnter)
	m, _ = apply(m, cmd())
	if m.step != stepReview || m.draft.BodyText != "unfinished" || m.err == "" {
		t.Fatal("save failure lost draft")
	}
}
func TestAttachmentsSnapshotAndRegularFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.txt")
	if e := os.WriteFile(path, []byte("approved contents"), 0600); e != nil {
		t.Fatal(e)
	}
	att, e := LoadAttachment(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, []byte("changed later"), 0600); e != nil {
		t.Fatal(e)
	}
	if string(att.Data) != "approved contents" {
		t.Fatal("contents changed")
	}
	if _, e = LoadAttachment(filepath.Dir(path)); e == nil {
		t.Fatal("directory accepted")
	}
	alias := filepath.Join(filepath.Dir(path), "alias")
	if e = os.Symlink(path, alias); e != nil {
		t.Fatal(e)
	}
	if !sameFile(path, alias) {
		t.Fatal("symlink duplicate missed")
	}
	m := testCompose(t, operations{})
	m.step = stepLoading
	m, _ = apply(m, attachmentLoaded{attachment: att})
	if m.step != stepAttachments || len(m.draft.Attachments) != 1 {
		t.Fatal(m.step)
	}
	m, _ = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if len(m.draft.Attachments) != 0 {
		t.Fatal("remove failed")
	}
}
func TestWizardViewsStayBoundedAndNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := testCompose(t, operations{})
	m.draft.Subject = strings.Repeat("界", 100)
	m.draft.BodyText = strings.Repeat("A long line\n", 50)
	for _, size := range [][2]int{{80, 24}, {35, 12}, {12, 5}, {1, 1}} {
		for step := stepAccount; step <= stepResult; step++ {
			m.setStep(step)
			m, _ = apply(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := m.View()
			if strings.Contains(view, "\x1b[") {
				t.Fatal("NO_COLOR ignored")
			}
			lines := strings.Split(view, "\n")
			if len(lines) > size[1] {
				t.Fatalf("step %d too tall", step)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("step %d too wide: %q", step, line)
				}
			}
		}
	}
}

func TestReviewScrollMovesImmediatelyUpFromBottom(t *testing.T) {
	m := testCompose(t, operations{})
	m.draft.BodyText = strings.Repeat("line\n", 50)
	m.setStep(stepReview)
	for range 20 {
		m, _ = press(m, tea.KeyPgDown)
	}
	before := m.scroll.Offset()
	m, _ = press(m, tea.KeyPgUp)
	if m.scroll.Offset() >= before {
		t.Fatal("review scroll was not clamped at bottom")
	}
}
func TestInteractiveDryRunBuildsWithoutPersistence(t *testing.T) {
	m := testCompose(t, operations{})
	m.opts.DryRun = true
	m.setStep(stepReview)
	m, cmd := press(m, tea.KeyEnter)
	if cmd == nil || m.outcome.State != "validated" || m.outcome.MIMEBytes == 0 {
		t.Fatal(m.outcome)
	}
}

func TestQQuitsMenusButRemainsText(t *testing.T) {
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
	menu := promptModel{mode: "choice", choices: []Choice{{ID: "send", Label: "Send"}}}
	next, cmd := menu.Update(key)
	if cmd == nil || !next.(promptModel).canceled {
		t.Fatal("q did not quit menu")
	}
	m := testCompose(t, operations{})
	m.setStep(stepReview)
	m, _ = apply(m, key)
	if m.step != stepCancel {
		t.Fatal("q did not protect unfinished draft")
	}
	m.setStep(stepTo)
	before := m.input.Value()
	m, _ = apply(m, key)
	if m.step != stepTo || m.input.Value() != before+"q" {
		t.Fatal("q was not typed", m.input.Value())
	}
	m.setStep(stepBody)
	before = m.editor.Value()
	m, _ = apply(m, key)
	if m.step != stepBody || m.editor.Value() != before+"q" {
		t.Fatal("q was not typed into body")
	}
}

func TestSkipReviewGoesStraightToSaveOrCountdown(t *testing.T) {
	saves, sends := 0, 0
	ops := operations{save: func(config.AccountConfig, db.Draft) (int64, error) {
		saves++
		return 7, nil
	}, enqueue: func(config.AccountConfig, smtp.OutgoingMessage, db.Draft, time.Time) (int64, error) {
		sends++
		return 8, nil
	}}
	attachmentsDone := func(mutate func(*composeModel)) (composeModel, tea.Cmd) {
		m := testCompose(t, ops)
		m.opts.SkipReview = true
		mutate(&m)
		m.setStep(stepAttachments)
		return press(m, tea.KeyEnter)
	}

	m, cmd := attachmentsDone(func(m *composeModel) { m.opts.DraftOnly = true })
	if cmd == nil || m.step != stepLoading {
		t.Fatal("draft should save without review", m.step)
	}
	m, _ = apply(m, cmd())
	if saves != 1 || sends != 0 || m.outcome.State != "drafted" {
		t.Fatal(m.outcome, saves, sends)
	}

	m, cmd = attachmentsDone(func(*composeModel) {})
	if cmd == nil {
		t.Fatal("send should be dispatched")
	}
	m, _ = apply(m, cmd())
	if sends != 1 || m.step != stepCountdown {
		t.Fatal("expected the undo countdown", m.step, sends)
	}

	for name, mutate := range map[string]func(*composeModel){
		"no delay": func(m *composeModel) { m.cfg.Display.SendDelaySeconds = 0 },
		"send now": func(m *composeModel) { m.opts.SendNow = true },
		"dry run":  func(m *composeModel) { m.opts.DryRun = true },
		"agent":    func(m *composeModel) { m.opts.Agent = true },
	} {
		m, _ := attachmentsDone(mutate)
		if m.step != stepReview {
			t.Fatalf("%s must still show the review, got step %v", name, m.step)
		}
	}

	m, _ = attachmentsDone(func(m *composeModel) { m.draft.Subject = "" })
	if m.step != stepReview || m.err == "" {
		t.Fatal("an invalid message must fall back to the review with an error", m.step, m.err)
	}
}
