package auth

import (
	"errors"
	"testing"
)

func TestIsAuthFailure(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("dial tcp: connection refused"), false},
		{errors.New("login: [AUTHENTICATIONFAILED] Invalid credentials"), true},
		{errors.New("535 5.7.8 Username and Password not accepted"), true},
		{errors.New(`oauth2: "invalid_grant" "Token has been expired or revoked."`), true},
	}
	for _, tc := range cases {
		if got := IsAuthFailure(tc.err); got != tc.want {
			t.Errorf("IsAuthFailure(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
