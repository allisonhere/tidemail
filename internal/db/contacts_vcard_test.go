package db

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVCardRoundTrip(t *testing.T) {
	d := newTestDB(t)
	_, _ = d.AddContact("amy@example.com", "Amy Smith", "manual")
	_, _ = d.AddContact("bob@example.com", "", "manual")

	var buf bytes.Buffer
	if err := d.ExportVCard(&buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(buf.String(), "amy@example.com") || !strings.Contains(buf.String(), "Amy Smith") {
		t.Fatalf("export missing data:\n%s", buf.String())
	}

	d2 := newTestDB(t)
	n, err := d2.ImportVCard(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 imported, got %d", n)
	}
	got, _ := d2.ListContacts()
	if len(got) != 2 {
		t.Fatalf("expected 2 contacts after import, got %+v", got)
	}
}

func TestImportVCardMultipleEmails(t *testing.T) {
	const card = "BEGIN:VCARD\r\n" +
		"VERSION:4.0\r\n" +
		"FN:Multi Person\r\n" +
		"EMAIL:one@example.com\r\n" +
		"EMAIL:two@example.com\r\n" +
		"END:VCARD\r\n"
	d := newTestDB(t)
	n, err := d.ImportVCard(strings.NewReader(card))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected both emails imported, got %d", n)
	}
	got, _ := d.ListContacts()
	if len(got) != 2 {
		t.Fatalf("expected 2 contacts, got %+v", got)
	}
	for _, c := range got {
		if c.DisplayName != "Multi Person" || c.Source != "vcard" {
			t.Fatalf("expected shared vcard name/source, got %+v", c)
		}
	}
}

func TestImportVCardStructuredNameFallback(t *testing.T) {
	const card = "BEGIN:VCARD\r\n" +
		"VERSION:4.0\r\n" +
		"N:Doe;Jane;;;\r\n" +
		"EMAIL:jane@example.com\r\n" +
		"END:VCARD\r\n"
	d := newTestDB(t)
	if _, err := d.ImportVCard(strings.NewReader(card)); err != nil {
		t.Fatalf("import: %v", err)
	}
	got, _ := d.ListContacts()
	if len(got) != 1 || got[0].DisplayName != "Jane Doe" {
		t.Fatalf("expected structured name fallback, got %+v", got)
	}
}

func TestImportVCardMetadata(t *testing.T) {
	const card = "BEGIN:VCARD\r\n" +
		"VERSION:4.0\r\n" +
		"FN:Meta Person\r\n" +
		"EMAIL:meta@example.com\r\n" +
		"TEL;TYPE=cell:555-0100\r\n" +
		"ORG:Example Co\r\n" +
		"TITLE:Engineer\r\n" +
		"NOTE:Met at conference\r\n" +
		"END:VCARD\r\n"
	d := newTestDB(t)
	if _, err := d.ImportVCard(strings.NewReader(card)); err != nil {
		t.Fatalf("import: %v", err)
	}
	got, _ := d.ListContacts()
	if len(got) != 1 {
		t.Fatalf("expected 1 contact, got %+v", got)
	}
	c := got[0]
	if c.Phone != "555-0100" || c.Organization != "Example Co" || c.Title != "Engineer" || c.Note != "Met at conference" {
		t.Fatalf("metadata not imported: %+v", c)
	}
}

func TestExportVCardMetadata(t *testing.T) {
	d := newTestDB(t)
	_, err := d.AddContactWithMetadata("meta@example.com", "Meta Person", "manual", ContactMetadata{
		Phone:        "555-0100",
		Organization: "Example Co",
		Title:        "Engineer",
		Note:         "Met at conference",
	})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := d.ExportVCard(&buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"TEL", "555-0100", "ORG", "Example Co", "TITLE:Engineer", "NOTE:Met at conference"} {
		if !strings.Contains(out, want) {
			t.Fatalf("export missing %q:\n%s", want, out)
		}
	}
}

func TestImportVCardMalformed(t *testing.T) {
	d := newTestDB(t)
	_, err := d.ImportVCard(strings.NewReader("BEGIN:VCARD\r\nFN:Broken\r\n"))
	if err == nil {
		t.Fatal("expected error for malformed vCard")
	}
}

// A card with an email but no name must not erase the name already saved for
// that address.
func TestImportVCardWithoutNameKeepsSavedName(t *testing.T) {
	const card = "BEGIN:VCARD\r\n" +
		"VERSION:4.0\r\n" +
		"EMAIL:ann@example.com\r\n" +
		"END:VCARD\r\n"
	d := newTestDB(t)
	id, _ := d.AddContact("ann@example.com", "Ann Lee", "manual")
	if _, err := d.ImportVCard(strings.NewReader(card)); err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := contactName(t, d, id); got != "Ann Lee" {
		t.Fatalf("nameless vCard erased the saved name: got %q", got)
	}
}

// A card with no EMAIL line cannot be saved. It must be counted, not dropped
// without a word. These are two cards from a real Radicale address book that
// look alike but differ in exactly this.
func TestImportVCardCountsCardsWithoutEmail(t *testing.T) {
	const cards = "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Tom Kettle\r\nN:Kettle;Tom;;;\r\nEMAIL;TYPE=home:tkettle@mail.com\r\nEND:VCARD\r\n" +
		"BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Joe & Jane Binger\r\nN:Binger;Joe & Jane;;;\r\nADR;TYPE=home:;;;Bradenton;FL;;\r\nEND:VCARD\r\n"
	d := newTestDB(t)
	res, err := d.ImportVCardCounts(strings.NewReader(cards))
	if err != nil {
		t.Fatal(err)
	}
	if res.Cards != 2 || res.Added != 1 || res.NoEmail != 1 {
		t.Fatalf("result = %+v, want 2 cards, 1 added, 1 without email", res)
	}
}

func TestImportVCardDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	card := func(fn, email string) string {
		s := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:" + fn + "\r\n"
		if email != "" {
			s += "EMAIL:" + email + "\r\n"
		}
		return s + "END:VCARD\r\n"
	}
	write("a.vcf", card("Ann", "ann@example.com"))
	write("B.VCF", card("Bob", "bob@example.com"))
	write("nomail.vcf", card("Nobody", ""))
	write("broken.vcf", "BEGIN:VCARD\r\nFN:Broken\r\n")
	write("notes.txt", card("Ignored", "ignored@example.com"))
	if err := os.Mkdir(filepath.Join(dir, "sub.vcf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deeper", "c.vcf"), []byte(card("Cy", "cy@example.com")), 0o644); err != nil {
		t.Fatal(err)
	}

	d := newTestDB(t)
	res, err := d.ImportVCardDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 4 || res.Added != 2 || res.NoEmail != 1 || res.Failed != 1 {
		t.Fatalf("result = %+v, want 4 files, 2 added, 1 without email, 1 failed", res)
	}
	if !strings.HasPrefix(res.FirstFail, "broken.vcf:") {
		t.Fatalf("FirstFail = %q", res.FirstFail)
	}
	got, _ := d.ListContacts()
	if len(got) != 2 {
		t.Fatalf("saved %d contacts, want 2 (subfolders and non-.vcf files are not read)", len(got))
	}
}
