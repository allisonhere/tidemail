package imap

import (
	"strings"
	"testing"
)

func TestXOAuth2ClientStart(t *testing.T) {
	c := &xoauth2Client{user: "me@outlook.com", token: "tok-123"}
	mech, ir, err := c.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if mech != "XOAUTH2" {
		t.Errorf("mech = %q, want XOAUTH2", mech)
	}
	want := "user=me@outlook.com\x01auth=Bearer tok-123\x01\x01"
	if string(ir) != want {
		t.Errorf("initial response = %q, want %q", ir, want)
	}
}

func TestXOAuth2ClientNextSurfacesChallenge(t *testing.T) {
	c := &xoauth2Client{}
	_, err := c.Next([]byte(`{"status":"401"}`))
	if err == nil || !strings.Contains(err.Error(), `{"status":"401"}`) {
		t.Errorf("Next should surface the server challenge, got %v", err)
	}
}
