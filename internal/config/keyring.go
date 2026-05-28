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
