package ui

const githubSponsorsURL = "https://github.com/sponsors/allisonhere"

// SupportLink is the single source of truth for support providers shown in the
// app.
type SupportLink struct {
	Name        string
	Description string
	URL         string
	Icon        string
}

var supportLinks = []SupportLink{
	{
		Name:        "GitHub Sponsors",
		Description: "One-time or monthly support",
		URL:         githubSponsorsURL,
		Icon:        "☕",
	},
}
