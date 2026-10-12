package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

func TestFlatSidebarDoesNotInferParentsFromPunctuation(t *testing.T) {
	mbs := []db.Mailbox{{ID: 1, AccountID: 10, Name: "INBOX"}, {ID: 2, AccountID: 10, Name: "INBOX.Receipts"}, {ID: 3, AccountID: 10, Name: "Work/Projects"}}
	rows := buildSidebarRows([]db.Account{{ID: 10}}, mbs, nil, nil)
	seen := map[int64]bool{}
	for _, row := range rows {
		if row.kind != rowKindMailbox {
			continue
		}
		if row.depth != 0 || row.hasChildren {
			t.Fatalf("flat mailbox has tree relationship: %+v", row)
		}
		seen[row.mailboxID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("flat mailboxes missing from sidebar: %v", seen)
	}
}

func TestEmptyAccountsHintUsesUppercaseAccountManagerShortcut(t *testing.T) {
	for _, tt := range []struct {
		name  string
		model Model
	}{
		{
			name: "plain",
			model: Model{
				cfg:    config.DefaultConfig(),
				styles: Styles{PlainUI: true},
			},
		},
		{
			name: "icons",
			model: Model{
				cfg:    config.DefaultConfig(),
				styles: BuildStyles(CatppuccinMocha, "comfortable", "square"),
			},
		},
		{
			name: "no icons",
			model: Model{
				cfg: func() config.Config {
					cfg := config.DefaultConfig()
					cfg.Display.Icons = false
					return cfg
				}(),
				styles: BuildStyles(CatppuccinMocha, "comfortable", "square"),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.model.emptyAccountsHint()
			if !strings.Contains(got, "press M to add accounts") {
				t.Fatalf("expected uppercase account shortcut, got %q", got)
			}
			if strings.Contains(got, "press m to add accounts") {
				t.Fatalf("expected no lowercase account shortcut, got %q", got)
			}
		})
	}
}

func TestLooksLikeMissingCredential(t *testing.T) {
	yes := []string{
		"login: imap: NO Empty username or password. 006d6...",
		"oauth2: auth: no refresh token for Gmail",
	}
	no := []string{
		"login: imap: NO [AUTHENTICATIONFAILED] Invalid credentials",
		"dial tcp: connection refused",
		"",
	}
	for _, s := range yes {
		if !looksLikeMissingCredential(errString(s)) {
			t.Errorf("expected %q to look like a missing credential", s)
		}
	}
	for _, s := range no {
		if looksLikeMissingCredential(errString(s)) {
			t.Errorf("did not expect %q to look like a missing credential", s)
		}
	}
	if looksLikeMissingCredential(nil) {
		t.Error("nil error must be false")
	}
}

func TestAnyAccountNeedsKeychain(t *testing.T) {
	cases := []struct {
		name string
		accs []config.AccountConfig
		want bool
	}{
		{"all secrets in memory", []config.AccountConfig{
			{AuthMethod: config.AuthPassword, Password: "pw"},
			{AuthMethod: config.AuthOAuth2, RefreshToken: "tok"},
		}, false},
		{"password account missing its password", []config.AccountConfig{
			{AuthMethod: config.AuthPassword},
		}, true},
		{"oauth account missing its token", []config.AccountConfig{
			{AuthMethod: config.AuthOAuth2},
		}, true},
		{"legacy blank auth method, has password", []config.AccountConfig{
			{Password: "pw"},
		}, false},
	}
	for _, tc := range cases {
		m := Model{cfg: config.Config{Accounts: tc.accs}}
		if got := m.anyAccountNeedsKeychain(); got != tc.want {
			t.Errorf("%s: anyAccountNeedsKeychain = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestSidebarShowsGmailStandardFoldersBelowNoselectContainer(t *testing.T) {
	for _, namespace := range []string{"[Gmail]", "[GoogleMail]"} {
		t.Run(namespace, func(t *testing.T) {
			mailboxes := []db.Mailbox{{ID: 1, AccountID: 10, Name: "INBOX", Delimiter: "/"}, {ID: 2, AccountID: 10, Name: namespace, Delimiter: "/", Flags: []string{"\\Noselect", "\\HasChildren"}}}
			for i, name := range []string{"All Mail", "Drafts", "Important", "Sent Mail", "Spam", "Starred", "Trash"} {
				mailboxes = append(mailboxes, db.Mailbox{ID: int64(i + 3), AccountID: 10, Name: namespace + "/" + name, DisplayName: name, Delimiter: "/"})
			}
			rows := buildSidebarRows([]db.Account{{ID: 10}}, mailboxes, nil, nil)
			visible := map[int64]bool{}
			systemFolders := 0
			for _, row := range rows {
				if row.kind == rowKindSysFolderHeader {
					systemFolders = row.count
				}
				if row.kind == rowKindMailbox {
					visible[row.mailboxID] = true
				}
			}
			for _, id := range []int64{1, 3, 4, 5, 6, 7, 8, 9} {
				if !visible[id] {
					t.Errorf("standard folder %s missing", mailboxes[id-1].Name)
				}
			}
			if systemFolders != 7 {
				t.Errorf("System section contains %d folders, want 7", systemFolders)
			}
			model := Model{cfg: config.DefaultConfig(), styles: Styles{PlainUI: true}}
			for _, row := range rows {
				if row.kind != rowKindMailbox || row.mailboxID == 1 {
					continue
				}
				mailbox := mailboxes[row.mailboxID-1]
				rendered := model.renderSidebarMailboxRow(mailbox, row, false, 80)
				if strings.Contains(rendered, namespace) {
					t.Errorf("standard folder shows namespace prefix: %q", rendered)
				}
			}
			if visible[2] {
				t.Error("unselectable container must not be opened as a mailbox")
			}
		})
	}
}

func TestSidebarKeepsSelectableChildrenOfGenericNoselectParents(t *testing.T) {
	mailboxes := []db.Mailbox{{ID: 1, AccountID: 10, Name: "INBOX", Delimiter: "."}, {ID: 2, AccountID: 10, Name: "INBOX.Group", Delimiter: ".", Flags: []string{"\\Noselect"}}, {ID: 3, AccountID: 10, Name: "INBOX.Group.Receipts", Delimiter: "."}, {ID: 4, AccountID: 10, Name: "INBOX.Group.Unavailable", Delimiter: ".", Flags: []string{"\\Noselect"}}}
	rows := buildSidebarRows([]db.Account{{ID: 10}}, mailboxes, nil, nil)
	visible := map[int64]bool{}
	for _, row := range rows {
		if row.kind == rowKindMailbox {
			visible[row.mailboxID] = true
		}
	}
	if !visible[1] || !visible[3] || visible[2] || visible[4] {
		t.Fatalf("selectable children must remain accessible: %v", visible)
	}
}
