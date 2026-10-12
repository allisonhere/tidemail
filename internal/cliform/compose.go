package cliform

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/allisonhere/ripple"
	"github.com/allisonhere/tidemail/internal/clipboard"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/mailapp"
	"github.com/allisonhere/tidemail/internal/smtp"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type ComposeOptions struct{ DraftOnly, DryRun, SendNow, Signature, Agent bool }
type Outcome struct {
	MIMEBytes, Attachments int
	State                  string
	ID                     int64
	AccountID              string
	NextAttempt            int64
	Err                    error
	Warning                bool
	Canceled               bool
}

type operations struct {
	save    func(config.AccountConfig, db.Draft) (int64, error)
	enqueue func(config.AccountConfig, smtp.OutgoingMessage, db.Draft, time.Time) (int64, error)
	cancel  func(int64) (db.Draft, error)
	deliver func(int64, config.AccountConfig) mailapp.Result
	status  func(int64) (db.OutboxItem, error)
}

func serviceOperations(database *db.DB, cfg config.Config, sessions *imapClient.SessionPool) operations {
	return operations{
		save: func(a config.AccountConfig, d db.Draft) (int64, error) { return mailapp.SaveDraft(database, a, d) },
		enqueue: func(a config.AccountConfig, m smtp.OutgoingMessage, d db.Draft, due time.Time) (int64, error) {
			return mailapp.Enqueue(database, cfg, a, m, d, due)
		},
		cancel: database.EditOutbox,
		deliver: func(id int64, a config.AccountConfig) mailapp.Result {
			return mailapp.Deliver(context.Background(), database, sessions, id, a, false, nil)
		},
		status: database.GetOutbox,
	}
}

type composeStep int

const (
	stepAccount composeStep = iota
	stepTo
	stepCC
	stepBCC
	stepSubject
	stepBody
	stepSignature
	stepAttachments
	stepReview
	stepCancel
	stepLoading
	stepCountdown
	stepSending
	stepResult
)

var stepNames = []string{"Sender account", "To", "CC (optional)", "BCC (optional)", "Subject", "Message body", "Signature", "Attachments", "Review", "Leave this draft?", "Working", "Queued", "Sending", "Delivery status"}

type composeModel struct {
	cfg                                     config.Config
	draft                                   db.Draft
	account                                 config.AccountConfig
	opts                                    ComposeOptions
	ops                                     operations
	step, previous                          composeStep
	cursor, attachmentCursor, width, height int
	input                                   textinput.Model
	editor                                  ripple.Model
	picker                                  filepicker.Model
	picking                                 bool
	err                                     string
	reviewText                              string
	scroll                                  tideui.PaneScroller
	a                                       appearance
	due                                     time.Time
	outcome                                 Outcome
	dirty                                   bool
}
type attachmentLoaded struct {
	attachment db.DraftAttachment
	err        error
}
type savedDraft struct {
	id  int64
	err error
}
type enqueued struct {
	id  int64
	due time.Time
	err error
}
type undone struct {
	draft db.Draft
	err   error
}
type delivered struct {
	result mailapp.Result
	item   db.OutboxItem
	err    error
}
type countdownTick time.Time

func newCompose(cfg config.Config, a config.AccountConfig, d db.Draft, opts ComposeOptions, ops operations, appearance appearance) composeModel {
	input := textinput.New()
	input.Width = 70
	editor := ripple.New()
	editor.SetClipboard(composeClipboard{})
	editor.SetSize(78, 17)
	editor.SetValue(d.BodyText)
	editor.SetPlaceholder("Write your email. Enter adds a new line.")
	if cfg.Display.ComposeVim {
		editor.SetInputMode(ripple.ModeVim)
	}
	picker := filepicker.New()
	picker.CurrentDirectory = "."
	if cwd, e := os.Getwd(); e == nil {
		picker.CurrentDirectory = cwd
	}
	picker.ShowSize = true
	picker.ShowPermissions = false
	picker.Styles.Selected = appearance.accent
	picker.Styles.Directory = appearance.title
	picker.Styles.File = appearance.muted
	picker.Styles.FileSize = appearance.muted
	picker.Styles.Symlink = appearance.accent
	m := composeModel{cfg: cfg, account: a, draft: d, opts: opts, ops: ops, input: input, editor: editor, picker: picker, width: 80, height: 24, a: appearance, dirty: d.To != "" || d.CC != "" || d.BCC != "" || d.Subject != "" || d.BodyText != "" || len(d.Attachments) > 0}
	for i, account := range cfg.Accounts {
		if account.ID == a.ID {
			m.cursor = i
		}
	}
	return m
}
func RunCompose(tty *os.File, database *db.DB, cfg config.Config, a config.AccountConfig, d db.Draft, opts ComposeOptions) (Outcome, error) {
	sessions := imapClient.NewSessionPool()
	defer sessions.Close()
	var ops operations
	if database != nil {
		ops = serviceOperations(database, cfg, sessions)
	}
	model := newCompose(cfg, a, d, opts, ops, styles(tty))
	model.editor.SetClipboard(composeClipboard{terminal: tty})
	final, err := run(model, tty)
	if err != nil {
		return Outcome{}, err
	}
	m := final.(composeModel)
	if m.outcome.State == "" {
		return Outcome{}, fmt.Errorf("editor interrupted before completion")
	}
	if m.step == stepSending || m.step == stepLoading {
		return Outcome{State: "sending", ID: m.outcome.ID, AccountID: m.account.ID, Err: fmt.Errorf("editor interrupted; inspect outbox status before retrying")}, nil
	}
	return m.outcome, nil
}
func (m composeModel) Init() tea.Cmd { return nil }
func (m *composeModel) setStep(step composeStep) {
	m.step = step
	m.cursor = 0
	m.err = ""
	m.scroll.ScrollToTop()
	m.input.Blur()
	m.editor.Blur()
	switch step {
	case stepAccount:
		for i, a := range m.cfg.Accounts {
			if a.ID == m.account.ID {
				m.cursor = i
			}
		}
	case stepTo:
		m.input.SetValue(m.draft.To)
		m.input.Focus()
	case stepCC:
		m.input.SetValue(m.draft.CC)
		m.input.Focus()
	case stepBCC:
		m.input.SetValue(m.draft.BCC)
		m.input.Focus()
	case stepSubject:
		m.input.SetValue(m.draft.Subject)
		m.input.Focus()
	case stepBody:
		m.editor.Focus()
		m.editor.StartInsert()
	case stepReview:
		m.reviewText = m.buildReview()
	case stepSignature:
		if !m.opts.Signature {
			m.cursor = 1
		}
	}
}
func (m *composeModel) storeField() {
	before := m.draft
	switch m.step {
	case stepTo:
		m.draft.To = m.input.Value()
	case stepCC:
		m.draft.CC = m.input.Value()
	case stepBCC:
		m.draft.BCC = m.input.Value()
	case stepSubject:
		m.draft.Subject = m.input.Value()
	case stepBody:
		m.draft.BodyText = m.editor.Value()
	}
	if before.To != m.draft.To || before.CC != m.draft.CC || before.BCC != m.draft.BCC || before.Subject != m.draft.Subject || before.BodyText != m.draft.BodyText {
		m.dirty = true
	}
}
func (m *composeModel) advance() tea.Cmd {
	m.storeField()
	if m.step == stepTo || m.step == stepCC || m.step == stepBCC {
		if e := mailapp.ValidateRecipients(m.input.Value(), m.step == stepTo && !m.opts.DraftOnly); e != nil {
			m.err = e.Error()
			return nil
		}
	}
	if m.step == stepAccount {
		if len(m.cfg.Accounts) == 0 {
			m.err = "Add an account in the inbox first."
			return nil
		}
		m.account = m.cfg.Accounts[m.cursor]
	}
	if m.step == stepSignature {
		m.opts.Signature = m.cursor == 0
	}
	m.setStep(m.step + 1)
	if m.step >= stepTo && m.step <= stepSubject {
		return textinput.Blink
	}
	return nil
}
func (m *composeModel) leave() {
	m.storeField()
	m.previous = m.step
	if !m.dirty {
		m.outcome = Outcome{Canceled: true, State: "canceled"}
		m.step = stepResult
		return
	}
	m.setStep(stepCancel)
}
func (m composeModel) choices() []string {
	switch m.step {
	case stepSignature:
		return []string{"Include configured signature", "No signature"}
	case stepReview:
		if m.opts.DryRun {
			return []string{"Validate only", "Edit", "Cancel"}
		}
		if m.opts.DraftOnly {
			return []string{"Save draft", "Edit", "Cancel"}
		}
		return []string{"Save draft", "Send email", "Edit", "Cancel"}
	case stepCancel:
		return []string{"Resume editing", "Save draft and leave", "Discard and leave"}
	}
	return nil
}
func (m *composeModel) save() tea.Cmd {
	if m.opts.Agent && !mailapp.Allowed(m.cfg, "drafts.create") {
		m.err = "Agent capability denied: drafts.create"
		return nil
	}
	if m.opts.DryRun {
		m.err = "Dry run cannot save a draft."
		return nil
	}
	m.step = stepLoading
	a, d, save := m.account, m.draft, m.ops.save
	return func() tea.Msg { id, err := save(a, d); return savedDraft{id, err} }
}
func (m composeModel) send() (composeModel, tea.Cmd) {
	if m.opts.Agent && !mailapp.Allowed(m.cfg, "mail.send") {
		m.err = "Agent capability denied: mail.send"
		return m, nil
	}
	msg, e := mailapp.Prepare(m.account, m.draft, m.opts.Signature)
	if e != nil {
		m.err = e.Error()
		return m, nil
	}
	if strings.TrimSpace(msg.Subject) == "" || (strings.TrimSpace(m.draft.BodyText) == "" && len(msg.Attachments) == 0) {
		m.err = "Sending requires a subject and body or attachment."
		return m, nil
	}
	a, d, enqueue := m.account, m.draft, m.ops.enqueue
	due := time.Now().Add(time.Duration(m.cfg.Display.SendDelaySeconds) * time.Second)
	if m.opts.SendNow {
		due = time.Now()
	}
	m.step = stepLoading
	return m, func() tea.Msg { id, err := enqueue(a, msg, d, due); return enqueued{id, due, err} }
}
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return countdownTick(t) })
}
func (m composeModel) deliver() tea.Cmd {
	id, a, ops := m.outcome.ID, m.account, m.ops
	return func() tea.Msg { r := ops.deliver(id, a); item, e := ops.status(id); return delivered{r, item, e} }
}
func (m *composeModel) undo() tea.Cmd {
	m.step = stepLoading
	id, cancel := m.outcome.ID, m.ops.cancel
	return func() tea.Msg { d, e := cancel(id); return undone{d, e} }
}

func (m composeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.input.Width = max(1, v.Width-4)
		m.editor.SetSize(max(1, v.Width-2), max(1, v.Height-7))
		m.picker.SetHeight(max(1, v.Height-8))
	case attachmentLoaded:
		m.setStep(stepAttachments)
		if v.err != nil {
			m.err = v.err.Error()
		} else {
			m.draft.Attachments = append(m.draft.Attachments, v.attachment)
			m.dirty = true
		}
		return m, nil
	case savedDraft:
		if v.err != nil {
			m.setStep(stepReview)
			m.err = "Could not save the draft."
			return m, nil
		}
		m.outcome = Outcome{State: "drafted", ID: v.id, AccountID: m.account.ID}
		m.step = stepResult
		return m, tea.Quit
	case enqueued:
		if v.err != nil {
			m.setStep(stepReview)
			m.err = "Could not persist the send. Your message is still here."
			return m, nil
		}
		m.outcome = Outcome{ID: v.id, State: db.OutboxQueued, AccountID: m.account.ID}
		m.due = v.due
		m.step = stepCountdown
		if !time.Now().Before(m.due) {
			m.step = stepSending
			return m, m.deliver()
		}
		return m, tick()
	case countdownTick:
		if m.step != stepCountdown {
			return m, nil
		}
		if !time.Time(v).Before(m.due) {
			m.step = stepSending
			return m, m.deliver()
		}
		return m, tick()
	case undone:
		if v.err != nil {
			m.step = stepSending
			m.err = "Delivery has started; undo is no longer available."
			return m, m.deliver()
		}
		m.draft = v.draft
		m.editor.SetValue(v.draft.BodyText)
		m.outcome = Outcome{}
		m.setStep(stepReview)
		m.err = "Send undone. The message is saved as a draft."
		return m, nil
	case delivered:
		m.step = stepResult
		m.outcome.Warning = v.result.CleanupErr != nil || v.result.SentCopyErr != nil
		if v.err != nil {
			m.outcome.Err = fmt.Errorf("cannot read delivery status; inspect outbox %d", m.outcome.ID)
			m.outcome.State = "sending"
		} else {
			m.outcome.State = v.item.State
			m.outcome.NextAttempt = v.item.NextAttempt
			if v.item.State == db.OutboxFailed || v.item.State == db.OutboxUncertain {
				m.outcome.Err = fmt.Errorf("delivery is %s; inspect outbox status before retrying", v.item.State)
			}
		}
		return m, nil
	case ripple.SubmitMsg:
		if m.step == stepBody {
			cmd := m.advance()
			return m, cmd
		}
	case ripple.CancelMsg:
		if m.step == stepBody {
			m.leave()
			return m, nil
		}
	case tea.KeyMsg:
		key := v.String()
		quitKey := key == "q" && (m.picking || m.step == stepAccount || m.step >= stepSignature)
		if m.step == stepSending || m.step == stepLoading {
			if key == "ctrl+c" || key == "esc" || quitKey {
				m.err = "Please wait. Once delivery starts, cancellation cannot guarantee stopping it."
			}
			return m, nil
		}
		if m.step == stepResult {
			return m, tea.Quit
		}
		if m.step == stepCountdown {
			if key == "ctrl+z" || key == "ctrl+c" || key == "esc" || key == "u" || quitKey {
				cmd := m.undo()
				return m, cmd
			}
			return m, nil
		}
		if m.picking {
			if key == "esc" || key == "ctrl+c" || quitKey {
				m.picking = false
				return m, nil
			}
			if key == "." {
				m.picker.ShowHidden = !m.picker.ShowHidden
				return m, m.picker.Init()
			}
		} else {
			if key == "esc" && m.step == stepBody && m.cfg.Display.ComposeVim {
				var cmd tea.Cmd
				m.editor, cmd = m.editor.Update(msg)
				return m, cmd
			}
			if key == "ctrl+c" || key == "esc" || quitKey {
				if m.step == stepCancel {
					m.setStep(m.previous)
				} else {
					m.leave()
					if m.outcome.Canceled {
						return m, tea.Quit
					}
				}
				return m, nil
			}
			if key == "shift+tab" && m.step > stepAccount && m.step <= stepReview {
				m.storeField()
				m.setStep(m.step - 1)
				return m, textinput.Blink
			}
			switch m.step {
			case stepAccount, stepSignature, stepReview, stepCancel:
				count := len(m.choices())
				if m.step == stepAccount {
					count = len(m.cfg.Accounts)
				}
				switch key {
				case "up", "k":
					m.cursor = max(0, m.cursor-1)
				case "down", "j":
					m.cursor = min(max(0, count-1), m.cursor+1)
				case "pgup":
					m.scroll.ScrollUp(max(1, m.height-10))
				case "pgdown":
					m.scroll.ScrollDown(max(1, m.height-10))
				case "enter":
					if m.step == stepAccount || m.step == stepSignature {
						cmd := m.advance()
						return m, cmd
					}
					if m.step == stepCancel {
						switch m.cursor {
						case 0:
							m.setStep(m.previous)
						case 1:
							cmd := m.save()
							return m, cmd
						case 2:
							m.outcome = Outcome{State: "canceled", Canceled: true}
							return m, tea.Quit
						}
						return m, nil
					}
					if m.opts.DryRun {
						switch m.cursor {
						case 0:
							message, e := mailapp.Prepare(m.account, m.draft, m.opts.Signature)
							if e != nil {
								m.err = e.Error()
								return m, nil
							}
							message.EnsureIdentity(message.From)
							m.outcome = Outcome{State: "validated", AccountID: m.account.ID, MIMEBytes: len(smtp.BuildRaw(m.account, message)), Attachments: len(message.Attachments)}
							return m, tea.Quit
						case 1:
							m.setStep(stepAccount)
						case 2:
							m.leave()
						}
						return m, nil
					}
					if m.cursor == 0 {
						cmd := m.save()
						return m, cmd
					}
					if !m.opts.DraftOnly && m.cursor == 1 {
						return m.send()
					}
					editIndex := 1
					if !m.opts.DraftOnly {
						editIndex = 2
					}
					if m.cursor == editIndex {
						m.setStep(stepAccount)
					} else {
						m.leave()
					}
					return m, nil
				}
			case stepAttachments:
				switch key {
				case "a":
					m.picking = true
					return m, m.picker.Init()
				case "up", "k":
					m.attachmentCursor = max(0, m.attachmentCursor-1)
				case "down", "j":
					m.attachmentCursor = min(max(0, len(m.draft.Attachments)-1), m.attachmentCursor+1)
				case "d", "delete", "backspace":
					if len(m.draft.Attachments) > 0 {
						m.draft.Attachments = append(m.draft.Attachments[:m.attachmentCursor], m.draft.Attachments[m.attachmentCursor+1:]...)
						m.attachmentCursor = min(m.attachmentCursor, max(0, len(m.draft.Attachments)-1))
						m.dirty = true
					}
				case "enter", "ctrl+s":
					cmd := m.advance()
					return m, cmd
				}
			case stepBody:
				if key == "ctrl+s" || key == "ctrl+enter" {
					cmd := m.advance()
					return m, cmd
				}
			default:
				if key == "enter" {
					cmd := m.advance()
					return m, cmd
				}
			}
		}
	}
	if m.step == stepReview {
		m.scroll.ClampTo(len(strings.Split(ansi.Wrap(m.reviewText, max(1, m.width), ""), "\n")), max(1, m.height-11))
	}
	if m.picking {
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		if selected, path := m.picker.DidSelectFile(msg); selected {
			for _, a := range m.draft.Attachments {
				if sameFile(a.Path, path) {
					m.err = "That file is already attached."
					m.picking = false
					return m, nil
				}
			}
			m.picking = false
			m.step = stepLoading
			return m, func() tea.Msg { a, e := LoadAttachment(path); return attachmentLoaded{a, e} }
		}
		return m, cmd
	}
	if m.step == stepBody {
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd
	}
	if m.step >= stepTo && m.step <= stepSubject {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func sameFile(a, b string) bool {
	aa, e := filepath.EvalSymlinks(a)
	if e != nil {
		aa = a
	}
	bb, e := filepath.EvalSymlinks(b)
	if e != nil {
		bb = b
	}
	aa, _ = filepath.Abs(aa)
	bb, _ = filepath.Abs(bb)
	return aa == bb
}
func LoadAttachment(path string) (db.DraftAttachment, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return db.DraftAttachment{}, fmt.Errorf("choose a readable regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return db.DraftAttachment{}, fmt.Errorf("cannot open attachment")
	}
	defer file.Close() //nolint:errcheck
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return db.DraftAttachment{}, fmt.Errorf("choose a readable regular file")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return db.DraftAttachment{}, fmt.Errorf("cannot read attachment")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return db.DraftAttachment{}, err
	}
	return db.DraftAttachment{Filename: filepath.Base(path), Path: absolute, Data: data, Size: int64(len(data))}, nil
}

func (m composeModel) View() string {
	title := stepNames[m.step]
	if m.step <= stepReview {
		title = fmt.Sprintf("%d/9  %s", m.step+1, title)
	}
	content := ""
	hint := "enter next  shift+tab back  esc leave"
	if m.picking {
		title = "Pick an attachment"
		content = m.a.muted.Render(oneLine(m.picker.CurrentDirectory)) + "\n" + m.picker.View()
		hint = "↑/↓ move  →/enter open or select  ← parent  . hidden  esc back"
	} else {
		switch m.step {
		case stepAccount:
			visible := max(1, m.height-6)
			start := max(0, m.cursor-visible+1)
			for i := start; i < min(len(m.cfg.Accounts), start+visible); i++ {
				a := m.cfg.Accounts[i]
				line := "  " + oneLine(a.Name) + "  " + oneLine(smtp.AccountSender(a))
				if i == m.cursor {
					line = m.a.accent.Render("> " + oneLine(a.Name) + "  " + oneLine(smtp.AccountSender(a)))
				}
				content += line + "\n"
			}
			if len(m.cfg.Accounts) == 0 {
				content = "No configured accounts. Add one in the inbox."
			}
			hint = "↑/↓ account  enter next  q quit"
		case stepTo, stepCC, stepBCC, stepSubject:
			content = m.input.View()
			if m.step == stepCC || m.step == stepBCC {
				hint = "enter skip or continue  shift+tab back  esc leave"
			}
		case stepBody:
			content = m.editor.View(ripple.Options{Cursor: m.a.accent.Reverse(true).Render(" "), Selected: func(s string) string { return m.a.accent.Reverse(true).Render(s) }, Placeholder: func(s string) string { return m.a.muted.Render(s) }})
			hint = "enter newline  ctrl+s next  shift+tab back  esc leave"
		case stepSignature:
			content = "Configured signature:\n" + safe(m.account.Signature) + "\n\n"
			if m.account.Signature == "" {
				content = "No signature is configured for this account.\n\n"
			}
			content += m.choiceView()
		case stepAttachments:
			if len(m.draft.Attachments) == 0 {
				content = "No attachments. Press a to browse, or Enter to skip."
			}
			visible := max(1, m.height-8)
			start := max(0, m.attachmentCursor-visible+1)
			for i := start; i < min(len(m.draft.Attachments), start+visible); i++ {
				a := m.draft.Attachments[i]
				prefix := "  "
				if i == m.attachmentCursor {
					prefix = "> "
				}
				content += fmt.Sprintf("%s%s  (%d bytes)\n", prefix, oneLine(a.Filename), len(a.Data))
			}
			hint = "a add file  d remove  ↑/↓ select  enter next  q quit"
		case stepReview:
			lines := strings.Split(ansi.Wrap(m.reviewText, max(1, m.width), ""), "\n")
			visible := max(1, m.height-11)
			m.scroll.ClampTo(len(lines), visible)
			start := m.scroll.Offset()
			content = strings.Join(lines[start:min(len(lines), start+visible)], "\n") + "\n\n" + m.choiceView()
			hint = "↑/↓ action  pgup/pgdn review  enter choose  q quit"
		case stepCancel:
			content = "Your message has not been sent.\n\n" + m.choiceView()
			hint = "↑/↓ choose  enter confirm  esc resume"
		case stepLoading:
			content = "Working… Your message stays here until it is saved."
		case stepCountdown:
			content = fmt.Sprintf("Outbox %d is queued. Sending in %d seconds.\n\nPress u or Ctrl+Z to undo and return to the draft.", m.outcome.ID, max(0, int(time.Until(m.due).Seconds()+1)))
			hint = "u / ctrl+z undo"
		case stepSending:
			content = "Submitting mail…\n\nOnce delivery starts, cancellation cannot guarantee stopping it."
			hint = "Please wait for the recorded delivery status."
		case stepResult:
			content = fmt.Sprintf("Outbox %d: %s", m.outcome.ID, m.outcome.State)
			if m.outcome.State == db.OutboxQueued {
				content += "\nA retry is queued. Use the inbox or outbox process to deliver it."
			}
			if m.outcome.Err != nil {
				content += "\n" + m.outcome.Err.Error()
			}
			if m.outcome.Warning {
				content += "\nDelivery status was recorded, but cleanup or the Sent copy needs attention."
			}
			hint = "enter close"
		}
	}
	if m.width < 50 {
		switch {
		case m.picking:
			hint = "arrows browse  enter pick  esc back"
		case m.step == stepBody:
			hint = "ctrl+s next  shift+tab back"
		case m.step == stepAttachments:
			hint = "a add  d remove  enter next"
		case m.step == stepReview:
			hint = "↑/↓ choose  enter  pgup/pgdn scroll"
		case m.step <= stepSignature:
			hint = "enter next  shift+tab back  esc exit"
		}
	}
	if m.err != "" {
		content += "\n" + m.a.danger.Render(safe(m.err))
	}
	return frame(m.a, title, strings.TrimRight(content, "\n"), hint, m.width, m.height)
}
func (m composeModel) choiceView() string {
	var s strings.Builder
	for i, label := range m.choices() {
		line := "  " + label
		if i == m.cursor {
			line = m.a.accent.Render("> " + label)
		}
		s.WriteString(line + "\n")
	}
	return strings.TrimRight(s.String(), "\n")
}

type composeClipboard struct{ terminal io.Writer }

func (composeClipboard) Read() (string, error) { return clipboard.Read() }
func (c composeClipboard) Write(text string) error {
	if c.terminal == nil {
		return fmt.Errorf("clipboard terminal unavailable")
	}
	return clipboard.CopyTo(text, c.terminal)
}

func (m composeModel) buildReview() string {
	body := m.draft.BodyText
	msg, e := mailapp.Prepare(m.account, m.draft, m.opts.Signature)
	if e == nil {
		body = msg.Body
	} else if m.opts.Signature && m.account.Signature != "" {
		body += "\n\nSignature:\n" + m.account.Signature
	}
	review := fmt.Sprintf("Account: %s [%s]\nFrom: %s\nTo: %s\nCC: %s\nBCC: %s\nSubject: %s\n\n%s", oneLine(m.account.Name), oneLine(m.account.ID), oneLine(smtp.AccountSender(m.account)), oneLine(m.draft.To), oneLine(m.draft.CC), oneLine(m.draft.BCC), oneLine(m.draft.Subject), safe(body))
	for _, a := range m.draft.Attachments {
		review += fmt.Sprintf("\nAttachment: %s (%d bytes)", oneLine(a.Filename), len(a.Data))
	}
	review += "\n\n"
	return review
}
