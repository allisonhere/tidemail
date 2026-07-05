package ui

type providerPreset struct {
	IMAPHost string
	IMAPPort int
	IMAPTLS  bool
	SMTPHost string
	SMTPPort int
	SMTPTLS  bool
	PassHint string
}

// No Outlook preset: Microsoft disabled basic auth (passwords/app passwords)
// for Outlook.com IMAP in 2024, so a password preset can never sign in.
// Outlook support needs OAuth2 (built and then reverted; see the git stash).
var providerPresets = map[string]providerPreset{
	"Gmail":  {"imap.gmail.com", 993, true, "smtp.gmail.com", 587, true, "App password"},
	"Yahoo":  {"imap.mail.yahoo.com", 993, true, "smtp.mail.yahoo.com", 587, true, "App password"},
	"iCloud": {"imap.mail.me.com", 993, true, "smtp.mail.me.com", 587, true, "App-specific password"},
}

var providerList = []string{"Custom", "Gmail", "Yahoo", "iCloud"}
