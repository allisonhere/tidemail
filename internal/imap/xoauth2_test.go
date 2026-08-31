package imap

import (
	"strings"
	"testing"
)

func TestXOAUTH2ClientStart(t *testing.T) {
	c := &xoauth2Client{user: "alice@example.com", token: "ya29.token"}
	mech, ir, err := c.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if mech != "XOAUTH2" {
		t.Fatalf("mechanism = %q, want XOAUTH2", mech)
	}
	want := "user=alice@example.com\x01auth=Bearer ya29.token\x01\x01"
	if string(ir) != want {
		t.Fatalf("initial response = %q, want %q", ir, want)
	}
}

func TestXOAUTH2ClientNextSurfacesChallenge(t *testing.T) {
	c := &xoauth2Client{user: "a", token: "b"}
	_, err := c.Next([]byte(`{"status":"400"}`))
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("Next should surface the server challenge, got %v", err)
	}
}
