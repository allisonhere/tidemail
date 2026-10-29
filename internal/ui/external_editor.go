package ui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

const externalEditorBodyLimit = 16 << 20

// A fresh pointer identifies one handoff, including across draft/compose resets.
type externalEditorSession struct{ original string }
type externalEditorFinishedMsg struct {
	session *externalEditorSession
	body    string
	err     error
}

// splitEditorCommand parses argv, not shell language. No expansion or shell
// invocation occurs; operators outside quotes are rejected rather than guessed.
func splitEditorCommand(command string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	flush := func() {
		if started {
			args = append(args, word.String())
			word.Reset()
			started = false
		}
	}
	for _, r := range command {
		if r < ' ' && r != '\t' || r == 0x7f {
			return nil, fmt.Errorf("editor command contains a control character")
		}
		if escaped {
			word.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote = r
			started = true
		case unicode.IsSpace(r):
			flush()
		case strings.ContainsRune(";|&<>`$", r):
			return nil, fmt.Errorf("editor command must be an executable and arguments, not shell syntax")
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("editor command has an unfinished quote or escape")
	}
	flush()
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("set VISUAL or EDITOR to a terminal editor (for example micro)")
	}
	return args, nil
}

func preferredEditor() ([]string, error) {
	command := strings.TrimSpace(os.Getenv("VISUAL"))
	if command == "" {
		command = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	args, err := splitEditorCommand(command)
	if err != nil {
		return nil, err
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return nil, fmt.Errorf("editor %q not found; check VISUAL or EDITOR", args[0])
	}
	args[0] = path
	return args, nil
}

// Add only documented cursor options for known editors. Arbitrary wrappers
// retain their configured arguments and are responsible for cursor placement.
func editorFileArgs(editor string, args []string, file string) []string {
	args = append([]string(nil), args...)
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(editor)), ".exe")
	if name == "micro" {
		return append(args, file, "+1:1")
	}
	position := ""
	switch name {
	case "vim", "vi", "nvim", "nano":
		position = "+1"
	}
	if position != "" {
		// A configured -- terminates option parsing: put the cursor option before it.
		at := len(args)
		for i, arg := range args {
			if arg == "--" {
				at = i
				break
			}
		}
		args = append(args, "")
		copy(args[at+1:], args[at:])
		args[at] = position
	}
	return append(args, file)
}

// prepareExternalEditor owns a private directory until its completion callback.
// Tests can supply ordinary file streams; production supplies actual tty files.
func prepareExternalEditor(session *externalEditorSession, input, output *os.File) (*exec.Cmd, tea.ExecCallback, error) {
	args, err := preferredEditor()
	if err != nil {
		return nil, nil, err
	}
	if len(session.original) > externalEditorBodyLimit {
		return nil, nil, fmt.Errorf("external editor bodies are limited to 16 MiB")
	}
	dir, err := os.MkdirTemp("", "tidemail-editor-")
	if err != nil {
		return nil, nil, fmt.Errorf("create editor directory: %w", err)
	}
	file := filepath.Join(dir, "message.md")
	if err = os.WriteFile(file, []byte(session.original), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("write editor body: %w", err)
	}
	cmd := exec.Command(args[0], editorFileArgs(args[0], args[1:], file)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, output, output
	finish := func(runErr error) tea.Msg {
		result := externalEditorFinishedMsg{session: session, err: runErr}
		if runErr == nil {
			result.body, result.err = readExternalEditorBody(file)
		}
		if err := os.RemoveAll(dir); err != nil && result.err == nil {
			result.err = fmt.Errorf("clean up editor file: %w", err)
		}
		return result
	}
	return cmd, finish, nil
}

func readExternalEditorBody(file string) (string, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return "", fmt.Errorf("read editor body: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("editor body must be a regular file")
	}
	f, err := os.Open(file)
	if err != nil {
		return "", fmt.Errorf("read editor body: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, externalEditorBodyLimit+1))
	if err != nil {
		return "", fmt.Errorf("read editor body: %w", err)
	}
	if len(data) > externalEditorBodyLimit {
		return "", fmt.Errorf("edited body exceeds 16 MiB; original body kept")
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("editor body must be UTF-8; original body kept")
	}
	body := string(data)
	for _, r := range body {
		if r < ' ' && r != '\n' && r != '\r' && r != '\t' || r == 0x7f {
			return "", fmt.Errorf("editor body contains a control character; original body kept")
		}
	}
	// These are the native Ripple text-entry rules, without the paste path's
	// combining-mark removal. Normalize before comparing to avoid no-op undo edits.
	return strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", "    ").Replace(body), nil
}

func externalEditorCmd(session *externalEditorSession) tea.Cmd {
	return func() tea.Msg {
		input, output := os.Stdin, os.Stdout
		var tty *os.File
		if !term.IsTerminal(input.Fd()) || !term.IsTerminal(output.Fd()) {
			var err error
			tty, err = os.OpenFile("/dev/tty", os.O_RDWR, 0)
			if err != nil {
				return externalEditorFinishedMsg{session: session, err: fmt.Errorf("external editor requires an interactive terminal: %w", err)}
			}
			input, output = tty, tty
		}
		cmd, finish, err := prepareExternalEditor(session, input, output)
		if err != nil {
			if tty != nil {
				tty.Close()
			}
			return externalEditorFinishedMsg{session: session, err: err}
		}
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			if tty != nil {
				defer tty.Close()
			}
			return finish(err)
		})()
	}
}

func (m Model) handleExternalEditorFinished(msg externalEditorFinishedMsg) (tea.Model, tea.Cmd) {
	// Bubble Tea releases mouse mode but does not restore it in this version.
	mouse := tea.DisableMouse
	if m.cfg.Display.MouseSupport {
		mouse = tea.EnableMouseCellMotion
	}
	restore := tea.Batch(mouse, setTermColorsCmd(m.styles.Theme.Fg, m.styles.Theme.Bg))
	if m.compose.externalEdit != msg.session || msg.session == nil {
		return m, restore
	}
	m.compose.externalEdit = nil
	m.compose.busy = false
	if msg.err != nil {
		m.compose.statusMsg, m.compose.isErr = "External editor: "+msg.err.Error()+" (original body kept)", true
		return m, restore
	}
	if m.compose.bodyInput.Value() != msg.session.original {
		m.compose.statusMsg, m.compose.isErr = "External edit ignored: compose body changed", true
		return m, restore
	}
	if msg.body == msg.session.original {
		m.compose.statusMsg, m.compose.isErr = "external editor: no changes", false
		return m, restore
	}
	m.compose.bodyInput.replaceExternalBody(msg.body)
	m.compose.focusedField = composeFieldBody
	m.compose.dirty = true
	m.compose.statusMsg, m.compose.isErr = "external editor: body updated", false
	if m.compose.hasContent() || m.compose.draftID != 0 {
		return m, tea.Batch(restore, m.saveDraftCmd(m.compose))
	}
	return m, restore
}
