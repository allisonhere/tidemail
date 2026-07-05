package auth

import "strings"

// IsAuthFailure reports whether err looks like a credential failure that
// requires the user to re-enter their account password — a rejected app
// password, or (for legacy accounts) an expired/revoked OAuth token. The status
// layer uses this to surface a "re-authenticate" hint instead of a raw error.
func IsAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	for _, sig := range []string{
		"authenticationfailed", // IMAP LOGIN rejected
		"authenticate failed",  // IMAP AUTHENTICATE (XOAUTH2) rejected
		"invalid credentials",  // common SMTP/IMAP wording
		"username and password not accepted",
		"invalid_grant", // OAuth token expired/revoked
		"expired or revoked",
		"aadsts70008",  // Microsoft: refresh token expired
		"aadsts700082", // Microsoft: refresh token expired (inactivity)
		"aadsts50173",  // Microsoft: token revoked (password change etc.)
	} {
		if strings.Contains(m, sig) {
			return true
		}
	}
	return false
}
