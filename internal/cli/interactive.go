package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/allisonhere/tidemail/internal/cliform"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"golang.org/x/term"
)

const OpenInbox = -1

func terminalIO(in io.Reader, out io.Writer) bool {
	input, ok := in.(*os.File)
	if !ok {
		return false
	}
	output, ok := out.(*os.File)
	return ok && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
}
func composeInteractive(cfg config.Config, a config.AccountConfig, d db.Draft, opts cliform.ComposeOptions, out, errOut io.Writer, jsonMode bool) int {
	tty, e := cliform.OpenTerminal()
	if e != nil {
		return mailResult(out, errOut, jsonMode, nil, 2, "tty_required", e.Error())
	}
	defer tty.Close() //nolint:errcheck
	var database *db.DB
	if !opts.DryRun {
		database, e = db.Open()
		if e != nil {
			return mailResult(out, errOut, jsonMode, nil, 1, "database_error", "cannot open mail cache")
		}
		defer database.Close() //nolint:errcheck
	}
	result, e := cliform.RunCompose(tty, database, cfg, a, d, opts)
	if e != nil {
		return mailResult(out, errOut, jsonMode, nil, 1, "interactive_error", "interactive editor stopped; inspect outbox status if a send was queued")
	}
	data := map[string]any{"state": result.State, "id": result.ID, "account_id": result.AccountID, "next_attempt": result.NextAttempt}
	if opts.DryRun {
		data["mime_bytes"] = result.MIMEBytes
		data["attachments"] = result.Attachments
	}
	if result.Warning {
		_, _ = fmt.Fprintln(errOut, "warning: delivery status recorded; cleanup or Sent copy needs attention")
	}
	if result.Err != nil {
		if jsonMode {
			if e = json.NewEncoder(out).Encode(map[string]any{"schema_version": 1, "ok": false, "data": data, "error": map[string]string{"code": "delivery_" + result.State, "message": result.Err.Error()}}); e != nil {
				return 1
			}
			return 1
		}
		return mailResult(out, errOut, false, nil, 1, "delivery_"+result.State, result.Err.Error())
	}
	return mailResult(out, errOut, jsonMode, data, 0, "", "")
}

func interactionFlags(args []string) (clean []string, explicit, disabled bool) {
	for _, a := range args {
		switch a {
		case "--interactive", "--interactive=true":
			explicit = true
		case "--no-interactive", "--no-interactive=true":
			disabled = true
		case "--interactive=false", "--no-interactive=false":
		default:
			clean = append(clean, a)
		}
	}
	return
}
func interactiveRoute(args []string, in io.Reader, out, errOut io.Writer) (bool, int) {
	command := args[0]
	if command == "send" || command == "draft" && len(args) > 1 && args[1] == "create" {
		return false, 0
	}
	clean, explicit, disabled := interactionFlags(args)
	if hasHelpFlag(clean) {
		if command == "cli" {
			_, _ = fmt.Fprintln(out, "Usage: tidemail cli\nOpen the interactive command menu. Plain tidemail opens the inbox.\nOther mail commands accept --interactive and --no-interactive.")
			return true, 0
		}
		if explicit || disabled {
			return true, RunHeadless(clean, in, out, errOut)
		}
		return false, 0
	}
	if explicit && disabled {
		return true, mailResult(out, errOut, containsFlag(args, "--json"), nil, 2, "invalid_arguments", "interactive options conflict")
	}
	if disabled {
		// Dispatch stripped flags directly rather than prompting.
		switch command {
		case "accounts", "messages":
			return true, RunMailRead(command, clean[1:], out, errOut)
		case "outbox":
			if len(clean) > 1 && (clean[1] == "process" || clean[1] == "retry" || clean[1] == "cancel") {
				return true, runOutboxAction(clean[1:], out, errOut)
			}
			return true, RunMailRead(command, clean[1:], out, errOut)
		case "draft":
			return true, RunHeadless(clean, in, out, errOut)
		case "install-skill":
			return true, RunInstallSkill(clean[1:], out, errOut)
		case "skill":
			return true, RunSkill(clean[1:], out, errOut)
		}
	}
	automatic := terminalIO(in, out) && !containsFlag(clean, "--json") && !containsFlag(clean, "--agent") && !containsFlag(clean, "--yes") && !containsFlag(clean, "--dry-run")
	missing := len(clean) == 1 && (command == "draft" || command == "messages" || command == "accounts" || command == "outbox" || command == "install-skill")
	if len(clean) == 2 && command == "messages" && (clean[1] == "search" || clean[1] == "read") {
		missing = true
	}
	if command != "cli" && !explicit && (!automatic || !missing) {
		return false, 0
	}
	tty, e := cliform.OpenTerminal()
	if e != nil {
		return true, mailResult(out, errOut, containsFlag(clean, "--json"), nil, 2, "tty_required", e.Error())
	}
	defer tty.Close() //nolint:errcheck
	if command == "cli" {
		if len(clean) != 1 {
			return true, mailResult(out, errOut, false, nil, 2, "invalid_arguments", "tidemail cli takes no arguments")
		}
		return true, runCommandMenu(tty, in, out, errOut)
	}
	if command == "draft" {
		return true, RunHeadless(append([]string{"draft", "create", "--interactive"}, clean[1:]...), in, out, errOut)
	}
	if command == "install-skill" {
		return true, installSkillInteractive(tty, clean[1:], out, errOut)
	}
	if command == "messages" {
		return true, messagesInteractive(tty, clean[1:], out, errOut)
	}
	if command == "outbox" {
		return true, outboxInteractive(tty, clean[1:], out, errOut)
	}
	if command == "accounts" {
		readArgs := clean[1:]
		if len(readArgs) == 0 || strings.HasPrefix(readArgs[0], "-") {
			readArgs = append([]string{"list"}, readArgs...)
		}
		if containsFlag(clean, "--json") {
			return true, RunMailRead("accounts", readArgs, out, errOut)
		}
		return true, showCommand(tty, "Accounts", append([]string{"accounts"}, readArgs...), out, errOut)
	}
	return true, mailResult(out, errOut, false, nil, 2, "invalid_arguments", "this command has no interactive flow")
}
func containsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || a == flag+"=true" {
			return true
		}
	}
	return false
}

func runCommandMenu(tty *os.File, in io.Reader, out, errOut io.Writer) int {
	choices := []cliform.Choice{{ID: "send", Label: "Send email"}, {ID: "draft", Label: "Create draft"}, {ID: "messages", Label: "Browse cached messages"}, {ID: "search", Label: "Search messages"}, {ID: "accounts", Label: "Accounts"}, {ID: "outbox", Label: "Outbox"}, {ID: "skill", Label: "Print agent skill"}, {ID: "install", Label: "Install agent skill"}, {ID: "inbox", Label: "Open inbox"}, {ID: "quit", Label: "Quit"}}
	for {
		action, e := cliform.Select(tty, "Choose a command", choices)
		if errors.Is(e, cliform.ErrCanceled) || action == "quit" {
			return 0
		}
		if e != nil {
			return 1
		}
		code := 0
		switch action {
		case "send":
			code = RunHeadless([]string{"send", "--interactive"}, in, out, errOut)
		case "draft":
			code = RunHeadless([]string{"draft", "create", "--interactive"}, in, out, errOut)
		case "messages":
			code = messagesInteractive(tty, []string{"list"}, out, errOut)
		case "search":
			code = messagesInteractive(tty, []string{"search"}, out, errOut)
		case "accounts":
			code = showCommand(tty, "Accounts", []string{"accounts", "list"}, out, errOut)
		case "outbox":
			code = outboxInteractive(tty, nil, out, errOut)
		case "skill":
			return RunSkill(nil, out, errOut)
		case "install":
			code = installSkillInteractive(tty, nil, out, errOut)
		case "inbox":
			return OpenInbox
		}
		if code != 0 {
			if e = cliform.Show(tty, "Command stopped", "The command could not finish. Check the diagnostic printed after returning to your terminal."); e != nil {
				return 1
			}
		}
	}
}
func captureRead(command string, args []string) (map[string]any, error) {
	var out, errs bytes.Buffer
	args = append(append([]string{}, args...), "--json")
	if code := RunMailRead(command, args, &out, &errs); code != 0 {
		return nil, fmt.Errorf("cached read failed: %s", out.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if e := json.Unmarshal(out.Bytes(), &envelope); e != nil {
		return nil, e
	}
	return envelope.Data, nil
}
func showCommand(tty *os.File, title string, args []string, out, errOut io.Writer) int {
	data, e := captureRead(args[0], args[1:])
	if e != nil {
		return mailResult(out, errOut, false, nil, 1, "read_error", "cannot read cached data")
	}
	var text bytes.Buffer
	if readSuccess(&text, false, data) != 0 {
		return 1
	}
	if e = cliform.Show(tty, title, text.String()); e != nil {
		return 1
	}
	return 0
}
func messagesInteractive(tty *os.File, args []string, out, errOut io.Writer) int {
	action := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = args[0]
		args = args[1:]
	}
	if action == "" {
		selected, e := cliform.Select(tty, "Messages", []cliform.Choice{{ID: "list", Label: "Browse cached inbox"}, {ID: "search", Label: "Search cached messages"}, {ID: "read", Label: "Read a message by ID"}})
		if e != nil {
			return 0
		}
		action = selected
	}
	flags := append([]string{}, args...)
	query := ""
	if action == "search" || action == "read" {
		if len(flags) > 0 && !strings.HasPrefix(flags[0], "-") {
			query = flags[0]
			flags = flags[1:]
		}
		if query == "" {
			label := "Search query"
			if action == "read" {
				label = "Message ID (cache:ID)"
			}
			value, e := cliform.Input(tty, label, "")
			if e != nil {
				return 0
			}
			query = value
		}
	}
	if action == "read" {
		cmdArgs := append([]string{"read", query}, flags...)
		if containsFlag(flags, "--json") {
			return RunMailRead("messages", cmdArgs, out, errOut)
		}
		return showCommand(tty, "Message", append([]string{"messages"}, cmdArgs...), out, errOut)
	}
	if action != "list" && action != "search" {
		return mailResult(out, errOut, false, nil, 2, "invalid_arguments", "unknown message action")
	}
	base := []string{action}
	if query != "" {
		base = append(base, query)
	}
	if containsFlag(flags, "--json") {
		return RunMailRead("messages", append(base, flags...), out, errOut)
	}
	offset := 0
	for {
		page, e := captureRead("messages", append(append(append([]string{}, base...), flags...), "--limit", "20", "--offset", fmt.Sprint(offset)))
		if e != nil {
			return mailResult(out, errOut, false, nil, 1, "read_error", "cannot query cached messages")
		}
		items, _ := page["items"].([]any)
		choices := []cliform.Choice{}
		for _, value := range items {
			m := value.(map[string]any)
			choices = append(choices, cliform.Choice{ID: fmt.Sprint(m["id"]), Label: fmt.Sprintf("%s  %s  [%s]", m["from"], m["subject"], m["account_name"])})
		}
		if page["next_offset"] != nil {
			choices = append(choices, cliform.Choice{ID: "next", Label: "Next page"})
		}
		if offset > 0 {
			choices = append(choices, cliform.Choice{ID: "prev", Label: "Previous page"})
		}
		choices = append(choices, cliform.Choice{ID: "back", Label: "Back"})
		title := "Cached messages"
		if len(items) == 0 {
			title = "No cached messages found"
		}
		id, e := cliform.Select(tty, title, choices)
		if e != nil || id == "back" {
			return 0
		}
		if id == "next" {
			offset += 20
			continue
		}
		if id == "prev" {
			offset = max(0, offset-20)
			continue
		}
		code := showCommand(tty, "Message", append([]string{"messages", "read", id}, flags...), out, errOut)
		if code != 0 {
			return code
		}
	}
}
func outboxInteractive(tty *os.File, args []string, out, errOut io.Writer) int {
	action := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = args[0]
		args = args[1:]
	}
	if action == "" {
		value, e := cliform.Select(tty, "Outbox", []cliform.Choice{{ID: "list", Label: "Inspect outgoing mail"}, {ID: "process", Label: "Process authorized due messages"}, {ID: "retry", Label: "Retry a failed or uncertain send"}, {ID: "cancel", Label: "Undo a queued send into a draft"}})
		if e != nil {
			return 0
		}
		action = value
	}
	if action == "list" {
		if containsFlag(args, "--json") {
			return RunMailRead("outbox", append([]string{"list"}, args...), out, errOut)
		}
		return showCommand(tty, "Outbox", append([]string{"outbox", "list"}, args...), out, errOut)
	}
	id := ""
	if action != "process" {
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			id = args[0]
			args = args[1:]
		} else {
			value, e := cliform.Input(tty, "Outbox ID", "")
			if e != nil {
				return 0
			}
			id = value
		}
	}
	if action == "status" {
		if containsFlag(args, "--json") {
			return RunMailRead("outbox", append([]string{"status", id}, args...), out, errOut)
		}
		return showCommand(tty, "Outbox status", append([]string{"outbox", "status", id}, args...), out, errOut)
	}
	if action != "process" && action != "retry" && action != "cancel" {
		return 2
	}
	warning := "Confirm " + action
	if action == "retry" {
		metadataArgs := []string{"status", id}
		if containsFlag(args, "--agent") {
			metadataArgs = append(metadataArgs, "--agent")
		}
		page, e := captureRead("outbox", metadataArgs)
		if e != nil {
			return 1
		}
		warning = fmt.Sprintf("Retry outbox %s (%s). Check Sent first; retrying uncertain delivery can send a duplicate.", id, page["state"])
	}
	if action == "retry" {
		if e := cliform.Show(tty, "Before retrying", warning); e != nil {
			return 1
		}
		warning = "Retry outbox " + id
	}
	choice, e := cliform.Select(tty, warning, []cliform.Choice{{ID: "back", Label: "Go back"}, {ID: "confirm", Label: "Confirm " + action}})
	if e != nil || choice != "confirm" {
		return 0
	}
	cmd := []string{action}
	if id != "" {
		cmd = append(cmd, id)
	}
	cmd = append(cmd, args...)
	cmd = append(cmd, "--yes")
	return runOutboxAction(cmd, out, errOut)
}
func installSkillInteractive(tty *os.File, args []string, out, errOut io.Writer) int {
	agent := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("install-skill", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dryRun := fs.Bool("dry-run", false, "show destination without writing")
	fs.Bool("force", false, "request replacement; interactive replacement still requires confirmation")
	if e := fs.Parse(args); e != nil || fs.NArg() != 0 {
		return 2
	}
	if agent == "" {
		value, e := cliform.Select(tty, "Install agent skill", []cliform.Choice{{ID: "codex", Label: "Codex"}, {ID: "claude", Label: "Claude Code"}, {ID: "pi", Label: "Pi"}, {ID: "hermes", Label: "Hermes"}})
		if e != nil {
			return 0
		}
		agent = value
	}
	var preview, errs bytes.Buffer
	if code := RunInstallSkill([]string{agent, "--dry-run"}, &preview, &errs); code != 0 {
		_, _ = io.Copy(errOut, &errs)
		return code
	}
	if *dryRun {
		_, e := io.Copy(out, &preview)
		if e != nil {
			return 1
		}
		return 0
	}
	if e := cliform.Show(tty, "Skill destination", preview.String()); e != nil {
		return 1
	}
	choice, e := cliform.Select(tty, "Install TideMail skill", []cliform.Choice{{ID: "back", Label: "Go back"}, {ID: "install", Label: "Install skill"}})
	if e != nil || choice != "install" {
		return 0
	}
	// A failed no-clobber install is shown before explicit replacement consent.
	var result, diagnostics bytes.Buffer
	code := RunInstallSkill([]string{agent}, &result, &diagnostics)
	if code != 0 && strings.Contains(diagnostics.String(), "already exists") {
		choice, e = cliform.Select(tty, "Existing skill: replace your saved guidance?", []cliform.Choice{{ID: "back", Label: "Keep existing file"}, {ID: "replace", Label: "Replace existing skill"}})
		if e != nil || choice != "replace" {
			return 0
		}
		return RunInstallSkill([]string{agent, "--force"}, out, errOut)
	}
	_, _ = io.Copy(out, &result)
	_, _ = io.Copy(errOut, &diagnostics)
	return code
}
