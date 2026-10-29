package ui

import (
	"fmt"
	"html"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/mailapp"
	tea "github.com/charmbracelet/bubbletea"
)

func TestParseComposeMailto(t *testing.T) {
	for _, tc := range []struct {
		name, uri string
		want      mailtoFields
		bad       bool
	}{
		{"basic", "mailto:a@example.com", mailtoFields{to: "a@example.com"}, false},
		{"multiple", "mailto:a@example.com,b@example.com", mailtoFields{to: "a@example.com,b@example.com"}, false},
		{"query", "mailto:a@example.com?TO=b@example.com&to=c@example.com&CC=d@example.com&Bcc=e@example.com", mailtoFields{to: "a@example.com, b@example.com, c@example.com", cc: "d@example.com", bcc: "e@example.com"}, false},
		{"query only", "mailto:?to=a@example.com", mailtoFields{to: "a@example.com"}, false},
		{"subject", "mailto:a@example.com?subject=Meeting%20Request", mailtoFields{to: "a@example.com", subject: "Meeting Request"}, false},
		{"body", "mailto:a@example.com?body=Hello%2C%0D%0A%0D%0AWorld", mailtoFields{to: "a@example.com", body: "Hello,\n\nWorld"}, false},
		{"unicode", "MAILTO:a@example.com?SuBjEcT=R%C3%A9union&body=Hello%20%E4%B8%96%E7%95%8C", mailtoFields{to: "a@example.com", subject: "Réunion", body: "Hello 世界"}, false},
		{"empty", "mailto:?to=&cc=&bcc=&subject=&body=", mailtoFields{}, false},
		{"unknown", "mailto:a@example.com?attach=/etc/passwd&command=send&subject=Review", mailtoFields{to: "a@example.com", subject: "Review"}, false},
		{"once", "mailto:a%2Bb@example.com?subject=%2520+%2B&body=%250A%26x%3Dy", mailtoFields{to: "a+b@example.com", subject: "%20++", body: "%0A&x=y"}, false},
		{"display name", "mailto:%22A%20Person%22%20%3Ca@example.com%3E", mailtoFields{to: `"A Person" <a@example.com>`}, false},
		{"bad escape", "mailto:a%zz@example.com", mailtoFields{}, true},
		{"bad query escape", "mailto:a@example.com?subject=%xx", mailtoFields{}, true},
		{"invalid address", "mailto:not-an-address", mailtoFields{}, true},
		{"invalid cc", "mailto:a@example.com?cc=bad", mailtoFields{}, true},
		{"header injection", "mailto:a@example.com?subject=Hi%0ABcc:x@example.com", mailtoFields{}, true},
		{"address injection", "mailto:a@example.com%0ABcc:b@example.com", mailtoFields{}, true},
		{"control body", "mailto:a@example.com?body=%1Bcommand", mailtoFields{}, true},
		{"invalid utf8", "mailto:a@example.com?body=%FF", mailtoFields{}, true},
		{"repeat subject", "mailto:a@example.com?subject=One&SUBJECT=Two", mailtoFields{}, true},
		{"host syntax", "mailto://a@example.com", mailtoFields{}, true},
		{"fragment", "mailto:a@example.com#fragment", mailtoFields{}, true},
		{"wrong scheme", "https://example.com", mailtoFields{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseComposeMailto(tc.uri)
			if (err != nil) != tc.bad {
				t.Fatalf("error=%v", err)
			}
			if !tc.bad && got != tc.want {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func mailtoTestModel(t *testing.T, source int) Model {
	t.Helper()
	target := "mailto:advisor@example.edu?cc=copy@example.edu&bcc=hidden@example.edu&subject=Meeting%20Request&body=Hello%2C%0A%0AWorld"
	m := readerLinksModel(t, `<p>Ordinary body.</p><a href="`+html.EscapeString(target)+`">Contact advisor</a>`, "")
	m.cfg.Accounts = []config.AccountConfig{
		{ID: "personal", Name: "Personal", User: "p@example.com", From: "Personal <p@example.com>", SMTPHost: "smtp.example.com", Signature: "Personal signature"},
		{ID: "work", Name: "Work", User: "w@example.com", From: "Work Identity <alias@example.com>", SMTPHost: "smtp.example.com", Signature: "Work signature"},
	}
	m.cfg.DefaultAccountID = "personal"
	for _, ac := range m.cfg.Accounts {
		id, err := m.db.AddAccount(ac.ID, ac.Name, "")
		if err != nil {
			t.Fatal(err)
		}
		mb, err := m.db.UpsertMailbox(db.Mailbox{AccountID: id, Name: "INBOX"})
		if err != nil {
			t.Fatal(err)
		}
		mailboxes, err := m.db.ListMailboxes(id)
		if err != nil {
			t.Fatal(err)
		}
		m.mailboxes = append(m.mailboxes, mailboxes...)
		if ac.ID == m.cfg.Accounts[source].ID {
			m.messages[0].MailboxID = mb
		}
	}
	m.accounts, _ = m.db.ListAccounts()
	m.messages[0].To = "p@example.com" // Deliberately unrelated to Work's stored owner.
	m.filteredMessages = m.messages
	m.sidebarRows = []sidebarRow{{kind: rowKindUnified}}
	m.sidebarCursor = 0
	return m
}

func TestReaderMailtoSourceAccount(t *testing.T) {
	for _, source := range []int{0, 1} {
		for _, unified := range []bool{false, true} {
			t.Run(fmt.Sprintf("source=%d/unified=%v", source, unified), func(t *testing.T) {
				requests, apply := recordReaderActivations(t)
				m := mailtoTestModel(t, source)
				if !unified {
					// A sidebar mailbox from the OTHER account must not choose the sender.
					for _, mb := range m.mailboxes {
						if mb.ID != m.messages[0].MailboxID {
							m.sidebarRows = []sidebarRow{{kind: rowKindMailbox, mailboxID: mb.ID, accountID: mb.AccountID}}
							break
						}
					}
				}
				for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, readerOpenKey} {
					original := m
					selected := apply(original, tea.KeyMsg{Type: tea.KeyCtrlN})
					if selected.overlay == overlayCompose {
						t.Fatal("selection composed")
					}
					selected = apply(selected, key)
					if selected.overlay != overlayCompose || selected.compose.selectedAccount().ID != m.cfg.Accounts[source].ID {
						t.Fatal("wrong sender or missing composer")
					}
					c := selected.compose
					if c.toInput.Value() != "advisor@example.edu" || c.ccInput.Value() != "copy@example.edu" || c.bccInput.Value() != "hidden@example.edu" || c.subjectInput.Value() != "Meeting Request" || c.bodyInput.Value() != "Hello,\n\nWorld\n\n⁂\n" {
						t.Fatalf("wrong prefill: %#v", c.toDraftRecord())
					}
					if c.busy || c.draftID != 0 || len(*requests) != 0 {
						t.Fatal("activation sent mail or launched handler")
					}
					draft := c.toDraftRecord()
					id, err := selected.db.SaveDraft(draft)
					if err != nil {
						t.Fatal(err)
					}
					saved, err := selected.db.GetDraft(id)
					if err != nil {
						t.Fatal(err)
					}
					restored := NewComposeFromDraft(saved, m.cfg.Accounts, nil)
					if restored.selectedAccount().ID != m.cfg.Accounts[source].ID || restored.bodyInput.Value() != draft.BodyText {
						t.Fatal("draft lost sender or body")
					}
					// Prepare only: no network dispatch. The marker adds one source-account signature.
					outgoing, err := mailapp.PrepareCompose(c.selectedAccount(), draft)
					if err != nil {
						t.Fatal(err)
					}
					if outgoing.From != m.cfg.Accounts[source].From || strings.Count(outgoing.Body, m.cfg.Accounts[source].Signature) != 1 || !strings.HasPrefix(outgoing.Body, "Hello,\n\nWorld\n\n-- \n") {
						t.Fatal("sender identity or normal signature lost")
					}
				}
			})
		}
	}
}

func TestReaderMailtoFallbackAndErrors(t *testing.T) {
	for _, mode := range []string{"missing mailbox", "missing account", "deleted config", "unusable source", "no default configured", "no accounts", "unusable default", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			requests, apply := recordReaderActivations(t)
			m := mailtoTestModel(t, 1)
			want := "personal"
			switch mode {
			case "missing mailbox":
				m.mailboxes = nil
			case "missing account":
				m.accounts = nil
			case "deleted config":
				m.cfg.Accounts = m.cfg.Accounts[:1]
			case "unusable source":
				m.cfg.Accounts[1].From = "bad"
				m.cfg.Accounts[1].User = "bad"
			case "no default configured":
				m.mailboxes = nil
				m.cfg.DefaultAccountID = ""
			case "no accounts":
				m.cfg.Accounts = nil
			case "unusable default":
				m.mailboxes = nil
				m.cfg.Accounts[0].SMTPHost = ""
			}
			m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
			if mode == "malformed" {
				m.contentLinks[0] = "mailto:bad%ZZ"
			}
			next, cmd := m.Update(readerOpenKey)
			m = next.(Model)
			bad := mode == "no accounts" || mode == "unusable default" || mode == "malformed"
			if bad {
				if m.overlay == overlayCompose || cmd == nil || !strings.Contains(m.statusMsg, "cannot compose") {
					t.Fatal("invalid compose did not report error")
				}
			} else if m.overlay != overlayCompose || m.compose.selectedAccount().ID != want {
				t.Fatal("default fallback failed")
			}
			if len(*requests) != 0 {
				t.Fatal("external handler launched")
			}
		})
	}
}

func TestReaderMailtoFocusedAndOrdinaryInteractions(t *testing.T) {
	for _, focus := range []bool{false, true} {
		t.Run(fmt.Sprint(focus), func(t *testing.T) {
			requests, apply := recordReaderActivations(t)
			m := mailtoTestModel(t, 1)
			m.cfg.Display.FocusLine = focus
			m.cfg.Display.MouseSupport = true
			m = apply(m, readerOpenKey)
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = apply(m, leftClick(m.feedsPaneWidth()+5, m.mainHeight()-2))
			m = apply(m, tea.MouseMsg{X: m.width - 2, Y: m.mainHeight() - 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
			if m.overlay == overlayCompose {
				t.Fatal("ordinary interaction composed")
			}
			m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
			m = apply(m, tea.MouseMsg{X: m.width - 2, Y: m.mainHeight() - 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
			m = apply(m, readerOpenKey)
			if m.overlay == overlayCompose {
				t.Fatal("wheel retained activation")
			}
			// Keyboard movement onto a visible mailto footer entry is deliberate
			// focus/selection, using the unchanged reader navigation path.
			if focus {
				row := m.contentLinkRows[0]
				m.contentFocusLine = row - 1
				m.viewport.SetYOffset(max(0, row-2))
				m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
				assertReaderLinkSelection(t, m, 0)
				if m.overlay == overlayCompose {
					t.Fatal("highlight alone composed")
				}
				m = apply(m, readerOpenKey)
				if m.overlay != overlayCompose || m.compose.selectedAccount().ID != "work" {
					t.Fatal("deliberate keyboard focus did not compose")
				}
			}

			if len(*requests) != 0 {
				t.Fatal("mailto launched browser")
			}
		})
	}
}

func TestReaderMailtoThreadSource(t *testing.T) {
	requests, apply := recordReaderActivations(t)
	m := mailtoTestModel(t, 1)
	first := m.messages[0]
	first.MessageID = "<first@example.com>"
	first.Date = time.Unix(1, 0)
	second := first
	second.ID = 2
	second.MessageID = "<second@example.com>"
	second.InReplyTo = first.MessageID
	second.Date = time.Unix(2, 0)
	second.BodyHTML = "<p>Later reply without link.</p>"
	for _, mb := range m.mailboxes {
		if mb.ID != first.MailboxID {
			second.MailboxID = mb.ID
			break
		}
	}
	m.cfg.Display.ThreadedConversations = true
	m.messages = []db.Message{first, second}
	m.filteredMessages = m.messages
	m.rebuildMessageThreads()
	m.setViewportForCurrentRow()
	if m.contentMessageID != 2 {
		t.Fatal("representative should be other account")
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m = apply(m, readerOpenKey)
	if m.overlay != overlayCompose || m.compose.selectedAccount().ID != "work" || len(*requests) != 0 {
		t.Fatal("thread used representative instead of link owner")
	}
}

func TestReaderMailtoCancelAndStaleSelection(t *testing.T) {
	requests, apply := recordReaderActivations(t)
	m := mailtoTestModel(t, 1)
	original := m.filteredMessages[0]
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m = apply(m, readerOpenKey)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.overlay != overlayDraftCloseConfirm {
		t.Fatal("normal draft close confirmation missing")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)
	if m.overlay != overlayNone || !reflect.DeepEqual(m.filteredMessages[0], original) {
		t.Fatal("cancel altered message")
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m.messages[0].ID = 3
	m.filteredMessages = m.messages
	m.setViewportForCurrentRow()
	m = apply(m, readerOpenKey)
	if m.overlay == overlayCompose || len(*requests) != 0 {
		t.Fatal("stale link composed")
	}
}
