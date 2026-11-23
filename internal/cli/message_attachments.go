package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/mailapp"
)

func runMessageAttachments(args []string, out, errOut io.Writer, jsonMode bool) int {
	fail := func(code, message string, status int) int {
		return readFailure(out, errOut, jsonMode, code, message, status)
	}
	fs := flag.NewFlagSet("messages attachments", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.BoolVar(&jsonMode, "json", jsonMode, "Print a versioned JSON envelope")
	agent := fs.Bool("agent", false, "Enforce the configured agent permissions")
	dir := fs.String("out", "", "Required output directory (created if absent)")
	fs.Usage = func() {
		w := errOut
		if hasHelpFlag(args) {
			w = out
		}
		fmt.Fprint(w, "Usage: tidemail messages attachments cache:ID --out DIR [--agent] [--json]\nExport all attachments, including inline parts. Uses cached bytes, otherwise IMAP.\nExisting files are preserved; duplicate names receive numeric suffixes.\nOptions may precede or follow the ID.\n")
		fs.SetOutput(w)
		fs.PrintDefaults()
	}
	pos, err := parseReadFlags(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return fail("invalid_arguments", err.Error(), 2)
	}
	if len(pos) != 1 || strings.TrimSpace(*dir) == "" {
		return fail("invalid_arguments", "Provide one message ID and --out DIR.", 2)
	}
	id, err := readID(pos[0])
	if err != nil {
		return fail("invalid_arguments", "Invalid message ID; use cache:ID from messages list.", 2)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("config_error", "Cannot load configuration.", 1)
	}
	if *agent && !mailapp.Allowed(cfg, "attachments.read") {
		return fail("permission_denied", "Agent capability attachments.read is not enabled in agent_permissions.", 3)
	}
	database, err := db.Open()
	if err != nil {
		return fail("database_error", "Cannot open the mail cache.", 1)
	}
	defer database.Close()
	m, err := database.GetMessage(id)
	if errors.Is(err, sql.ErrNoRows) {
		return fail("not_found", "Cached message not found.", 1)
	}
	if err != nil {
		return fail("database_error", "Cannot read cached message.", 1)
	}
	atts, err := database.GetAttachmentsMeta(id)
	if err != nil {
		return fail("database_error", "Cannot read attachment metadata.", 1)
	}
	// Resolve every part before writing files, so a failed fetch leaves no export.
	missing := len(atts) == 0 && m.HasAttachment
	available := make([]bool, len(atts))
	for i := range atts {
		cached, e := database.AttachmentCached(atts[i].ID)
		if e != nil {
			return fail("database_error", "Cannot check attachment availability.", 1)
		}
		available[i] = cached
		if !cached {
			missing = true
			continue
		}
		atts[i].Data, err = database.GetAttachmentData(atts[i].ID)
		if err != nil {
			return fail("database_error", "Cannot read cached attachment contents.", 1)
		}
	}
	if missing {
		remote, e := fetchMessageAttachments(database, cfg, m)
		if e != nil {
			return fail("attachment_fetch_failed", "Cannot fetch attachments from IMAP; check the account connection and sync the message before retrying.", 1)
		}
		if len(atts) == 0 {
			atts = remote
		} else {
			if len(atts) != len(remote) {
				return fail("attachment_fetch_failed", "Attachment metadata changed; sync before retrying.", 1)
			}
			for i := range atts {
				if atts[i].Filename != remote[i].Filename || atts[i].ContentType != remote[i].ContentType {
					return fail("attachment_fetch_failed", "Attachment metadata changed; sync before retrying.", 1)
				}
				if !available[i] {
					atts[i].Data = remote[i].Data
				}
			}
		}
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		return fail("output_error", "Cannot resolve output directory.", 1)
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return fail("output_error", "Cannot create output directory.", 1)
	}
	items := []map[string]any{}
	for _, a := range atts {
		path, e := writeExportAttachment(abs, a.Filename, a.Data)
		if e != nil {
			return fail("output_error", "Cannot write attachment; earlier files may have been saved.", 1)
		}
		items = append(items, map[string]any{"name": a.Filename, "path": path, "size": len(a.Data), "type": a.ContentType, "inline": a.Inline})
	}
	return readSuccess(out, jsonMode, map[string]any{"id": fmt.Sprintf("cache:%d", id), "items": items})
}

func fetchMessageAttachments(database *db.DB, cfg config.Config, m db.Message) ([]db.Attachment, error) {
	mb, err := database.GetMailbox(m.MailboxID)
	if err != nil {
		return nil, err
	}
	account, err := database.GetAccount(mb.AccountID)
	if err != nil {
		return nil, err
	}
	validity, err := database.MailboxUIDValidity(m.MailboxID)
	// Refuse unknown UID identity rather than risk fetching a different message.
	if err != nil || validity == 0 {
		return nil, fmt.Errorf("sync required to establish UIDVALIDITY")
	}
	for _, a := range cfg.Accounts {
		if a.ID != account.ConfigID {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		ctx = imap.WithUIDValidity(ctx, mb.Name, validity)
		client := imap.New(a)
		if err := client.Connect(ctx); err != nil {
			return nil, err
		}
		defer client.Close()
		raw, err := client.FetchRaw(ctx, mb.Name, m.UID)
		if err != nil {
			return nil, err
		}
		header, err := mail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		if m.MessageID != "" && conversation.NormalizeMessageID(header.Header.Get("Message-ID")) != conversation.NormalizeMessageID(m.MessageID) {
			return nil, fmt.Errorf("message identity changed")
		}
		return imap.DecodeBody(raw).AttachmentData, nil
	}
	return nil, fmt.Errorf("configured account not found")
}

func writeExportAttachment(dir, name string, data []byte) (string, error) {
	// Treat both slash conventions as separators, even on Unix.
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." || name == "/" {
		name = "attachment"
	}
	for n := 0; ; n++ {
		candidate := name
		if n > 0 {
			candidate = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, filepath.Ext(name)), n, filepath.Ext(name))
		}
		path := filepath.Join(dir, candidate)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
			return "", err
		}
		return path, nil
	}
}
