package cli

import (
	"bytes"
	"flag"
	"reflect"
	"strings"
	"testing"
)

func TestReadFlagsInterspersedAndLiteral(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"query", "--mailbox", "Course Notes", "--limit=3"}, []string{"query"}},
		{[]string{"--mailbox=Course Notes", "query", "-limit", "3"}, []string{"query"}},
		{[]string{"--limit", "3", "--mailbox", "Course Notes", "--", "-query"}, []string{"-query"}},
	} {
		fs := flag.NewFlagSet("messages search", flag.ContinueOnError)
		mailbox := fs.String("mailbox", "", "")
		limit := fs.Int("limit", 50, "")
		got, err := parseReadFlags(fs, tc.args)
		if err != nil || !reflect.DeepEqual(got, tc.want) || *mailbox != "Course Notes" || *limit != 3 {
			t.Fatalf("%v: got %v, mailbox %q, limit %d, error %v", tc.args, got, *mailbox, *limit, err)
		}
	}
}

func TestMailReadsRejectInapplicableOptions(t *testing.T) {
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{"accounts", []string{"list", "--unread", "--json"}},
		{"outbox", []string{"list", "--unread", "--json"}},
		{"messages", []string{"read", "cache:1", "--limit", "1", "--json"}},
		{"outbox", []string{"status", "1", "--offset", "1", "--json"}},
		{"messages", []string{"search", "invoice", "--limit", "--json"}},
	} {
		var out, diagnostics bytes.Buffer
		code := RunMailRead(tc.command, tc.args, &out, &diagnostics)
		if code != 2 || !strings.Contains(out.String(), "invalid_arguments") {
			t.Fatalf("%s %v: %d %s %s", tc.command, tc.args, code, out.String(), diagnostics.String())
		}
	}
}

func TestMailReadFlagsLiteralQuery(t *testing.T) {
	mailReadFixture(t)
	var out, diagnostics bytes.Buffer
	code := RunMailRead("messages", []string{"search", "--json", "--", "-invoice"}, &out, &diagnostics)
	if code != 0 || !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("%d %s %s", code, out.String(), diagnostics.String())
	}
}

func TestMailReadFilterValidation(t *testing.T) {
	for _, args := range [][]string{
		{"list", "--since", "2026-99-01", "--json"},
		{"list", "--since", "2026-11-20", "--before", "2026-11-19", "--json"},
		{"search", "invoice", "--sort", "random", "--json"},
		{"read", "1", "--starred", "--json"},
	} {
		var out, diag bytes.Buffer
		if code := RunMailRead("messages", args, &out, &diag); code != 2 || !strings.Contains(out.String(), "invalid_arguments") {
			t.Fatalf("%v: %d %s %s", args, code, out.String(), diag.String())
		}
	}
}

func TestBrowserPassesOnlyReadFlags(t *testing.T) {
	got := readFlags([]string{"--unread", "--mailbox", "INBOX", "--agent", "--since=2026-01-01", "-agent=false", "--limit", "5"})
	if strings.Join(got, " ") != "--agent -agent=false" {
		t.Fatalf("readFlags = %q", got)
	}
}
