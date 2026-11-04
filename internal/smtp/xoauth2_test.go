package smtp

import (
	"net/smtp"
	"strings"
	"testing"
)

func TestXOAUTH2AuthStartInitialResponseFormat(t *testing.T) {
	a := &xoauth2Auth{user: "drbayless@gmail.com", token: "ya29.token"}
	mech, resp, err := a.Start(&smtp.ServerInfo{Name: "smtp.gmail.com", TLS: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if mech != "XOAUTH2" {
		t.Fatalf("mechanism = %q, want XOAUTH2", mech)
	}
	// RAW bytes — net/smtp base64-encodes them itself; encoding here would
	// double-encode and Gmail answers "501 5.5.2 Cannot Decode".
	want := "user=drbayless@gmail.com\x01auth=Bearer ya29.token\x01\x01"
	if string(resp) != want {
		t.Fatalf("initial response = %q, want %q", resp, want)
	}
}

func TestXOAUTH2AuthNextSurfacesServerError(t *testing.T) {
	a := &xoauth2Auth{user: "a", token: "b"}
	_, err := a.Next([]byte(`{"status":"401","schemes":"Bearer"}`), true)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Next(_, true) should surface the server error, got %v", err)
	}
	if got, err := a.Next(nil, false); err != nil || got != nil {
		t.Fatalf("Next(nil, false) = %q, %v; want nil, nil", got, err)
	}
}

func TestXOAUTH2NeverReturnsCredentialsWithoutTLS(t *testing.T) {
	for _, host := range []string{"smtp.gmail.com", "localhost", "127.0.0.1", "::1"} {
		a := &xoauth2Auth{user: "test@example.com", token: "secret-token"}
		mechanism, response, err := a.Start(&smtp.ServerInfo{Name: host, TLS: false, Auth: []string{"XOAUTH2"}})
		if err == nil || mechanism != "" || len(response) != 0 {
			t.Fatalf("plaintext credentials exposed for %s", host)
		}
		if strings.Contains(err.Error(), a.token) {
			t.Fatal("token leaked in error")
		}
	}
}
