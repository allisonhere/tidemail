package mailtemplate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func templateNames(ts []Template) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

func find(ts []Template, name string) (Template, bool) {
	for _, t := range ts {
		if t.Name == name {
			return t, true
		}
	}
	return Template{}, false
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Invoice.md", "---\nsubject: Your invoice\n---\nHi {first_name}\n")
	write("plain.txt", "just text")
	write("Thanks.txt", "my own thanks")
	write(".hidden.md", "no")
	write("notes.pdf", "no")
	write("big.md", strings.Repeat("x", MaxBytes+1))
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "escape.md")); err != nil {
		t.Fatal(err)
	}

	got := Load(dir)
	inv, ok := find(got, "Invoice")
	if !ok || inv.Subject != "Your invoice" || inv.Body != "Hi {first_name}\n" {
		t.Errorf("Invoice = %+v", inv)
	}
	if p, ok := find(got, "plain"); !ok || p.Body != "just text" || p.Subject != "" {
		t.Errorf("plain = %+v", p)
	}
	if th, _ := find(got, "Thanks"); th.Body != "my own thanks" {
		t.Errorf("a file should replace the built-in of the same name, got %q", th.Body)
	}
	for _, bad := range []string{".hidden", "notes", "big", "escape", "secret"} {
		if _, ok := find(got, bad); ok {
			t.Errorf("%q should have been skipped: %v", bad, templateNames(got))
		}
	}
	if len(got) != len(Builtins)+2 { // Invoice and plain added, Thanks replaced
		t.Errorf("got %d templates: %v", len(got), templateNames(got))
	}
}

func TestLoadMissingDir(t *testing.T) {
	got := Load(filepath.Join(t.TempDir(), "nope"))
	if len(got) != len(Builtins) {
		t.Errorf("a missing folder should give just the built-ins, got %v", templateNames(got))
	}
}

func TestParse(t *testing.T) {
	for in, want := range map[string]Template{
		"body only":                        {Body: "body only"},
		"---\nsubject: Hi\n---\nbody":      {Subject: "Hi", Body: "body"},
		"---\nsubject: Hi\r\n---\r\nbody":  {Subject: "Hi", Body: "body"},
		"---\nother: x\n---\nbody":         {Body: "body"},
		"---\nsubject: unterminated\nbody": {Body: "---\nsubject: unterminated\nbody"},
		"---\nsubject: Only\n---":          {Subject: "Only"},
	} {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %+v; want %+v", in, got, want)
		}
	}
}

func TestFill(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	v := Vars(`"Priya Natarajan" <p@acme.com>`, "Allie", now)
	got := Fill("Hi {first_name} ({name}), {date}, {my_name} {unknown} {Not}", v)
	want := "Hi Priya (Priya Natarajan), October 18, 2026, Allie {unknown} {Not}"
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
	if v := Vars("p@acme.com", "A", now); v["first_name"] != "there" || v["name"] != "" {
		t.Errorf("without a display name first_name should be 'there': %v", v)
	}
	if v := Vars("", "A", now); v["first_name"] != "there" {
		t.Errorf("empty To: %v", v)
	}
}

func TestSerializeParseRoundTrip(t *testing.T) {
	for _, in := range []Template{{Body: "hi\n"}, {Subject: "Re: x", Body: "hi {name}\n"}} {
		if got := Parse(Serialize(in)); got != in {
			t.Errorf("round trip %+v -> %+v", in, got)
		}
	}
}
