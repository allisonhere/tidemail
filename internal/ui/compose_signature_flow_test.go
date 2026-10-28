package ui

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/mailapp"
	"github.com/allisonhere/tidemail/internal/smtp"
	tea "github.com/charmbracelet/bubbletea"
)

// Exercise the real UI send preparation and outgoing MIME serializer, without
// delivering a SendQueuedMsg to the application or opening any SMTP connection.
func TestComposeSignatureMIMEFlow(t *testing.T) {
	for _, mode := range []string{"new", "reply", "forward", "mailto", "mailto-body"} {
		for _, source := range []int{0, 1} {
			for _, signature := range []string{"multiline", "empty", "whitespace"} {
				t.Run(fmt.Sprintf("%s/account=%d/%s", mode, source, signature), func(t *testing.T) {
					m := mailtoTestModel(t, source) // Unified Inbox, source account != sidebar.
					for i := range m.cfg.Accounts {
						switch signature {
						case "multiline":
							m.cfg.Accounts[i].Signature = fmt.Sprintf("Signature %d\nSecond line %d", i, i)
						case "empty":
							m.cfg.Accounts[i].Signature = ""
						case "whitespace":
							m.cfg.Accounts[i].Signature = " \n\t "
						}
					}
					var c ComposeModel
					account := m.cfg.Accounts[source]
					original := db.Message{From: "original@example.com", BodyText: "Original text", Subject: "Original", MessageID: "<original@example.com>"}
					switch mode {
					case "new":
						m.cfg.DefaultAccountID = account.ID
						next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
						c = next.(Model).compose
					case "reply":
						c = NewReply(original, account, m.cfg.Accounts, nil)
					case "forward":
						c = NewForward(original, account, m.cfg.Accounts, nil)
					default:
						target := "mailto:recipient@example.com"
						if mode == "mailto-body" {
							target += "?body=URL%20body%0D%0ASecond%20paragraph%0A"
						}
						if cmd := m.openReaderLink(target); cmd != nil {
							t.Fatal("mailto initialization returned background command")
						}
						c = m.compose
					}
					if c.selectedAccount().ID != account.ID {
						t.Fatal("initial signature account mismatch")
					}
					if mode == "new" && c.bodyInput.Value() != "" {
						t.Fatal("new composer should not insert signature into editor")
					}
					if mode == "mailto" && c.bodyInput.Value() != "" {
						t.Fatal("body-less mailto inserted signature into editor")
					}
					if mode == "mailto-body" && c.bodyInput.Value() != "URL body\nSecond paragraph\n" {
						t.Fatal("URL body overwritten or line breaks lost")
					}
					if (mode == "reply" || mode == "forward") && !strings.Contains(c.bodyInput.Value(), "Original text") {
						t.Fatal("quote lost")
					}
					if strings.Contains(c.bodyInput.Value(), "Signature ") {
						t.Fatal("signature duplicated into editable body")
					}
					c.toInput.SetValue("recipient@example.com")
					if mode == "new" {
						c.bodyInput.SetValue("New body\nSecond paragraph\n")
					}
					for _, switched := range []bool{false, true} {
						current := c
						if switched {
							var cmd tea.Cmd
							current, cmd, _ = current.Update(tea.KeyMsg{Type: tea.KeyCtrlU}, DefaultKeys)
							if cmd != nil {
								t.Fatal("switching sender should not send")
							}
						}
						expected := source
						if switched {
							expected = 1 - source
						}
						if current.selectedAccount().ID != m.cfg.Accounts[expected].ID {
							t.Fatal("sender switching chose wrong signature account")
						}
						body := current.bodyInput.Value()
						for _, reopened := range []bool{false, true} {
							sent := current
							if reopened {
								id, err := m.db.SaveDraft(sent.toDraftRecord())
								if err != nil {
									t.Fatal(err)
								}
								draft, err := m.db.GetDraft(id)
								if err != nil {
									t.Fatal(err)
								}
								sent = NewComposeFromDraft(draft, m.cfg.Accounts, nil)
								if sent.bodyInput.Value() != body || sent.selectedAccount().ID != m.cfg.Accounts[expected].ID {
									t.Fatal("draft changed body or sender")
								}
							}
							prepared, cmd, closed := sent.send()
							if cmd == nil || closed || !prepared.busy {
								t.Fatal("normal send preparation failed")
							}
							queued, ok := cmd().(SendQueuedMsg)
							if !ok {
								t.Fatal("expected queued message only")
							}
							sig := strings.TrimSpace(m.cfg.Accounts[expected].Signature)
							want := body
							if sig != "" {
								want = strings.TrimRight(body, "\n") + "\n\n-- \n" + sig + "\n"
							}
							if queued.Msg.Body != want || queued.Account.ID != m.cfg.Accounts[expected].ID {
								t.Fatal("prepared body or signature sender mismatch")
							}
							// Check the persisted Outbox message as well as immediate preparation.
							id, err := mailapp.Enqueue(m.db, m.cfg, queued.Account, queued.Msg, sent.toDraftRecord(), time.Time{})
							if err != nil {
								t.Fatal(err)
							}
							item, err := m.db.GetOutbox(id)
							if err != nil {
								t.Fatal(err)
							}
							outgoing, err := mailapp.OutboxMessage(item, queued.Account)
							if err != nil {
								t.Fatal(err)
							}
							raw := smtp.BuildRaw(queued.Account, outgoing)
							msg, err := mail.ReadMessage(bytes.NewReader(raw))
							if err != nil {
								t.Fatal(err)
							}
							if msg.Header.Get("From") != smtp.AccountSender(m.cfg.Accounts[expected]) {
								t.Fatal("serialized MIME sender changed")
							}
							media, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
							if err != nil || (media != "multipart/alternative" && media != "text/plain") {
								t.Fatalf("bad MIME content type %q", media)
							}
							reader := multipart.NewReader(msg.Body, params["boundary"])
							parts := map[string]string{}
							if media == "text/plain" {
								data, readErr := io.ReadAll(msg.Body)
								if readErr != nil {
									t.Fatal(readErr)
								}
								parts[media] = strings.ReplaceAll(string(data), "\r\n", "\n")
							}
							for media == "multipart/alternative" {
								part, err := reader.NextPart()
								if err == io.EOF {
									break
								}
								if err != nil {
									t.Fatal(err)
								}
								data, err := io.ReadAll(part)
								if err != nil {
									t.Fatal(err)
								}
								kind, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
								if err != nil {
									t.Fatal(err)
								}
								parts[kind] = strings.ReplaceAll(string(data), "\r\n", "\n")
							}
							mimeWant := want
							if !strings.HasSuffix(mimeWant, "\n") {
								mimeWant += "\n"
							}
							if parts["text/plain"] != mimeWant {
								t.Fatalf("actual plain MIME body mismatch: got %q, want %q", parts["text/plain"], mimeWant)
							}
							if sig != "" {
								for _, line := range strings.Split(sig, "\n") {
									if strings.Count(parts["text/plain"], line) != 1 || strings.Count(parts["text/html"], line) != 1 {
										t.Fatal("actual MIME signature missing or duplicated")
									}
								}
								if !strings.Contains(parts["text/html"], "<br>") {
									t.Fatal("HTML signature line breaks missing")
								}
							} else if strings.Contains(parts["text/plain"], "-- \n") || strings.Contains(parts["text/html"], "Signature ") {
								t.Fatal("empty signature was inserted")
							}
						}
					}
				})
			}
		}
	}
}

func TestSignatureAccountFormAssociation(t *testing.T) {
	m := mailtoTestModel(t, 1)
	for _, account := range m.cfg.Accounts {
		account.Signature = "First line\nSecond line"
		form := NewAccountManager(m.db)
		form.populateFormFrom(account)
		if form.sigArea.Value() != account.Signature {
			t.Fatal("account form lost multiline signature")
		}
		saved := form.buildCfg()
		if saved.ID != account.ID || saved.Signature != account.Signature || saved.From != account.From {
			t.Fatal("account form lost signature association")
		}
	}
}
