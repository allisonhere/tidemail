package ui

import "github.com/allisonhere/tidemail/internal/autodiscover"

type providerPreset struct {
	autodiscover.Servers
	PassHint string
}

// Outlook signs in with OAuth (Microsoft retired basic auth for Outlook.com);
// the "app password" hint applies only to M365 tenants that still allow one.
// The server settings live in autodiscover, which also maps custom domains
// hosted by these providers back to the same presets.
var providerPresets = map[string]providerPreset{
	"Gmail":   {autodiscover.Gmail, "App password"},
	"Outlook": {autodiscover.Outlook, "M365 app password (rare)"},
	"Yahoo":   {autodiscover.Yahoo, "App password"},
	"iCloud":  {autodiscover.ICloud, "App-specific password"},
}

var providerList = []string{"Custom", "Gmail", "Outlook", "Yahoo", "iCloud"}
