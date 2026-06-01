package db

import (
	"bytes"
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
