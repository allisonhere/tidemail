package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/allisonhere/ripple"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

func TestExternalEditorArguments(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{"vim -f", []string{"vim", "-f"}}, {`"path with spaces/editor" --flag 'two words'`, []string{"path with spaces/editor", "--flag", "two words"}},
		{`micro escaped\ space ""`, []string{"micro", "escaped space", ""}},
		{`editor '$HOME;literal'`, []string{"editor", "$HOME;literal"}},
	} {
		got, err := splitEditorCommand(tc.command)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: %q %v", tc.command, got, err)
		}
	}
	for _, command := range []string{"", " ", `vim 'open`, `vim \`, "vim\n-f", "vim\x00", "vim | other", "vim; other", "$(editor)", "vim > file"} {
		if _, err := splitEditorCommand(command); err == nil {
			t.Fatalf("accepted %q", command)
		}
	}
	for _, tc := range []struct {
		name       string
		args, want []string
	}{
		{"micro", nil, []string{"/tmp/body.md", "+1:1"}}, {"vim", []string{"-f"}, []string{"-f", "+1", "/tmp/body.md"}},
		{"nvim", []string{"--"}, []string{"+1", "--", "/tmp/body.md"}}, {"nano", nil, []string{"+1", "/tmp/body.md"}},
		{"wrapper", []string{"--wait"}, []string{"--wait", "/tmp/body.md"}},
	} {
		if got := editorFileArgs(tc.name, tc.args, "/tmp/body.md"); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: %q", tc.name, got)
		}
	}
}

func TestExternalEditorSelection(t *testing.T) {
	t.Setenv("VISUAL", strconv.Quote(os.Args[0])+" -test.v")
	t.Setenv("EDITOR", "missing-editor")
	args, err := preferredEditor()
	if err != nil || args[0] != os.Args[0] || args[1] != "-test.v" {
		t.Fatalf("VISUAL precedence: %q %v", args, err)
	}
	t.Setenv("VISUAL", " \t")
	t.Setenv("EDITOR", strconv.Quote(os.Args[0]))
	if _, err := preferredEditor(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "tidemail-no-such-editor")
	if _, err := preferredEditor(); err == nil {
		t.Fatal("invalid VISUAL silently fell back")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if _, err := preferredEditor(); err == nil || !strings.Contains(err.Error(), "VISUAL or EDITOR") {
		t.Fatal("missing actionable configuration error")
	}
}

func TestExternalEditorHelper(t *testing.T) {
	if os.Getenv("TIDEMAIL_EDITOR_TEST_HELPER") != "1" {
		return
	}
	file := os.Args[len(os.Args)-1]
	mode := os.Getenv("TIDEMAIL_EDITOR_TEST_MODE")
	body, err := os.ReadFile(file)
	if err != nil {
		os.Exit(10)
	}
	switch mode {
	case "change":
		body = append([]byte("Edited 世界 e\u0301\n"), body...)
	case "empty":
		body = nil
	case "unchanged":
		os.Exit(0)
	case "failure":
		_ = os.WriteFile(file, []byte("saved but failed"), 0600)
		os.Exit(7)
	case "invalid":
		body = []byte{0xff}
	case "control":
		body = []byte("escape\x1b")
	case "crlf":
		body = []byte(strings.ReplaceAll(string(body), "\n", "\r\n"))
	case "missing":
		_ = os.Remove(file)
		os.Exit(0)
	case "directory":
		_ = os.Remove(file)
		_ = os.Mkdir(file, 0700)
		os.Exit(0)
	case "large":
		body = []byte(strings.Repeat("a", 512<<10) + "\n⁂\n")
	case "oversize":
		f, e := os.OpenFile(file, os.O_WRONLY, 0600)
		if e != nil {
			os.Exit(11)
		}
		_ = f.Truncate(externalEditorBodyLimit + 1)
		f.Close()
		os.Exit(0)
	case "pty":
		fmt.Fprintln(os.Stdout, "EDITOR_PTY_READY")
		body = []byte(fmt.Sprintf("%t,%t,%t", term.IsTerminal(os.Stdin.Fd()), term.IsTerminal(os.Stdout.Fd()), term.IsTerminal(os.Stderr.Fd())))
	}
	// Exercise editors that save by rename rather than modifying the old inode.
	if err := os.WriteFile(file+".new", body, 0600); err != nil {
		os.Exit(12)
	}
	if err := os.Rename(file+".new", file); err != nil {
		os.Exit(13)
	}
	os.Exit(0)
}

func configureExternalEditorHelper(t *testing.T, mode string) {
	t.Helper()
	t.Setenv("VISUAL", strconv.Quote(os.Args[0])+" '-test.run=^TestExternalEditorHelper$' --")
	t.Setenv("EDITOR", "")
	t.Setenv("TIDEMAIL_EDITOR_TEST_HELPER", "1")
	t.Setenv("TIDEMAIL_EDITOR_TEST_MODE", mode)
}

func runExternalEditorHelper(t *testing.T, original, mode string) externalEditorFinishedMsg {
	t.Helper()
	configureExternalEditorHelper(t, mode)
	stream, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	session := &externalEditorSession{original: original}
	cmd, finish, err := prepareExternalEditor(session, stream, stream)
	if err != nil {
		t.Fatal(err)
	}
	file := cmd.Args[len(cmd.Args)-1]
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file permissions: %v", err)
	}
	info, err = os.Stat(filepath.Dir(file))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("directory permissions: %v", err)
	}
	if cmd.Stdin != stream || cmd.Stdout != stream || cmd.Stderr != stream {
		t.Fatal("child does not have explicit file streams")
	}
	if mode == "launch-failure" {
		cmd.Path = filepath.Join(t.TempDir(), "missing")
	}
	result := finish(cmd.Run()).(externalEditorFinishedMsg)
	if _, err := os.Stat(filepath.Dir(file)); !os.IsNotExist(err) {
		t.Fatalf("temporary directory not cleaned up: %v", err)
	}
	return result
}

func TestExternalEditorProcessResultsAndCleanup(t *testing.T) {
	original := "# Markdown\n\n⁂\n\n> Quoted text\n"
	for _, mode := range []string{"change", "empty", "unchanged", "failure", "launch-failure", "invalid", "control", "crlf", "missing", "directory", "large", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			result := runExternalEditorHelper(t, original, mode)
			bad := mode == "failure" || mode == "launch-failure" || mode == "invalid" || mode == "control" || mode == "missing" || mode == "directory" || mode == "oversize"
			if (result.err != nil) != bad {
				t.Fatalf("%s: %v", mode, result.err)
			}
			if bad {
				return
			}
			switch mode {
			case "empty":
				if result.body != "" {
					t.Fatal("empty body not imported")
				}
			case "unchanged", "crlf":
				if result.body != original {
					t.Fatalf("unchanged body altered: %q", result.body)
				}
			default:
				if !strings.Contains(result.body, "⁂") {
					t.Fatal("marker lost")
				}
			}
		})
	}
	// Configuration/size failures create no temporary resources.
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	configureExternalEditorHelper(t, "unchanged")
	stream, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, _, err := prepareExternalEditor(&externalEditorSession{original: strings.Repeat("a", externalEditorBodyLimit+1)}, stream, stream); err == nil {
		t.Fatal("oversized input accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("prelaunch resources leaked: %v", err)
	}
}

func TestExternalEditorShortcutAndFooter(t *testing.T) {
	for _, field := range []composeField{composeFieldFrom, composeFieldTo, composeFieldCC, composeFieldBCC, composeFieldSubject, composeFieldBody} {
		c := NewCompose(config.AccountConfig{}, nil, nil)
		c.focusedField = field
		updated, cmd, exit := c.Update(tea.KeyMsg{Type: tea.KeyCtrlE}, DefaultKeys)
		if (updated.externalEdit != nil) != (field == composeFieldBody) || exit {
			t.Fatalf("wrong shortcut context %v", field)
		}
		if field == composeFieldBody && (cmd == nil || !updated.busy) {
			t.Fatal("body editor handoff not armed")
		}
		for _, width := range []int{16, 24, 32, 52, 74, 128} {
			hints := ansi.Strip(c.composeHints(width, newManagerChrome(width, CatppuccinMocha, false)))
			if strings.Contains(hints, "^e ext. editor") != (field == composeFieldBody) {
				t.Fatalf("wrong hint context %v", field)
			}
			if field == composeFieldBody && !strings.HasSuffix(strings.Join(strings.Fields(hints), " "), "esc cancel ⁂ sig ^e ext. editor") {
				t.Fatal(hints)
			}
			for _, line := range strings.Split(hints, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("footer overflow: %q", line)
				}
			}
		}
	}
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.ed.SetInputMode(ripple.ModeVim)
	c.bodyInput.ed, _ = c.bodyInput.ed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlE}, DefaultKeys)
	if c.externalEdit != nil {
		t.Fatal("launched from Vim command entry")
	}
	for _, panel := range []string{"picker", "sender", "template", "spell"} {
		c := NewCompose(config.AccountConfig{}, nil, nil)
		c.focusedField = composeFieldBody
		switch panel {
		case "picker":
			c.picker.active = true
		case "sender":
			c.senderPickerOpen = true
		case "template":
			c.tmplOpen = true
		case "spell":
			c.spellOpen = true
		}
		c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlE}, DefaultKeys)
		if c.externalEdit != nil {
			t.Fatalf("launched from %s panel", panel)
		}
	}
}

func TestExternalEditorPreservesComposeAndAutosaves(t *testing.T) {
	for _, forward := range []bool{false, true} {
		t.Run(fmt.Sprint(forward), func(t *testing.T) {
			m := newSendTestModel(t, 5)
			a := config.AccountConfig{ID: "work", Name: "Work", User: "sender@example.com"}
			accounts := []config.AccountConfig{{ID: "personal", User: "personal@example.com"}, a}
			source := db.Message{From: "other@example.com", To: a.User, Subject: "Topic", MessageID: "<original@example.com>", References: "<thread@example.com>", BodyText: "Original"}
			c := NewReply(source, a, accounts, nil)
			if forward {
				c = NewForward(source, a, accounts, nil)
			}
			c.toInput.SetValue("to@example.com")
			c.ccInput.SetValue("cc@example.com")
			c.bccInput.SetValue("bcc@example.com")
			c.attachments = []attachmentFile{{Name: "note.txt", Data: []byte("attachment")}}
			c.bodyInput.InsertText("Native edit\n\n⁂\n\n")
			original := c.bodyInput.Value()
			before := c.toDraftRecord()
			result := runExternalEditorHelper(t, original, "change")
			c.externalEdit = result.session
			c.busy = true
			m.compose = c
			m.overlay = overlayCompose
			// A draft ID arriving during handoff must not be replaced by an old snapshot.
			id, err := m.db.SaveDraft(before)
			if err != nil {
				t.Fatal(err)
			}
			m.compose.draftID = id
			before.ID = id
			next, cmd := m.Update(result)
			m = next.(Model)
			if m.overlay != overlayCompose || m.compose.busy || m.compose.focusedField != composeFieldBody || m.compose.bodyInput.ed.CursorIndex() != 0 {
				t.Fatal("compose not restored")
			}
			after := m.compose.toDraftRecord()
			after.BodyText = before.BodyText
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("other compose fields changed: %#v", after)
			}
			if !m.compose.bodyInput.ed.Undo() || m.compose.bodyInput.Value() != original || !m.compose.bodyInput.ed.Undo() || strings.Contains(m.compose.bodyInput.Value(), "Native edit") {
				t.Fatal("native history lost")
			}
			for i := 0; i < 2; i++ {
				if !m.compose.bodyInput.ed.Redo() {
					t.Fatal("redo failed")
				}
			}
			if m.compose.bodyInput.Value() != result.body {
				t.Fatal("redo did not restore the external edit")
			}
			commands := cmd().(tea.BatchMsg)
			saved := commands[len(commands)-1]().(DraftSavedMsg)
			if saved.Err != nil {
				t.Fatal(saved.Err)
			}
			record, err := m.db.GetDraft(saved.DraftID)
			if err != nil || record.BodyText != result.body {
				t.Fatalf("autosave missing: %v", err)
			}
			var queued int
			if err = m.db.QueryRow("SELECT count(*) FROM outbox").Scan(&queued); err != nil || queued != 0 {
				t.Fatalf("external editing queued mail: %d %v", queued, err)
			}
		})
	}
}

func TestExternalEditorFailureUnchangedStaleAndMouse(t *testing.T) {
	for _, mouse := range []bool{false, true} {
		for _, mode := range []string{"failure", "unchanged", "change", "stale", "conflict"} {
			t.Run(fmt.Sprintf("%v/%s", mouse, mode), func(t *testing.T) {
				m := newSendTestModel(t, 5)
				m.cfg.Display.MouseSupport = mouse
				m.overlay = overlayCompose
				m.compose = NewCompose(config.AccountConfig{}, nil, nil)
				m.compose.bodyInput.SetValue("Original\n\n⁂\n")
				helperMode := mode
				if mode == "stale" || mode == "conflict" {
					helperMode = "change"
				}
				result := runExternalEditorHelper(t, m.compose.bodyInput.Value(), helperMode)
				m.compose.externalEdit = result.session
				m.compose.busy = true
				if mode == "stale" {
					m.compose = NewCompose(config.AccountConfig{}, nil, nil)
					m.compose.bodyInput.SetValue("New composer")
				}
				if mode == "conflict" {
					m.compose.bodyInput.SetValue("New body")
				}
				before := m.compose.bodyInput.Value()
				next, cmd := m.handleExternalEditorFinished(result)
				m = next.(Model)
				if mode != "change" && m.compose.bodyInput.Value() != before {
					t.Fatal("failed/stale edit overwrote body")
				}
				if mode == "failure" && !m.compose.isErr {
					t.Fatal("failure hidden")
				}
				commands := cmd().(tea.BatchMsg)
				if mode == "change" {
					commands = commands[0]().(tea.BatchMsg)
				}
				want := tea.DisableMouse()
				if mouse {
					want = tea.EnableMouseCellMotion()
				}
				if reflect.TypeOf(commands[0]()) != reflect.TypeOf(want) {
					t.Fatal("mouse mode not restored")
				}
			})
		}
	}
}

func TestExternalEditorPreservesVimModes(t *testing.T) {
	for _, mode := range []string{"", "NORMAL", "INSERT", "VISUAL", "V-LINE"} {
		t.Run(mode, func(t *testing.T) {
			e := newEditorArea()
			e.SetValue("Original")
			if mode != "" {
				e.ed.SetInputMode(ripple.ModeVim)
			}
			switch mode {
			case "INSERT":
				e.ed.StartInsert()
			case "VISUAL":
				e.ed, _ = e.ed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
			case "V-LINE":
				e.ed, _ = e.ed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
			}
			e.replaceExternalBody("Updated\n\n⁂\n")
			if e.Mode() != mode || e.ed.CursorIndex() != 0 || !e.ed.Undo() || e.Value() != "Original" {
				t.Fatalf("mode/history lost: %s", e.Mode())
			}
		})
	}
}

func TestExternalEditorEmptyDraftAutosaveAndPendingKeys(t *testing.T) {
	m := newSendTestModel(t, 5)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.bodyInput.SetValue("Original body")
	id, err := m.db.SaveDraft(m.compose.toDraftRecord())
	if err != nil {
		t.Fatal(err)
	}
	m.compose.draftID = id
	result := runExternalEditorHelper(t, m.compose.bodyInput.Value(), "empty")
	m.compose.externalEdit, m.compose.busy = result.session, true
	for _, key := range []tea.KeyType{tea.KeyCtrlE, tea.KeyCtrlP, tea.KeyCtrlS, tea.KeyEsc} {
		next, cmd := m.Update(tea.KeyMsg{Type: key})
		m = next.(Model)
		if cmd != nil || m.overlay != overlayCompose || m.compose.externalEdit != result.session {
			t.Fatal("pending handoff allowed an action")
		}
	}
	next, cmd := m.Update(result)
	m = next.(Model)
	commands := cmd().(tea.BatchMsg)
	saved := commands[len(commands)-1]().(DraftSavedMsg)
	if saved.Err != nil {
		t.Fatal(saved.Err)
	}
	draft, err := m.db.GetDraft(id)
	if err != nil || draft.BodyText != "" {
		t.Fatalf("empty external edit was not saved: %v", err)
	}
}

type externalEditorPTYModel struct {
	model Model
	done  bool
	err   error
}

func (m *externalEditorPTYModel) Init() tea.Cmd {
	return externalEditorCmd(m.model.compose.externalEdit)
}
func (m *externalEditorPTYModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(msg)
	m.model = next.(Model)
	if result, ok := msg.(externalEditorFinishedMsg); ok {
		m.done = true
		m.err = result.err
		return m, tea.Sequence(cmd, tea.Quit)
	}
	return m, cmd
}
func (m *externalEditorPTYModel) View() string { return "\n" }

// Run the compiled test binary inside a disposable PTY, never a real account.
func TestExternalEditorPTY(t *testing.T) {
	if os.Getenv("TIDEMAIL_EXTERNAL_EDITOR_PTY") != "1" {
		t.Skip("isolated PTY acceptance probe")
	}
	for _, mouse := range []bool{false, true} {
		t.Run(fmt.Sprint(mouse), func(t *testing.T) {
			configureExternalEditorHelper(t, "pty")
			cfg := config.DefaultConfig()
			cfg.Display.MouseSupport = mouse
			model := NewModel(nil, cfg, "test", false)
			model.overlay = overlayCompose
			model.compose = NewCompose(config.AccountConfig{}, nil, nil)
			model.compose.bodyInput.SetValue("Body\n\n⁂\n\n> Quote")
			model.compose.externalEdit = &externalEditorSession{original: model.compose.bodyInput.Value()}
			model.compose.busy = true
			wrapper := &externalEditorPTYModel{model: model}
			opts := []tea.ProgramOption{tea.WithInput(os.Stdin), tea.WithOutput(SerializedStdout()), tea.WithAltScreen(), tea.WithoutSignalHandler()}
			if mouse {
				opts = append(opts, tea.WithMouseCellMotion())
			}
			_, err := tea.NewProgram(wrapper, opts...).Run()
			if err != nil || wrapper.err != nil || !wrapper.done {
				t.Fatalf("handoff/resume: %v %v", err, wrapper.err)
			}
			if wrapper.model.compose.bodyInput.Value() != "true,true,true" || wrapper.model.overlay != overlayCompose {
				t.Fatal("child did not have real terminal streams or compose was lost")
			}
			t.Logf("mouse=%v: real editor streams and framework handoff/resume passed", mouse)
		})
	}
}

func TestExternalEditorSignaturePlacementAtSend(t *testing.T) {
	for _, body := range []string{"Top\n\n⁂\n\nBottom", "Unsigned body"} {
		t.Run(body, func(t *testing.T) {
			m := newSendTestModel(t, 5)
			m.compose = NewCompose(config.AccountConfig{User: "me@example.com", Signature: "My signature"}, nil, nil)
			m.compose.toInput.SetValue("reader@example.com")
			result := runExternalEditorHelper(t, body, "change")
			m.compose.bodyInput.SetValue(body)
			m.compose.externalEdit, m.compose.busy = result.session, true
			next, _ := m.handleExternalEditorFinished(result)
			m = next.(Model)
			if m.compose.bodyInput.Value() != result.body {
				t.Fatal("edited marker position changed")
			}
			_, cmd, _ := m.compose.send()
			if cmd == nil {
				t.Fatal(m.compose.statusMsg)
			}
			prepared := cmd().(SendQueuedMsg).Msg.Body
			if strings.Contains(prepared, "⁂") {
				t.Fatal("marker leaked into prepared message")
			}
			if strings.Contains(body, "⁂") {
				if !strings.Contains(prepared, "My signature") || strings.Index(prepared, "My signature") > strings.Index(prepared, "Bottom") {
					t.Fatalf("signature misplaced: %q", prepared)
				}
			} else if strings.Contains(prepared, "My signature") {
				t.Fatal("deleted marker still signed")
			}
		})
	}
}
