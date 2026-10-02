package mailcore

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/auth"
)

func TestStartMicrosoftSignInNeedsAClientID(t *testing.T) {
	if _, err := StartMicrosoftSignIn(context.Background(), ""); err == nil {
		t.Fatal("an empty client ID must be refused")
	}
}

func TestMicrosoftSignInURLAndCancel(t *testing.T) {
	s, err := StartMicrosoftSignIn(context.Background(), "my-client-id")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(s.AuthURL())
	if err != nil || u.Query().Get("client_id") != "my-client-id" || !strings.HasPrefix(u.Query().Get("redirect_uri"), "http://localhost:") {
		t.Fatalf("unexpected sign-in URL %q (%v)", s.AuthURL(), err)
	}
	s.Cancel()
	if tok, err := s.Wait(); err == nil || tok != "" {
		t.Fatalf("a cancelled sign-in must fail, got %q, %v", tok, err)
	}
}

func TestSetRefreshTokenSaverReceivesRotatedTokens(t *testing.T) {
	t.Cleanup(func() { SetRefreshTokenSaver(nil) })
	var gotKey, gotToken string
	SetRefreshTokenSaver(func(sessionKey, refreshToken string) error {
		gotKey, gotToken = sessionKey, refreshToken
		return nil
	})
	if err := auth.PersistRefreshToken("me@outlook.com@outlook.office365.com", "new-refresh"); err != nil {
		t.Fatal(err)
	}
	if gotKey != "me@outlook.com@outlook.office365.com" || gotToken != "new-refresh" {
		t.Fatalf("saver got %q / %q", gotKey, gotToken)
	}
	SetRefreshTokenSaver(nil)
	if err := auth.PersistRefreshToken("k", "t"); err != nil {
		t.Fatalf("the default saver must accept tokens: %v", err)
	}
}

func TestForgetOAuthSessionIsSafeForUnknownAccounts(t *testing.T) {
	ForgetOAuthSession("nobody@example.test")
	ForgetOAuthSession("")
}
