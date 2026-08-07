package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

func TestListUnsubscribeTarget(t *testing.T) {
	cases := []struct {
		name    string
		headers string
		want    string
	}{
		{
			name:    "prefers https over mailto",
			headers: "List-Unsubscribe\n<mailto:leave@list.example.com>, <https://list.example.com/leave?u=1>\n",
			want:    "https://list.example.com/leave?u=1",
		},
		{
			name:    "falls back to mailto",
			headers: "Return-Path\n<news@list.example.com>\nList-Unsubscribe\n<mailto:leave@list.example.com>\n",
			want:    "mailto:leave@list.example.com",
		},
		{
			name:    "absent header",
			headers: "Return-Path\n<news@list.example.com>\n",
			want:    "",
		},
		{
			name:    "empty headers",
			headers: "",
			want:    "",
		},
	}
	for _, tc := range cases {
		if got := listUnsubscribeTarget(tc.headers); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMessageUnsubscribeTargetFallsBackToLabelledBodyLink(t *testing.T) {
	cases := []struct {
		name string
		msg  db.Message
		want string
	}{
		{
			name: "html text label",
			msg:  db.Message{BodyHTML: `<p><a href="https://list.example.com/leave?id=1">Unsubscribe</a></p>`},
			want: "https://list.example.com/leave?id=1",
		},
		{
			name: "html accessibility label",
			msg:  db.Message{BodyHTML: `<a href="mailto:leave@list.example.com" aria-label="Unsubscribe from updates">Manage</a>`},
			want: "mailto:leave@list.example.com",
		},
		{
			name: "plain text nearby URL",
			msg:  db.Message{BodyText: "To unsubscribe, visit https://list.example.com/leave?id=2 today."},
			want: "https://list.example.com/leave?id=2",
		},
		{
			name: "header wins over body",
			msg: db.Message{
				Headers:  "List-Unsubscribe\n<https://header.example.com/leave>\n",
				BodyHTML: `<a href="https://body.example.com/leave">Unsubscribe</a>`,
			},
			want: "https://header.example.com/leave",
		},
		{
			name: "unrelated body link is ignored",
			msg:  db.Message{BodyHTML: `<p>Unsubscribe in account settings.</p><a href="https://example.com/article">Read more</a>`},
			want: "",
		},
		{
			name: "unsafe labelled link is ignored",
			msg:  db.Message{BodyHTML: `<a href="javascript:alert(1)">Unsubscribe</a>`},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := messageUnsubscribeTarget(tc.msg); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCtrlUDispatchesUnsubscribeOnlyFromContentPane(t *testing.T) {
	msg := db.Message{
		ID:       42,
		BodyHTML: `<a href="https://list.example.com/leave">Unsubscribe</a>`,
	}
	m := Model{
		keys:               DefaultKeys,
		focused:            paneContent,
		contentMessageID:   msg.ID,
		filteredMessages:   []db.Message{msg},
		selectedMessages:   make(map[int64]bool),
		pendingUnsubscribe: db.Message{},
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(Model)
	if m.overlay != overlayUnsubscribeConfirm {
		t.Fatal("expected ctrl+u in content to open unsubscribe confirmation")
	}
	if cmd != nil {
		t.Fatal("expected no unsubscribe action before confirmation")
	}

	m.overlay = overlayNone
	m.pendingUnsubscribe = db.Message{}
	m.focused = paneMessages
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(Model)
	if m.overlay != overlayNone || cmd != nil {
		t.Fatal("expected ctrl+u outside content to do nothing")
	}
}

func TestCtrlUReportsWhenMessageHasNoUnsubscribeTarget(t *testing.T) {
	msg := db.Message{ID: 42, BodyHTML: `<a href="https://example.com/article">Read more</a>`}
	m := Model{
		keys:             DefaultKeys,
		focused:          paneContent,
		contentMessageID: msg.ID,
		filteredMessages: []db.Message{msg},
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(Model)
	if m.statusMsg != "no unsubscribe link in this message" {
		t.Fatalf("unexpected status %q", m.statusMsg)
	}
	if cmd == nil {
		t.Fatal("expected status cleanup command")
	}
}

func TestParseMailtoURI(t *testing.T) {
	addr, subject := parseMailtoURI("mailto:leave@list.example.com?subject=stop%20mail")
	if addr != "leave@list.example.com" || subject != "stop mail" {
		t.Fatalf("got %q / %q", addr, subject)
	}
	addr, subject = parseMailtoURI("mailto:leave@list.example.com")
	if addr != "leave@list.example.com" || subject != "unsubscribe" {
		t.Fatalf("expected default subject, got %q / %q", addr, subject)
	}
}

func TestSupportsOneClickUnsubscribe(t *testing.T) {
	yes := "List-Unsubscribe\n<https://x.example/u>\nList-Unsubscribe-Post\nList-Unsubscribe=One-Click\n"
	if !supportsOneClickUnsubscribe(yes) {
		t.Fatal("expected one-click support to be detected")
	}
	no := "List-Unsubscribe\n<https://x.example/u>\n"
	if supportsOneClickUnsubscribe(no) {
		t.Fatal("expected no one-click support without the -Post header")
	}
}

func TestOneClickUnsubscribeCmdPostsHandshake(t *testing.T) {
	var gotBody, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotType = r.Header.Get("Content-Type")
	}))
	defer srv.Close()

	msg := oneClickUnsubscribeCmd(srv.URL)()
	res, ok := msg.(UnsubscribeResultMsg)
	if !ok {
		t.Fatalf("expected UnsubscribeResultMsg, got %T", msg)
	}
	if res.Err != nil {
		t.Fatalf("expected success, got %v", res.Err)
	}
	if gotBody != "List-Unsubscribe=One-Click" || gotType != "application/x-www-form-urlencoded" {
		t.Fatalf("unexpected POST: body=%q type=%q", gotBody, gotType)
	}
}

func TestOneClickUnsubscribeCmdReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := oneClickUnsubscribeCmd(srv.URL)().(UnsubscribeResultMsg)
	if res.Err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if res.FallbackURL != srv.URL {
		t.Fatalf("expected fallback URL %q, got %q", srv.URL, res.FallbackURL)
	}
}

func TestUnsubscribeURLAsksForConfirmation(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)

	cur := db.Message{
		MailboxID: 1,
		Headers:   "List-Unsubscribe\n<https://list.example.com/leave>\n",
	}
	n, cmd := m.handleUnsubscribe(cur)
	m = n.(Model)

	if m.overlay != overlayUnsubscribeConfirm {
		t.Fatal("expected a confirmation overlay before opening the browser")
	}
	if cmd != nil {
		t.Fatal("nothing should run before the user confirms")
	}

	// esc cancels without acting
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.overlay != overlayNone {
		t.Fatal("expected esc to dismiss the confirmation")
	}
	if m.pendingUnsubscribe.Headers != "" {
		t.Fatal("expected the pending unsubscribe to be cleared on cancel")
	}

	// y confirms and dispatches the action
	n, _ = m.handleUnsubscribe(cur)
	m = n.(Model)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = next.(Model)
	if m.overlay != overlayNone {
		t.Fatal("expected the overlay to close on confirm")
	}
	if cmd == nil {
		t.Fatal("expected the confirmed unsubscribe to dispatch a command")
	}
}

func TestUnsubscribeMailtoOpensPrefilledCompose(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)

	cur := db.Message{
		MailboxID: 1,
		Headers:   "List-Unsubscribe\n<mailto:leave@list.example.com?subject=stop>\n",
	}
	n, _ := m.handleUnsubscribe(cur)
	m = n.(Model)

	if m.overlay != overlayCompose {
		t.Fatal("expected a compose overlay for a mailto unsubscribe")
	}
	if got := m.compose.toInput.Value(); got != "leave@list.example.com" {
		t.Fatalf("expected prefilled recipient, got %q", got)
	}
	if got := m.compose.subjectInput.Value(); got != "stop" {
		t.Fatalf("expected prefilled subject, got %q", got)
	}
}
