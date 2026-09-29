package ui

const koFiSupportURL = "https://ko-fi.com/E1E31QQRI9"

// SupportLink is the single source of truth for support providers shown in the
// app. GitHub Sponsors can be added here when the project has a confirmed
// Sponsors listing.
type SupportLink struct {
	Name        string
	Description string
	URL         string
	Icon        string
}

var supportLinks = []SupportLink{
	{
		Name:        "Ko-fi",
		Description: "One-time or recurring support",
		URL:         koFiSupportURL,
		Icon:        "☕",
	},
}
