package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Theme    string          `toml:"theme"`
	Display  DisplayConfig   `toml:"display"`
	Feed     FeedConfig      `toml:"feed"`
	Updates  UpdatesConfig   `toml:"updates"`
	AI       AIConfig        `toml:"ai"`
	OAuth    OAuthConfig     `toml:"oauth"`
	Accounts []AccountConfig `toml:"account"`
}

type OAuthConfig struct {
	GoogleClientID     string `toml:"google_client_id"`
	GoogleClientSecret string `toml:"google_client_secret"`
	MSClientID         string `toml:"ms_client_id"`
}

// ThunderbirdMSClientID is Mozilla Thunderbird's Microsoft app registration —
// the de facto shared public client ID of the open-source mail ecosystem (mutt,
// getmail, mbsync setups). A public client's ID identifies the app, it is not a
// secret; Thunderbird's own docs publish it. Microsoft forbids the device-code
// flow for this registration, so Outlook accounts using it sign in via the
// authorization-code (paste the code) flow.
const ThunderbirdMSClientID = "9e5f94bc-e8a4-4e73-b8be-63364c29d753"

// DefaultMSClientID is the client ID Outlook accounts use unless
// TIDEMAIL_MS_CLIENT_ID overrides it.
const DefaultMSClientID = ThunderbirdMSClientID

type RetroTerminalTweak struct {
	Bg     string `toml:"bg"`
	Fg     string `toml:"fg"`
	Accent string `toml:"accent"`
}

type DisplayConfig struct {
	Icons                 bool               `toml:"icons"`
	DateFormat            string             `toml:"date_format"`
	MarkReadOnOpen        bool               `toml:"mark_read_on_open"`
	MarkReadOnFocus       bool               `toml:"mark_read_on_focus"`
	FocusLine             bool               `toml:"focus_line"`
	DefaultUnreadOnly     bool               `toml:"default_unread_only"`
	ActionableLinks       bool               `toml:"actionable_links"`
	FilterLinks           bool               `toml:"filter_links"`
	ShowSender            bool               `toml:"show_sender"`
	ThreadedConversations bool               `toml:"threaded_conversations"`
	ReadingWidth          int                `toml:"reading_width"`
	AccountsWidthPercent  int                `toml:"accounts_width_percent"`
	MessagesHeightPercent int                `toml:"messages_height_percent"`
	ConfirmQuit           bool               `toml:"confirm_quit"`
	ShowHeaders           bool               `toml:"show_headers"`
	UnreadFirst           bool               `toml:"unread_first"`
	StarredFirst          bool               `toml:"starred_first"`
	Notifications         bool               `toml:"notifications"`
	Browser               string             `toml:"browser"`
	Density               string             `toml:"density"`
	PaneCorners           string             `toml:"pane_corners"` // "square" or "round"
	ComposeVim            bool               `toml:"compose_vim"`
	SendDelaySeconds      int                `toml:"send_delay_seconds"` // undo grace period; 0 = send immediately
	VT52                  RetroTerminalTweak `toml:"vt52"`
	VT100                 RetroTerminalTweak `toml:"vt100"`
}

type UpdatesConfig struct {
	CheckOnStartup     bool   `toml:"check_on_startup"`
	CheckIntervalHours int    `toml:"check_interval_hours"`
	LastCheckedUnix    int64  `toml:"last_checked_unix"`
	DismissedVersion   string `toml:"dismissed_version"`
	AvailableVersion   string `toml:"available_version"`
	AvailableSummary   string `toml:"available_summary"`
	AvailablePublished int64  `toml:"available_published_unix"`
}

type FeedConfig struct {
	MaxBodyMiB int `toml:"max_body_mib"`
}

type AIConfig struct {
	Provider            string `toml:"provider"`
	OpenAIKey           string `toml:"openai_key"`
	OpenAIModel         string `toml:"openai_model"`
	ClaudeKey           string `toml:"claude_key"`
	ClaudeModel         string `toml:"claude_model"`
	GeminiKey           string `toml:"gemini_key"`
	GeminiModel         string `toml:"gemini_model"`
	OllamaURL           string `toml:"ollama_url"`
	OllamaModel         string `toml:"ollama_model"`
	SavePath            string `toml:"save_path"`
	MarkReadOnSummarize bool   `toml:"mark_read_on_summarize"`
}

type AccountConfig struct {
	Name     string `toml:"name"`
	Provider string `toml:"provider"`
	IMAPHost string `toml:"imap_host"`
	IMAPPort int    `toml:"imap_port"`
	IMAPTLS  bool   `toml:"imap_tls"`
	SMTPHost string `toml:"smtp_host"`
	SMTPPort int    `toml:"smtp_port"`
	SMTPTLS  bool   `toml:"smtp_tls"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	From     string `toml:"from"`
	// SyncMinutes selects how the account refreshes in the background:
	//
	//	< 0  manual only — no polling and no push connection
	//	  0  push — IMAP IDLE, plus an infrequent safety poll
	//	> 0  poll every N minutes, with IDLE accelerating it
	//
	// Note that 0 is the "some background work" case, not the "none" case; use
	// -1 for none.
	SyncMinutes int `toml:"sync_minutes"`
	// Signature is appended to outgoing mail after a standard "-- " delimiter.
	// The account form accepts literal \n escapes for multi-line signatures;
	// TOML multi-line strings work too when editing the file directly.
	Signature string `toml:"signature"`

	// OAuth2 — if RefreshToken is set, OAuth2 is used instead of password.
	// ClientID and ClientSecret are auto-filled from the app-level [oauth] config at load time.
	RefreshToken string `toml:"refresh_token"`

	// Filled at load time from OAuthConfig — never written to TOML.
	ClientID     string `toml:"-"`
	ClientSecret string `toml:"-"`
}

// UsesOAuth2 returns true if this account is configured for OAuth2 auth.
func (a AccountConfig) UsesOAuth2() bool {
	return a.RefreshToken != ""
}

// UsesGoogleOAuth2 reports whether the account authenticates with Gmail XOAUTH2.
// The provider gate keeps an orphaned refresh token from silently changing
// another account's auth path.
func (a AccountConfig) UsesGoogleOAuth2() bool {
	return a.RefreshToken != "" && a.Provider == "Gmail"
}

// UsesMicrosoftOAuth2 reports whether the account authenticates with Outlook
// XOAUTH2 (Microsoft retired basic auth for Outlook.com).
func (a AccountConfig) UsesMicrosoftOAuth2() bool {
	return a.RefreshToken != "" && a.Provider == "Outlook"
}

func DefaultAccountConfig() AccountConfig {
	return AccountConfig{
		IMAPPort: 993,
		IMAPTLS:  true,
		SMTPPort: 587,
		SMTPTLS:  true,
	}
}

func DefaultConfig() Config {
	return Config{
		Theme: "catppuccin-mocha",
		Display: DisplayConfig{
			Icons:                 true,
			DateFormat:            "relative",
			MarkReadOnOpen:        true,
			FocusLine:             true,
			ShowSender:            true,
			AccountsWidthPercent:  28,
			MessagesHeightPercent: 40,
			Density:               "compact",
			PaneCorners:           "square",
			ConfirmQuit:           true,
			ShowHeaders:           true,
			Notifications:         true,
			SendDelaySeconds:      5,
		},
		Updates: UpdatesConfig{
			CheckOnStartup:     true,
			CheckIntervalHours: 24,
		},
		Feed: FeedConfig{
			MaxBodyMiB: 10,
		},
		AI: AIConfig{
			OllamaURL:   "http://localhost:11434",
			OllamaModel: "llama3.2",
			SavePath:    "~/",
		},
		OAuth: OAuthConfig{
			// App-level OAuth client IDs — sourced from env vars first, then the
			// config file. These identify the app, not individual users. Gmail
			// has no default (bring your own); Outlook falls back to
			// Thunderbird's shared public client.
			GoogleClientID:     firstNonEmpty(os.Getenv("TIDEMAIL_GOOGLE_CLIENT_ID"), ""),
			GoogleClientSecret: firstNonEmpty(os.Getenv("TIDEMAIL_GOOGLE_CLIENT_SECRET"), ""),
			MSClientID:         firstNonEmpty(os.Getenv("TIDEMAIL_MS_CLIENT_ID"), DefaultMSClientID),
		},
	}
}

func Load() (Config, error) {
	path, err := configPath()
	if err != nil {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return DefaultConfig(), err
	}

	cfg := DefaultConfig()
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return DefaultConfig(), err
	}
	if cfg.Updates.CheckIntervalHours <= 0 {
		cfg.Updates.CheckIntervalHours = DefaultConfig().Updates.CheckIntervalHours
	}
	cfg.Display.Density = NormalizeDisplayDensity(cfg.Display.Density)
	cfg.Display.PaneCorners = NormalizePaneCorners(cfg.Display.PaneCorners)
	fillSecrets(&cfg)
	return cfg, nil
}

func NormalizeDisplayDensity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "comfortable":
		return "comfortable"
	default:
		return "compact"
	}
}

func NormalizePaneCorners(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "round":
		return "round"
	default:
		return "square"
	}
}

func IsRetroTerminalTheme(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "vt52" || n == "vt100"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func Save(cfg Config) error {
	// Deep-copy accounts to avoid mutating the caller's slice (stripSecrets
	// clears Password fields on the shared backing array).
	accts := make([]AccountConfig, len(cfg.Accounts))
	copy(accts, cfg.Accounts)
	cfg.Accounts = accts
	stripSecrets(&cfg)
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}

	return toml.NewEncoder(f).Encode(cfg)
}

func SecurityWarnings() ([]string, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return []string{fmt.Sprintf("config file %s may contain passwords/API keys and is readable by other users; run: chmod 600 %s", path, path)}, nil
	}
	return nil, nil
}

func RedactSecrets(s string, cfg Config) string {
	for _, secret := range secretValues(cfg) {
		s = strings.ReplaceAll(s, secret, "[redacted]")
	}
	return s
}

func secretValues(cfg Config) []string {
	seen := map[string]struct{}{}
	var secrets []string
	add := func(secret string) {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			return
		}
		if _, ok := seen[secret]; ok {
			return
		}
		seen[secret] = struct{}{}
		secrets = append(secrets, secret)
	}
	add(cfg.AI.OpenAIKey)
	add(cfg.AI.ClaudeKey)
	add(cfg.AI.GeminiKey)
	add(cfg.OAuth.GoogleClientSecret)
	for _, account := range cfg.Accounts {
		add(account.Password)
		add(account.RefreshToken)
		add(account.ClientSecret)
	}
	return secrets
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	return filepath.Join(xdg, "tidemail", "config.toml"), nil
}

// LogPath returns the path to the fetch log file (alongside config.toml).
func LogPath() (string, error) {
	cfgPath, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfgPath), "fetch.log"), nil
}
