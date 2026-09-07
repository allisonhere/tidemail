package ui

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
)

func TestSaveAttachmentsPreservesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(original, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	// Also exercise a symlink collision: the target must remain untouched.
	if err := os.Symlink(original, filepath.Join(dir, "report (1).txt")); err != nil {
		t.Fatal(err)
	}
	result := saveAttachmentsCmdTo([]db.Attachment{
		{Filename: "report.txt", Data: []byte("first")},
		{Filename: "report.txt", Data: []byte("second")},
	}, dir)().(AttachmentsSavedMsg)
	if result.Err != nil || result.Count != 2 {
		t.Fatalf("save: %+v", result)
	}
	for name, want := range map[string]string{"report.txt": "original", "report (2).txt": "first", "report (3).txt": "second"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s: got %q, err %v", name, got, err)
		}
	}
}

func TestSaveAttachmentsDefaultDirectoryAvoidsOverwrite(t *testing.T) {
	name := fmt.Sprintf("review-%d.txt", time.Now().UnixNano())
	atts := []db.Attachment{{Filename: name, Data: []byte("original")}}
	result := saveAttachmentsCmd(atts)().(AttachmentsSavedMsg)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	atts[0].Data = []byte("second")
	if got := saveAttachmentsCmd(atts)().(AttachmentsSavedMsg); got.Err != nil {
		t.Fatal(got.Err)
	}
	original, err := os.ReadFile(filepath.Join(result.Path, name))
	if err != nil || string(original) != "original" {
		t.Fatalf("original overwritten: %q %v", original, err)
	}
}

func TestRecipientListQuotedNames(t *testing.T) {
	input := `"Doe, Jane" <jane@example.com>, Bob <bob@example.com>`
	if err := validateAddressList(input); err != "" {
		t.Fatal(err)
	}
	got := parseAddressList(input)
	if strings.Join(got, ",") != "jane@example.com,bob@example.com" {
		t.Fatalf("recipients: %v", got)
	}
	for _, input := range []string{",,,", `"Doe, Jane <jane@example.com>`, "jane@example.com, invalid"} {
		if validateAddressList(input) == "" {
			t.Errorf("accepted invalid list %q", input)
		}
	}
}

func TestImportedDraftPreservesAttachmentsThroughCompose(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	name := t.Name()
	aid, err := database.AddAccount(name, "")
	if err != nil {
		t.Fatal(err)
	}
	mid, err := database.UpsertMailbox(db.Mailbox{AccountID: aid, Name: "Drafts"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(db.Message{MailboxID: mid, UID: 123, MessageID: "<attachment-review@x>", To: "bob@example.com", BodyText: "See attached"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := database.ListMessages(mid)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages: %v %v", msgs, err)
	}
	if _, err := database.SaveAttachment(msgs[0].ID, db.Attachment{Filename: "report.txt", ContentType: "text/plain", Data: []byte("report")}); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: name, User: "alice@example.com"}}
	m := NewModel(database, cfg, "dev", false)
	m.accounts = []db.Account{{ID: aid, Name: name}}
	m.mailboxes = []db.Mailbox{{ID: mid, AccountID: aid, Name: "Drafts"}}
	for i := 0; i < 2; i++ {
		result := m.importRemoteDraftsCmd(mid)().(DraftsLoadedMsg)
		if result.Err != nil || len(result.Drafts) != 1 {
			t.Fatalf("draft import: %+v", result)
		}
		draft := result.Drafts[0]
		if len(draft.Attachments) != 1 || string(draft.Attachments[0].Data) != "report" || draft.Attachments[0].ContentType != "text/plain" {
			t.Fatalf("attachments: %+v", draft.Attachments)
		}
		c := NewComposeFromDraft(draft, cfg.Accounts, nil)
		_, cmd, _ := c.send()
		if cmd == nil {
			t.Fatal("send unavailable")
		}
		queued := cmd().(SendQueuedMsg)
		if len(queued.Msg.Attachments) != 1 || string(queued.Msg.Attachments[0].Data) != "report" {
			t.Fatalf("outgoing attachments: %+v", queued.Msg.Attachments)
		}
	}
}

// A local SMTP peer pauses at DATA so shutdown is exercised during a real send.
func sendRegressionServer(t *testing.T, reject bool) (config.AccountConfig, <-chan struct{}, chan<- struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	_, portString, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	entered, release := make(chan struct{}), make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprint(conn, "220 localhost ESMTP\r\n")
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				fmt.Fprint(conn, "250-localhost\r\n250 AUTH PLAIN\r\n")
			case strings.HasPrefix(line, "AUTH"):
				fmt.Fprint(conn, "235 authenticated\r\n")
			case strings.HasPrefix(line, "DATA"):
				close(entered)
				<-release
				if reject {
					fmt.Fprint(conn, "550 rejected\r\n")
					continue
				}
				fmt.Fprint(conn, "354 send data\r\n")
				for {
					line, err = reader.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
				}
				fmt.Fprint(conn, "250 accepted\r\n")
			default:
				fmt.Fprint(conn, "250 OK\r\n")
			}
		}
	}()
	return config.AccountConfig{SMTPHost: "localhost", SMTPPort: port, User: "alice@example.com", Password: "test"}, entered, release
}

func TestShutdownCompletesSendAndDraftCleanup(t *testing.T) {
	for _, delay := range []int{0, 5} {
		for _, started := range []bool{false, true} {
			for _, reject := range []bool{false, true} {
				t.Run(fmt.Sprintf("delay=%d/started=%v/reject=%v", delay, started, reject), func(t *testing.T) {
					m := newSendTestModel(t, delay)
					account, entered, release := sendRegressionServer(t, reject)
					draftID, err := m.db.SaveDraft(db.Draft{AccountName: t.Name(), Subject: "shutdown", BodyText: "body"})
					if err != nil {
						t.Fatal(err)
					}
					m.compose = NewCompose(account, nil, nil)
					m.compose.draftID = draftID
					m, cmd := m.handleSendQueued(SendQueuedMsg{Account: account, Msg: smtp.OutgoingMessage{To: []string{"bob@example.com"}, Body: "body"}})
					if started {
						if delay > 0 {
							cmd = m.commitPendingSend(m.pendingSends[0].ID)
						}
						go cmd()
					}
					flushed := make(chan error, 1)
					go func() { flushed <- m.FlushPendingSends() }()
					select {
					case <-entered:
					case <-time.After(3 * time.Second):
						t.Fatal("send never reached DATA")
					}
					// Both started and not-yet-scheduled sends must be awaited by shutdown.
					select {
					case err := <-flushed:
						t.Fatalf("shutdown finished before delivery: %v", err)
					default:
					}
					close(release)
					select {
					case err := <-flushed:
						if (err != nil) != reject {
							t.Fatalf("flush error: %v", err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("shutdown did not finish")
					}
					_, err = m.db.GetDraft(draftID)
					if reject && err != nil {
						t.Fatalf("failed send lost draft: %v", err)
					}
					if !reject && !errors.Is(err, db.ErrDraftNotFound) {
						t.Fatalf("sent draft remains: %v", err)
					}
					// A delayed command arriving after shutdown must reuse the completed result.
					if err := m.FlushPendingSends(); (err != nil) != reject {
						t.Fatalf("second flush: %v", err)
					}
				})
			}
		}
	}
}
