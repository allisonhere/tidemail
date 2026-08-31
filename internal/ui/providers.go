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

// Outlook signs in with OAuth (Microsoft retired basic auth for Outlook.com);
// the "app password" hint applies only to M365 tenants that still allow one.
var providerPresets = map[string]providerPreset{
	"Gmail":   {"imap.gmail.com", 993, true, "smtp.gmail.com", 587, true, "App password"},
	"Outlook": {"outlook.office365.com", 993, true, "smtp.office365.com", 587, true, "M365 app password (rare)"},
	"Yahoo":   {"imap.mail.yahoo.com", 993, true, "smtp.mail.yahoo.com", 587, true, "App password"},
	"iCloud":  {"imap.mail.me.com", 993, true, "smtp.mail.me.com", 587, true, "App-specific password"},
}

var providerList = []string{"Custom", "Gmail", "Outlook", "Yahoo", "iCloud"}
