package config

import (
	"fmt"
	"os/exec"
	"strings"
)

const keyringApp = "tidemail"

// keyringAvailable returns true if secret-tool is installed.
func keyringAvailable() bool {
	_, err := exec.LookPath("secret-tool")
	return err == nil
}

// secretTool runs `secret-tool` with the given arguments and returns stdout.
func secretTool(args ...string) (string, error) {
	cmd := exec.Command("secret-tool", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("secret-tool %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// storeSecret stores a secret in the system keychain.
// secret-tool store reads the value from stdin.
func storeSecret(key, value string) error {
	if value == "" {
		return nil
	}
	cmd := exec.Command("secret-tool", "store", "--label="+keyringApp, "application", keyringApp, "key", key)
	cmd.Stdin = strings.NewReader(value + "\n")
	return cmd.Run()
}

// lookupSecret retrieves a secret from the system keychain.
// Returns empty string if the key doesn't exist.
func lookupSecret(key string) string {
	out, err := secretTool("lookup", "application", keyringApp, "key", key)
	if err != nil {
		return ""
	}
	return out
}

// clearSecret removes a secret from the system keychain.
func clearSecret(key string) error {
	_, err := secretTool("clear", "application", keyringApp, "key", key)
	return err
}

// ── Account passwords ──────────────────────────────────────────────────────────

func accountPasswordKey(accountName string) string {
	return "account:" + accountName
}

// StoreAccountPassword saves an account password to the keychain.
// Returns true on success, false if keyring is unavailable.
func StoreAccountPassword(accountName, password string) bool {
	if !keyringAvailable() || password == "" {
		return false
	}
	if err := storeSecret(accountPasswordKey(accountName), password); err != nil {
		return false
	}
	return true
}

// GetAccountPassword retrieves an account password from the keychain.
// Returns empty string if not found or keyring unavailable.
func GetAccountPassword(accountName string) string {
	if !keyringAvailable() {
		return ""
	}
	return lookupSecret(accountPasswordKey(accountName))
}

// DeleteAccountPassword removes an account password from the keychain.
func DeleteAccountPassword(accountName string) {
	if !keyringAvailable() {
		return
	}
	_ = clearSecret(accountPasswordKey(accountName))
}

// ── AI API keys ────────────────────────────────────────────────────────────────

func aiKeyKey(providerField string) string {
	return "ai:" + providerField
}

// StoreAIKey saves an AI provider API key to the keychain.
// Returns true on success, false if keyring is unavailable.
func StoreAIKey(providerField, key string) bool {
	if !keyringAvailable() || key == "" {
		return false
	}
	if err := storeSecret(aiKeyKey(providerField), key); err != nil {
		return false
	}
	return true
}

// GetAIKey retrieves an AI provider API key from the keychain.
func GetAIKey(providerField string) string {
	if !keyringAvailable() {
		return ""
	}
	return lookupSecret(aiKeyKey(providerField))
}

// DeleteAIKey removes an AI provider API key from the keychain.
func DeleteAIKey(providerField string) {
	if !keyringAvailable() {
		return
	}
	_ = clearSecret(aiKeyKey(providerField))
}

// ── OAuth2 tokens ──────────────────────────────────────────────────────────────

func oauth2SecretKey(accountName string) string {
	return "oauth2:" + accountName
}

func oauth2RefreshKey(accountName string) string {
	return "oauth2_refresh:" + accountName
}

// StoreOAuth2Secret saves an OAuth2 client secret to the keychain.
func StoreOAuth2Secret(accountName, secret string) bool {
	if !keyringAvailable() || secret == "" {
		return false
	}
	if err := storeSecret(oauth2SecretKey(accountName), secret); err != nil {
		return false
	}
	return true
}

// GetOAuth2Secret retrieves an OAuth2 client secret from the keychain.
func GetOAuth2Secret(accountName string) string {
	if !keyringAvailable() {
		return ""
	}
	return lookupSecret(oauth2SecretKey(accountName))
}

// StoreOAuth2RefreshToken saves an OAuth2 refresh token to the keychain.
func StoreOAuth2RefreshToken(accountName, token string) bool {
	if !keyringAvailable() || token == "" {
		return false
	}
	if err := storeSecret(oauth2RefreshKey(accountName), token); err != nil {
		return false
	}
	return true
}

// GetOAuth2RefreshToken retrieves an OAuth2 refresh token from the keychain.
func GetOAuth2RefreshToken(accountName string) string {
	if !keyringAvailable() {
		return ""
	}
	return lookupSecret(oauth2RefreshKey(accountName))
}

// DeleteOAuth2Secrets removes OAuth2 secrets for an account from the keychain.
func DeleteOAuth2Secrets(accountName string) {
	if !keyringAvailable() {
		return
	}
	_ = clearSecret(oauth2SecretKey(accountName))
	_ = clearSecret(oauth2RefreshKey(accountName))
}

// ── Bulk operations ────────────────────────────────────────────────────────────

// stripSecrets moves all secrets from cfg into the keychain and clears the
// in-memory fields so they won't be written to the TOML config file.
// Secrets that were already in the keychain (password field empty) are left as-is.
// Returns the number of secrets successfully stored. Failing to store a secret
// leaves its in-memory value intact so it still gets written to the TOML file.
func stripSecrets(cfg *Config) int {
	stored := 0
	for i := range cfg.Accounts {
		if cfg.Accounts[i].Password != "" {
			if StoreAccountPassword(cfg.Accounts[i].Name, cfg.Accounts[i].Password) {
				cfg.Accounts[i].Password = ""
				stored++
			}
		}
		// OAuth2 refresh_token — per-user secret, store in keychain for OAuth
		// accounts. A non-OAuth account's token (if any) is left untouched: it
		// is already inert (fillSecrets and Uses*OAuth2 gate on AuthMethod), and
		// wiping it here would destroy a token an auto-migration mis-classified.
		// Real cleanup happens on account delete (deleteAccountCmd).
		if cfg.Accounts[i].AuthMethod == AuthOAuth2 && cfg.Accounts[i].RefreshToken != "" &&
			StoreOAuth2RefreshToken(cfg.Accounts[i].Name, cfg.Accounts[i].RefreshToken) {
			cfg.Accounts[i].RefreshToken = ""
			stored++
		}
	}
	if cfg.AI.OpenAIKey != "" && StoreAIKey("openai", cfg.AI.OpenAIKey) {
		cfg.AI.OpenAIKey = ""
		stored++
	}
	if cfg.AI.ClaudeKey != "" && StoreAIKey("claude", cfg.AI.ClaudeKey) {
		cfg.AI.ClaudeKey = ""
		stored++
	}
	if cfg.AI.GeminiKey != "" && StoreAIKey("gemini", cfg.AI.GeminiKey) {
		cfg.AI.GeminiKey = ""
		stored++
	}
	return stored
}

// fillSecrets fills in any empty password/key fields in cfg by looking them
// up from the system keychain. This is called after loading from TOML so
// that in-memory usage (IMAP/SMTP login, AI calls) has the real values.
func fillSecrets(cfg *Config) {
	for i := range cfg.Accounts {
		if cfg.Accounts[i].Password == "" {
			if pw := GetAccountPassword(cfg.Accounts[i].Name); pw != "" {
				cfg.Accounts[i].Password = pw
			}
		}
		// OAuth secrets are restored only for accounts that explicitly opted in
		// (AuthMethod == "oauth2"). This is set by the account form or by
		// migrateAuthMethod at load — never inferred from a keychain entry
		// alone, so an orphaned oauth2_refresh:<name> item stays inert.
		if cfg.Accounts[i].AuthMethod == AuthOAuth2 {
			if cfg.Accounts[i].RefreshToken == "" {
				if t := GetOAuth2RefreshToken(cfg.Accounts[i].Name); t != "" {
					cfg.Accounts[i].RefreshToken = t
				}
			}
			if cfg.Accounts[i].Provider == "Outlook" {
				// Microsoft is a public client: client ID only, no secret.
				if cfg.Accounts[i].ClientID == "" {
					cfg.Accounts[i].ClientID = cfg.OAuth.MSClientID
				}
			} else {
				if cfg.Accounts[i].ClientID == "" {
					cfg.Accounts[i].ClientID = cfg.OAuth.GoogleClientID
				}
				if cfg.Accounts[i].ClientSecret == "" {
					cfg.Accounts[i].ClientSecret = cfg.OAuth.GoogleClientSecret
				}
			}
		}
	}
	if cfg.AI.OpenAIKey == "" {
		if k := GetAIKey("openai"); k != "" {
			cfg.AI.OpenAIKey = k
		}
	}
	if cfg.AI.ClaudeKey == "" {
		if k := GetAIKey("claude"); k != "" {
			cfg.AI.ClaudeKey = k
		}
	}
	if cfg.AI.GeminiKey == "" {
		if k := GetAIKey("gemini"); k != "" {
			cfg.AI.GeminiKey = k
		}
	}
}
