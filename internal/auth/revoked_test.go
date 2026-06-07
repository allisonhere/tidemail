package auth

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/oauth2"
)

func TestIsTokenRevoked(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"typed invalid_grant", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}, true},
		{"wrapped typed", fmt.Errorf("oauth2 refresh: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}), true},
		{"string expired/revoked", errors.New(`oauth2: "invalid_grant" "Token has been expired or revoked."`), true},
		{"other oauth error", &oauth2.RetrieveError{ErrorCode: "invalid_client"}, false},
		{"unrelated", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTokenRevoked(tc.err); got != tc.want {
				t.Fatalf("IsTokenRevoked = %v, want %v", got, tc.want)
			}
		})
	}
}
