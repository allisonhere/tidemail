package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/allisonhere/tidemail/internal/cliform"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/mailapp"
	"github.com/allisonhere/tidemail/internal/smtp"
	"golang.org/x/term"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func IsMailCommand(s string) bool {
	switch s {
	case "cli", "send", "draft", "messages", "accounts", "outbox", "skill", "install-skill", "--help", "-h", "help":
		return true
	}
	return false
}

func RunHeadless(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		return 2
	}
	if handled, code := interactiveRoute(args, in, out, errOut); handled {
		return code
	}
	switch args[0] {
	case "help", "--help", "-h":
		_, _ = fmt.Fprintln(out, paint(out, "TideMail", 124, 58, 237)+" — terminal email\n\nUsage: tidemail [command]\n\nNo arguments opens the three-pane client.\nCommands: cli (interactive menu), send, draft create, messages list|search|read, accounts list,\n          outbox list|status|process|retry|cancel, skill, install-skill, plugin\nRun tidemail COMMAND --help for options.\nExit codes: 0 success, 1 operation failed, 2 invalid usage, 3 permission denied.")
		return 0
	case "skill":
		return RunSkill(args[1:], out, errOut)
	case "install-skill":
		return RunInstallSkill(args[1:], out, errOut)
	case "accounts", "messages":
		return RunMailRead(args[0], args[1:], out, errOut)
	case "outbox":
		if len(args) > 1 && (args[1] == "process" || args[1] == "retry" || args[1] == "cancel") {
			return runOutboxAction(args[1:], out, errOut)
		}
		return RunMailRead(args[0], args[1:], out, errOut)
	case "send":
		return runComposeCLI(false, args[1:], in, out, errOut)
	case "draft":
		if len(args) > 1 && args[1] == "create" {
			return runComposeCLI(true, args[2:], in, out, errOut)
		}
		_, _ = fmt.Fprintln(out, "Usage: tidemail draft create [send options] [--body-file PATH]")
		if len(args) == 1 || hasHelpFlag(args[1:]) {
			return 0
		}
		return 2
	}
	return 2
}

func paint(w io.Writer, s string, r, g, b int) string {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return s
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, s)
}
func mailResult(out, errOut io.Writer, j bool, data any, code int, id, message string) int {
	if j {
		v := map[string]any{"schema_version": 1, "ok": code == 0}
		if code == 0 {
			v["data"] = data
		} else {
			v["error"] = map[string]string{"code": id, "message": message}
		}
		if err := json.NewEncoder(out).Encode(v); err != nil {
			_, _ = fmt.Fprintln(errOut, "cannot write result")
			return 1
		}
	} else if code != 0 {
		_, _ = fmt.Fprintln(errOut, paint(errOut, "error: ", 239, 68, 68)+message)
	} else {
		switch v := data.(type) {
		case map[string]any:
			if state, ok := v["state"]; ok {
				if _, e := fmt.Fprintf(out, "%s %v\n", paint(out, fmt.Sprint(state), 6, 182, 212), v["id"]); e != nil {
					return 1
				}
			} else {
				raw, e := json.Marshal(v)
				if e != nil {
					return 1
				}
				if _, e = fmt.Fprintln(out, string(raw)); e != nil {
					return 1
				}
			}
		default:
			if _, e := fmt.Fprintln(out, data); e != nil {
				return 1
			}
		}
	}
	return code
}

func runComposeCLI(draftOnly bool, args []string, in io.Reader, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var account, to, cc, bcc, subject, body, file, markdown string
	var attachments []string
	var yes, now, j, dry, preview, noSignature, signature, agent, interactive, noInteractive bool
	fs.StringVar(&account, "account", "", "stable account ID or unambiguous name")
	fs.StringVar(&to, "to", "", "comma-separated recipients")
	fs.StringVar(&cc, "cc", "", "CC recipients")
	fs.StringVar(&bcc, "bcc", "", "BCC recipients")
	fs.StringVar(&subject, "subject", "", "subject")
	fs.StringVar(&body, "body", "", "message body")
	fs.StringVar(&file, "file", "", "body file")
	fs.StringVar(&file, "body-file", "", "body file")
	fs.StringVar(&markdown, "markdown", "", "Markdown body file")
	fs.Var(stringList(&attachments), "attach", "attachment path, repeatable")
	fs.BoolVar(&signature, "signature", true, "append configured signature")
	fs.BoolVar(&noSignature, "no-signature", false, "omit signature")
	fs.BoolVar(&yes, "yes", false, "authorize sending this invocation")
	fs.BoolVar(&now, "send-now", false, "bypass configured delay")
	fs.BoolVar(&j, "json", false, "versioned JSON result")
	fs.BoolVar(&dry, "dry-run", false, "validate and build without saving")
	fs.BoolVar(&preview, "preview", false, "open the editable review wizard through /dev/tty")
	fs.BoolVar(&agent, "agent", false, "enforce configured agent permissions")
	fs.BoolVar(&interactive, "interactive", false, "open the compose wizard through the controlling terminal")
	fs.BoolVar(&noInteractive, "no-interactive", false, "never open automatic prompts")
	for _, arg := range args {
		if arg == "--json" || arg == "--json=true" {
			j = true
		}
	}
	usageOut := errOut
	if hasHelpFlag(args) {
		usageOut = out
	}
	fs.Usage = func() {
		_, _ = fmt.Fprintln(usageOut, "Usage: tidemail send [options]\nIn a terminal, missing fields open the wizard. Complete scripted commands\nwithout --yes save a draft. Bodies come from one of\n--body, --file, --markdown, or piped stdin. Queue processing: outbox process.")
		fs.SetOutput(usageOut)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return mailResult(out, errOut, j, nil, 2, "invalid_arguments", "invalid command options; use --help")
	}
	fail := func(code int, id, msg string) int { return mailResult(out, errOut, j, nil, code, id, msg) }
	if fs.NArg() != 0 {
		return fail(2, "invalid_arguments", "unexpected positional arguments")
	}
	sources := 0
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "body", "file", "body-file", "markdown":
			sources++
		}
	})
	if sources > 1 {
		return fail(2, "body_conflict", "choose one body source")
	}
	if interactive && noInteractive || preview && noInteractive {
		return fail(2, "invalid_arguments", "interactive options conflict with --no-interactive")
	}
	if draftOnly && (yes || now || preview) {
		return fail(2, "invalid_arguments", "draft create cannot authorize delivery")
	}
	if preview && dry {
		return fail(2, "invalid_arguments", "choose --preview or --dry-run")
	}
	cfg, err := config.Load()
	if err != nil {
		return fail(1, "config_error", "cannot load config; check tidemail configuration")
	}
	a, err := mailapp.ResolveAccount(cfg, account)
	if err != nil {
		return fail(2, "account_error", err.Error())
	}
	cap := "drafts.create"
	if !draftOnly && (yes || preview) {
		cap = "mail.send"
	}
	if agent && !interactive && !preview && !mailapp.Allowed(cfg, cap) {
		return fail(3, "permission_denied", "agent capability denied: "+cap)
	}
	if sources == 0 {
		if f, ok := in.(*os.File); !ok || !term.IsTerminal(int(f.Fd())) {
			raw, e := io.ReadAll(io.LimitReader(in, (16<<20)+1))
			if e != nil {
				return fail(1, "body_read_error", "cannot read stdin")
			}
			if len(raw) > 16<<20 {
				return fail(2, "body_too_large", "stdin body exceeds 16 MiB")
			}
			body = string(raw)
		}
	} else if file != "" || markdown != "" {
		p := file
		if markdown != "" {
			p = markdown
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			return fail(1, "body_read_error", "cannot read body file")
		}
		body = string(raw)
	}
	d := db.Draft{To: to, CC: cc, BCC: bcc, Subject: subject, BodyText: body}
	for _, p := range attachments {
		att, e := cliform.LoadAttachment(p)
		if e != nil {
			return fail(1, "attachment_error", e.Error())
		}
		d.Attachments = append(d.Attachments, att)
	}
	auto := !noInteractive && !yes && !j && !agent && !dry && terminalIO(in, out) && (strings.TrimSpace(to) == "" || strings.TrimSpace(subject) == "" || (strings.TrimSpace(body) == "" && len(attachments) == 0))
	if interactive || preview || auto {
		return composeInteractive(cfg, a, d, cliform.ComposeOptions{DraftOnly: draftOnly, DryRun: dry, SendNow: now, Signature: signature && !noSignature, Agent: agent}, out, errOut, j)
	}
	msg, err := mailapp.Prepare(a, d, signature && !noSignature)
	if err != nil {
		return fail(2, "invalid_message", err.Error())
	}
	if !draftOnly && (yes || preview) && (strings.TrimSpace(subject) == "" || (strings.TrimSpace(body) == "" && len(attachments) == 0)) {
		return fail(2, "incomplete_message", "sending requires a subject and body or attachment")
	}
	if dry {
		msg.EnsureIdentity(msg.From)
		raw := smtp.BuildRaw(a, msg)
		return mailResult(out, errOut, j, map[string]any{"state": "validated", "id": nil, "account_id": a.ID, "mime_bytes": len(raw), "attachments": len(d.Attachments)}, 0, "", "")
	}
	database, err := db.Open()
	if err != nil {
		return fail(1, "database_error", "cannot open mail cache")
	}
	defer database.Close()
	if draftOnly || !yes {
		id, e := mailapp.SaveDraft(database, a, d)
		if e != nil {
			return fail(1, "draft_error", "cannot save draft")
		}
		return mailResult(out, errOut, j, map[string]any{"state": "drafted", "id": id, "account_id": a.ID}, 0, "", "")
	}
	due := time.Now().Add(time.Duration(cfg.Display.SendDelaySeconds) * time.Second)
	if now {
		due = time.Now()
	}
	id, err := mailapp.Enqueue(database, cfg, a, msg, d, due)
	if err != nil {
		return fail(1, "queue_error", "cannot persist outgoing message")
	}
	if now {
		sessions := imapClient.NewSessionPool()
		defer sessions.Close()
		result := mailapp.Deliver(context.Background(), database, sessions, id, a, false, nil)
		if result.CleanupErr != nil || result.SentCopyErr != nil {
			_, _ = fmt.Fprintln(errOut, "warning: delivery recorded; Sent copy or draft cleanup failed")
		}
	}
	item, e := database.GetOutbox(id)
	if e != nil {
		return fail(1, "outbox_error", "cannot read delivery status")
	}
	code := 0
	if item.State == db.OutboxFailed || item.State == db.OutboxUncertain {
		code = 1
	}
	if code != 0 && j {
		if e := json.NewEncoder(out).Encode(map[string]any{"schema_version": 1, "ok": false, "data": map[string]any{"state": item.State, "id": id, "account_id": a.ID}, "error": map[string]string{"code": "delivery_" + item.State, "message": "inspect outbox status before retrying"}}); e != nil {
			return 1
		}
		return code
	}
	if code != 0 {
		return fail(code, "delivery_"+item.State, fmt.Sprintf("outbox %d is %s; inspect outbox status", id, item.State))
	}
	return mailResult(out, errOut, j, map[string]any{"state": item.State, "id": id, "account_id": a.ID, "next_attempt": item.NextAttempt}, 0, "", "")
}

func safeTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 32 && r != 127 && (r < 0x80 || r > 0x9f) {
			return r
		}
		return -1
	}, s)
}

func runOutboxAction(args []string, out, errOut io.Writer) int {
	action := args[0]
	rest := args[1:]
	idText := ""
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		idText = rest[0]
		rest = rest[1:]
	}
	fs := flag.NewFlagSet("outbox "+action, flag.ContinueOnError)
	fs.SetOutput(errOut)
	jsonDefault := false
	for _, arg := range rest {
		if arg == "--json" || arg == "--json=true" {
			jsonDefault = true
		}
	}
	j := fs.Bool("json", jsonDefault, "versioned JSON")
	yes := fs.Bool("yes", false, "authorize explicit retry or processing")
	agent := fs.Bool("agent", false, "enforce agent permissions")
	usageOut := errOut
	if hasHelpFlag(rest) {
		usageOut = out
	}
	fs.Usage = func() {
		_, _ = fmt.Fprintf(usageOut, "Usage: tidemail outbox %s [ID] [--yes] [--json] [--agent]\nprocess sends due queue entries and exits; run periodically.\nretry requires --yes; inspect Sent before retrying uncertain delivery.\n", action)
	}
	if e := fs.Parse(rest); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return mailResult(out, errOut, *j, nil, 2, "invalid_arguments", "invalid command options; use --help")
	}
	fail := func(code int, id, msg string) int { return mailResult(out, errOut, *j, nil, code, id, msg) }
	if fs.NArg() > 0 {
		return fail(2, "invalid_arguments", "unexpected arguments")
	}
	if action == "process" && idText != "" {
		return fail(2, "invalid_arguments", "process does not accept an ID")
	}
	cfg, e := config.Load()
	if e != nil {
		return fail(1, "config_error", "cannot load configuration")
	}
	if *agent && !mailapp.Allowed(cfg, "mail.send") {
		return fail(3, "permission_denied", "agent capability denied: mail.send")
	}
	if (action == "retry" || *agent) && !*yes {
		return fail(3, "approval_required", "explicit --yes authorization is required")
	}
	database, e := db.Open()
	if e != nil {
		return fail(1, "database_error", "cannot open database")
	}
	defer database.Close()
	if action != "process" {
		id, e := strconv.ParseInt(idText, 10, 64)
		if e != nil || id <= 0 {
			return fail(2, "invalid_id", "provide a positive outbox ID")
		}
		if action == "retry" {
			e = database.RetryOutbox(id, config.NormalizeSendMaxAttempts(cfg.Display.SendMaxAttempts))
		} else {
			_, e = database.EditOutbox(id)
		}
		if e != nil {
			return fail(1, "outbox_error", "operation failed; inspect outbox status")
		}
		state := "queued"
		if action == "cancel" {
			state = "drafted"
		}
		return mailResult(out, errOut, *j, map[string]any{"state": state, "id": id}, 0, "", "")
	}
	if e = database.RecoverStaleOutbox(); e != nil {
		return fail(1, "recovery_error", "cannot recover interrupted sends")
	}
	items, e := database.ListOutbox()
	if e != nil {
		return fail(1, "outbox_error", "cannot list outbox")
	}
	sessions := imapClient.NewSessionPool()
	defer sessions.Close()
	processed := 0
	failed := 0
	for _, item := range items {
		if item.State != db.OutboxQueued || item.NextAttempt > time.Now().Unix() {
			continue
		}
		a, ok := mailapp.ResolveOutboxAccount(cfg, item)
		if !ok {
			failed++
			continue
		}
		r := mailapp.Deliver(context.Background(), database, sessions, item.ID, a, false, nil)
		if !r.Skipped {
			processed++
		}
		if r.Err != nil {
			failed++
		}
		if r.SentCopyErr != nil || r.CleanupErr != nil {
			_, _ = fmt.Fprintln(errOut, "warning: delivery recorded; Sent copy or draft cleanup failed")
		}
	}
	if failed > 0 {
		return fail(1, "processing_failed", fmt.Sprintf("processed %d queue entries; %d failed; inspect outbox list", processed, failed))
	}
	return mailResult(out, errOut, *j, map[string]any{"processed": processed}, 0, "", "")
}
