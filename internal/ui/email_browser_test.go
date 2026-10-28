package ui

import (
	"bytes"
	"errors"
	"golang.org/x/net/html/charset"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func TestOpenEmailInBrowser(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	originalFetch, originalOpen := fetchEmailSource, openEmailBrowser
	t.Cleanup(func() { fetchEmailSource, openEmailBrowser = originalFetch, originalOpen })
	html := "<html><a href=\"https://example.com/?a=1&amp;b=2\">Go</a>\r\n</html>"
	raw := []byte("MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nplain\r\n--x\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + html + "\r\n--x--\r\n")
	for _, content := range []bool{false, true} {
		m := sourceModel(t)
		if content {
			m.focused = paneContent
			m.contentMessageID = m.filteredMessages[1].ID
		}
		wantID := m.commandMessage().ID
		fetched, opened := false, false
		var launchedPath string
		fetchEmailSource = func(_ Model, msg db.Message) func() ([]byte, error) {
			if msg.ID != wantID {
				t.Fatalf("message = %d, want %d", msg.ID, wantID)
			}
			return func() ([]byte, error) { fetched = true; return raw, nil }
		}
		openEmailBrowser = func(_, path string) error {
			opened = true
			launchedPath = path
			if !strings.HasSuffix(path, ".html") {
				t.Fatalf("browser target: %q", path)
			}
			data, err := os.ReadFile(path)
			if err != nil || !strings.HasPrefix(string(data), "\ufeff") ||
				!strings.Contains(string(data), `<a href="https://example.com/?a=1&amp;b=2">Go</a>`) ||
				!strings.Contains(string(data), "Content-Security-Policy") {
				t.Fatalf("HTML = %q, err %v", data, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("permissions = %v", info.Mode())
			}
			return nil
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
		if cmd == nil || fetched || opened {
			t.Fatal("browser work must be deferred to a command")
		}
		result := cmd()
		if !fetched || !opened {
			t.Fatal("command did not fetch and open")
		}
		updated, _ := next.(Model).Update(result)
		if updated.(Model).statusErr {
			t.Fatal(updated.(Model).statusMsg)
		}
		if _, err := os.Stat(launchedPath); err != nil {
			t.Fatalf("launched file did not survive command completion: %v", err)
		}
	}
}

func TestOpenEmailBrowserFailures(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	original := openEmailBrowser
	t.Cleanup(func() { openEmailBrowser = original })
	calls := 0
	openEmailBrowser = func(_, _ string) error { calls++; return errors.New("launcher failed") }
	for _, tc := range []struct{ raw, want string }{
		{"Content-Type: text/plain\r\n\r\nhello", "no HTML body"},
		{"Content-Type: text/html\r\n\r\n<a href='https://example.com'>hi</a>", "launcher failed"},
	} {
		_, err := openEmailHTML([]byte(tc.raw), "", false)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("error = %v", err)
		}
		m, _ := sourceModel(t).Update(emailBrowserOpenedMsg{err: err})
		if !m.(Model).statusErr || !strings.Contains(m.(Model).statusMsg, tc.want) {
			t.Fatal("failure not reported")
		}
		exportDir, _ := emailExportDir()
		files, _ := os.ReadDir(exportDir)
		if len(files) != 0 {
			t.Fatal("failed export leaked files")
		}
	}
	if calls != 1 {
		t.Fatalf("browser calls = %d", calls)
	}
}

func TestOpenEmailBrowserFetchFailure(t *testing.T) {
	original := fetchEmailSource
	t.Cleanup(func() { fetchEmailSource = original })
	fetchEmailSource = func(Model, db.Message) func() ([]byte, error) {
		return func() ([]byte, error) { return nil, errors.New("synthetic fetch failure") }
	}
	next, cmd := sourceModel(t).Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	result := cmd()
	updated, _ := next.(Model).Update(result)
	if !updated.(Model).statusErr || !strings.Contains(updated.(Model).statusMsg, "synthetic fetch failure") {
		t.Fatal("fetch error not reported")
	}
	m, _ := newMailboxListModel(7, 1)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !next.(Model).statusErr {
		t.Fatal("missing server not reported")
	}
}

func TestOpenEmailBrowserTempFailure(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir()+"/missing")
	_, err := openEmailHTML([]byte("Content-Type: text/html\r\n\r\n<p>hello</p>"), "", false)
	if err == nil || !strings.Contains(err.Error(), "export directory") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenEmailBrowserEncoding(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	original := openEmailBrowser
	t.Cleanup(func() { openEmailBrowser = original })
	for _, tc := range []struct{ name, charset, body, want string }{
		{"UTF-8 without declaration", "utf-8", "<p>café 日本語 <a href='https://example.com/?q=café'>link</a></p>", "<p>café 日本語 <a href='https://example.com/?q=café'>link</a></p>"},
		{"ISO-8859-1 with declaration", "ISO-8859-1", "<html><head><meta charset=\"ISO-8859-1\"></head><body>caf\xe9 <a href='https://example.com'>link</a></body></html>", "<html><head><meta charset=\"ISO-8859-1\"></head><body>café <a href='https://example.com'>link</a></body></html>"},
		{"existing UTF-8 BOM", "utf-8", "\ufeff<p>café</p>", "<p>café</p>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			openEmailBrowser = func(_, target string) error { path = target; return nil }
			raw := "Content-Type: text/html; charset=" + tc.charset + "\r\n\r\n" + tc.body
			if _, err := openEmailHTML([]byte(raw), "", false); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// A BOM has precedence over HTML charset declarations, and the text
			// after it must be the decoded UTF-8 body.
			if !strings.HasPrefix(string(data), "\ufeff") || strings.Count(string(data), "\ufeff") != 1 {
				t.Fatalf("export = %q, want exactly one leading BOM", data)
			}
			decoded, err := charset.NewReader(bytes.NewReader(data), "text/html")
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := io.ReadAll(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(rendered), "café") {
				t.Fatalf("browser charset detection corrupted text: %q", rendered)
			}
		})
	}
}

func TestOpenEmailBrowserReopenWithImages(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	originalFetch, originalOpen := fetchEmailSource, openEmailBrowser
	t.Cleanup(func() { fetchEmailSource, openEmailBrowser = originalFetch, originalOpen })
	raw := []byte("Content-Type: text/html\r\n\r\n<p>hi</p><img src=\"https://img.example/p.gif\">")
	fetchEmailSource = func(Model, db.Message) func() ([]byte, error) {
		return func() ([]byte, error) { return raw, nil }
	}
	var exports []string
	openEmailBrowser = func(_, path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		exports = append(exports, string(data))
		return nil
	}
	press := func(m Model, key tea.KeyMsg) Model {
		next, cmd := m.Update(key)
		m = next.(Model)
		if cmd != nil {
			if res, ok := cmd().(emailBrowserOpenedMsg); ok {
				next, _ = m.Update(res)
				m = next.(Model)
			}
		}
		return m
	}
	m := sourceModel(t)
	m = press(m, tea.KeyMsg{Type: tea.KeyCtrlB})
	if m.browserReopen == 0 || !strings.Contains(m.statusMsg, "1 remote images blocked") {
		t.Fatalf("offer not armed: reopen=%d status=%q", m.browserReopen, m.statusMsg)
	}
	if strings.Contains(exports[0], "img.example") {
		t.Fatal("first export must block the image")
	}
	// Any other key withdraws the offer.
	other := press(m, tea.KeyMsg{Type: tea.KeyDown})
	if other.browserReopen != 0 {
		t.Fatal("another key must disarm the offer")
	}
	// Ctrl+B again reopens with images and does not re-arm.
	m = press(m, tea.KeyMsg{Type: tea.KeyCtrlB})
	if len(exports) != 2 || !strings.Contains(exports[1], "https://img.example/p.gif") {
		t.Fatalf("second export did not load images: %d exports", len(exports))
	}
	if m.browserReopen != 0 || !strings.Contains(m.statusMsg, "images loaded") {
		t.Fatalf("reopen=%d status=%q", m.browserReopen, m.statusMsg)
	}
	// Without blocked images there is nothing to offer.
	raw = []byte("Content-Type: text/html\r\n\r\n<p>hi</p>")
	m = press(sourceModel(t), tea.KeyMsg{Type: tea.KeyCtrlB})
	if m.browserReopen != 0 {
		t.Fatal("no offer when nothing was blocked")
	}
	// The offer lapses when the status clears.
	m.browserReopen = 5
	next, _ := m.Update(StatusClearMsg{})
	if next.(Model).browserReopen != 0 {
		t.Fatal("status clear must disarm the offer")
	}
}
