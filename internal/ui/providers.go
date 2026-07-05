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

var providerPresets = map[string]providerPreset{
	"Gmail":   {"imap.gmail.com", 993, true, "smtp.gmail.com", 587, true, "App password"},
	"Outlook": {"outlook.office365.com", 993, true, "smtp.office365.com", 587, true, "app password (or sign in below)"},
	"Yahoo":   {"imap.mail.yahoo.com", 993, true, "smtp.mail.yahoo.com", 587, true, "App password"},
	"iCloud":  {"imap.mail.me.com", 993, true, "smtp.mail.me.com", 587, true, "App-specific password"},
}

var providerList = []string{"Custom", "Gmail", "Outlook", "Yahoo", "iCloud"}
