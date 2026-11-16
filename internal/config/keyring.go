package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const keyringApp = "tidemail"

// keyringOpTimeout bounds every keychain CLI invocation. A locked collection
// with an activatable-but-headless prompter (e.g. org.kde.secretprompter over
// SSH) can otherwise hang the call — and startup with it.
const keyringOpTimeout = 3 * time.Second

// keyringBackend names the CLI tool a platform uses to store secrets in its
// system keychain, and how to build that tool's argv for each operation.
type keyringBackend struct {
	tool string

	storeArgs func(key string) []string
	// stdin is true when the value is written to the store command's stdin
	// (secret-tool) rather than appended to storeArgs (security, which has
	// no stdin form for add-generic-password — the secret is briefly visible
	// in the process's argv/ps output on macOS, a limitation of the tool
	// with no CLI workaround).
	stdin bool

	lookupArgs func(key string) []string
	clearArgs  func(key string) []string
}

// backendFor returns the keychain backend for goos, or ok=false if the
// platform has none (e.g. Windows, which always uses the plaintext fallback).
func backendFor(goos string) (b keyringBackend, ok bool) {
	switch goos {
	case "linux":
		return keyringBackend{
			tool: "secret-tool",
			storeArgs: func(key string) []string {
				return []string{"store", "--label=" + keyringApp, "application", keyringApp, "key", key}
			},
			stdin: true,
			lookupArgs: func(key string) []string {
				return []string{"lookup", "application", keyringApp, "key", key}
			},
			clearArgs: func(key string) []string {
				return []string{"clear", "application", keyringApp, "key", key}
			},
		}, true
	case "darwin":
		return keyringBackend{
			tool: "security",
			// -U updates the item in place if one already exists, so store
			// stays idempotent like secret-tool's. Targets the user's
			// default (login) keychain, like secret-tool's default
			// collection; the same binary stores and looks up, so no -T
			// trusted-application flag is needed to avoid an ACL prompt.
			storeArgs: func(key string) []string {
				return []string{"add-generic-password", "-U", "-a", key, "-s", keyringApp, "-w"}
			},
			stdin: false,
			lookupArgs: func(key string) []string {
				return []string{"find-generic-password", "-a", key, "-s", keyringApp, "-w"}
			},
			clearArgs: func(key string) []string {
				return []string{"delete-generic-password", "-a", key, "-s", keyringApp}
			},
		}, true
	default:
		return keyringBackend{}, false
	}
}

// keyringInstalledFor reports whether goos's keychain CLI is on PATH.
func keyringInstalledFor(goos string) bool {
	b, ok := backendFor(goos)
	if !ok {
		return false
	}
	_, err := exec.LookPath(b.tool)
	return err == nil
}

var (
	keyringProbeOnce   sync.Once
	keyringProbeResult bool
	keyringProbeReason string
)

// keyringDisabled reports the TIDEMAIL_DISABLE_KEYRING opt-out. It is read on
// every call rather than cached with the probe below, so a test can scope it to
// itself. Tests must set it: the keychain is per-user and global, so a test that
// loads a config whose account names match the real ones would otherwise read
// live passwords out of it and write copies back under new keys.
func keyringDisabled() bool {
	v := strings.TrimSpace(os.Getenv("TIDEMAIL_DISABLE_KEYRING"))
	return v != "" && v != "0" && !strings.EqualFold(v, "false")
}

// keyringUsable reports whether the system keychain can actually store and
// retrieve a secret right now — not merely whether the backend tool is
// installed. It probes once per process with a sentinel round-trip; a locked
// login collection/keychain, a missing session bus, or a hung prompter all
// fail the probe, and the caller then falls back to the plaintext config file.
func keyringUsable() bool { return keyringUsableFor(runtime.GOOS) }

func keyringUsableFor(goos string) bool {
	if keyringDisabled() {
		return false
	}
	keyringProbeOnce.Do(func() {
		if !keyringInstalledFor(goos) {
			keyringProbeReason = "the system keychain tool is not installed"
			return
		}
		const k = "selftest"
		want := "ok-" + time.Now().Format("150405.000")
		if err := storeSecretFor(goos, k, want); err != nil {
			keyringProbeReason = "keychain is locked or unreachable (store failed)"
			return
		}
		got := lookupSecretFor(goos, k)
		_ = clearSecretFor(goos, k)
		if got != want {
			keyringProbeReason = "keychain is locked or unreachable (readback failed)"
			return
		}
		keyringProbeResult = true
	})
	return keyringProbeResult
}

// KeyringStatus reports whether the keychain is usable and, if not, a short
// human-readable reason for the UI.
func KeyringStatus() (usable bool, reason string) {
	if keyringDisabled() {
		return false, "credential storage disabled by TIDEMAIL_DISABLE_KEYRING"
	}
	return keyringUsable(), keyringProbeReason
}

// keyringAvailable is kept for callers that gate a write attempt; it now means
// "usable", so a locked keychain no longer looks writable.
func keyringAvailable() bool { return keyringUsable() }

// runBackendTool runs a keychain CLI tool with the given arguments and
// returns stdout, bounded by keyringOpTimeout.
func runBackendTool(tool string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keyringOpTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, tool, args...).Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", tool, strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// storeSecret stores a secret in the system keychain.
func storeSecret(key, value string) error { return storeSecretFor(runtime.GOOS, key, value) }

func storeSecretFor(goos, key, value string) error {
	if value == "" {
		return nil
	}
	b, ok := backendFor(goos)
	if !ok {
		return fmt.Errorf("no keychain backend for %s", goos)
	}
	ctx, cancel := context.WithTimeout(context.Background(), keyringOpTimeout)
	defer cancel()
	args := b.storeArgs(key)
	if !b.stdin {
		args = append(args, value)
	}
	cmd := exec.CommandContext(ctx, b.tool, args...)
	if b.stdin {
		cmd.Stdin = strings.NewReader(value + "\n")
	}
	return cmd.Run()
}

// lookupSecret retrieves a secret from the system keychain.
// Returns empty string if the key doesn't exist or the lookup fails.
func lookupSecret(key string) string { return lookupSecretFor(runtime.GOOS, key) }

func lookupSecretFor(goos, key string) string {
	b, ok := backendFor(goos)
	if !ok {
		return ""
	}
	out, err := runBackendTool(b.tool, b.lookupArgs(key)...)
	if err != nil {
		return ""
	}
	return out
}

// clearSecret removes a secret from the system keychain.
func clearSecret(key string) error { return clearSecretFor(runtime.GOOS, key) }

func clearSecretFor(goos, key string) error {
	b, ok := backendFor(goos)
	if !ok {
		return fmt.Errorf("no keychain backend for %s", goos)
	}
	_, err := runBackendTool(b.tool, b.clearArgs(key)...)
	return err
}

// ── Account secrets ────────────────────────────────────────────────────────────
//
// Every account secret is stored under the account's stable ID. Builds up to
// v1.0.19 keyed them by display name instead, so each getter falls back to the
// name-keyed item when the ID-keyed one is absent.
//
// The fallback is read-only on purpose: nothing rewrites or clears the legacy
// item as a "migration". keyringUsable() probes the keychain with a 3s timeout
// and a locked or momentarily unreachable collection fails that probe, so a
// migration that cleared the old item could destroy the only copy of a
// password. The legacy item is removed only when the user deletes the account.

func accountPasswordKey(id string) string {
	return "account:" + id
}

// lookupWithLegacy reads the ID-keyed item, falling back to the item an older
// build wrote under the display name.
func lookupWithLegacy(key func(string) string, id, legacyName string) string {
	if !keyringAvailable() {
		return ""
	}
	if id != "" {
		if v := lookupSecret(key(id)); v != "" {
			return v
		}
	}
	if legacyName == "" || legacyName == id {
		return ""
	}
	return lookupSecret(key(legacyName))
}

// StoreAccountPassword saves an account password to the keychain under the
// account's stable ID. Returns true on success, false if keyring is unavailable.
func StoreAccountPassword(id, password string) bool {
	if !keyringAvailable() || id == "" || password == "" {
		return false
	}
	if err := storeSecret(accountPasswordKey(id), password); err != nil {
		return false
	}
	return true
}

// GetAccountPassword retrieves an account password from the keychain, falling
// back to the legacy name-keyed item. Returns empty string if not found or
// keyring unavailable.
func GetAccountPassword(id, legacyName string) string {
	return lookupWithLegacy(accountPasswordKey, id, legacyName)
}

// DeleteAccountPassword removes an account password from the keychain, both
// generations of the key.
func DeleteAccountPassword(id, legacyName string) {
	if !keyringAvailable() {
		return
	}
	if id != "" {
		_ = clearSecret(accountPasswordKey(id))
	}
	if legacyName != "" && legacyName != id {
		_ = clearSecret(accountPasswordKey(legacyName))
	}
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

// ── Plugin secrets ─────────────────────────────────────────────────────────────
//
// Plugin secret settings are keyed by plugin ID and setting key, so one
// plugin's secrets are never visible under another plugin's name. There is no
// plaintext fallback: without a usable keychain a plugin secret is not saved.

func pluginSecretKey(pluginID, key string) string {
	return "plugin:" + pluginID + ":" + key
}

// MinPluginSecretLen is the shortest plugin secret TideMail will store. Plugin
// output is scrubbed of secret values before it reaches an error message, and
// that scrub skips values shorter than this (masking them would mangle ordinary
// text), so a shorter secret could leak.
const MinPluginSecretLen = 6

// StorePluginSecret saves a plugin secret to the keychain.
func StorePluginSecret(pluginID, key, value string) error {
	if value == "" {
		return errors.New("empty secret")
	}
	if len(value) < MinPluginSecretLen {
		return fmt.Errorf("secret must be at least %d characters", MinPluginSecretLen)
	}
	if !keyringAvailable() {
		_, reason := KeyringStatus()
		return fmt.Errorf("system keychain unavailable: %s", reason)
	}
	if err := storeSecret(pluginSecretKey(pluginID, key), value); err != nil {
		return errors.New("could not write to the system keychain")
	}
	return nil
}

// GetPluginSecret returns a plugin secret and whether it is usable. Older
// short values remain in the keychain but appear unset until replaced.
func GetPluginSecret(pluginID, key string) (string, bool) {
	if !keyringAvailable() {
		return "", false
	}
	v := lookupSecret(pluginSecretKey(pluginID, key))
	if len(v) < MinPluginSecretLen {
		return "", false
	}
	return v, true
}

// DeletePluginSecret removes a plugin secret from the keychain.
func DeletePluginSecret(pluginID, key string) error {
	if !keyringAvailable() {
		_, reason := KeyringStatus()
		return fmt.Errorf("system keychain unavailable: %s", reason)
	}
	_ = clearSecret(pluginSecretKey(pluginID, key))
	return nil
}

// ── OAuth2 tokens ──────────────────────────────────────────────────────────────

func oauth2SecretKey(id string) string {
	return "oauth2:" + id
}

func oauth2RefreshKey(id string) string {
	return "oauth2_refresh:" + id
}

// StoreOAuth2Secret saves an OAuth2 client secret to the keychain.
func StoreOAuth2Secret(id, secret string) bool {
	if !keyringAvailable() || id == "" || secret == "" {
		return false
	}
	if err := storeSecret(oauth2SecretKey(id), secret); err != nil {
		return false
	}
	return true
}

// GetOAuth2Secret retrieves an OAuth2 client secret from the keychain, falling
// back to the legacy name-keyed item.
func GetOAuth2Secret(id, legacyName string) string {
	return lookupWithLegacy(oauth2SecretKey, id, legacyName)
}

// StoreOAuth2RefreshToken saves an OAuth2 refresh token to the keychain.
func StoreOAuth2RefreshToken(id, token string) bool {
	if !keyringAvailable() || id == "" || token == "" {
		return false
	}
	if err := storeSecret(oauth2RefreshKey(id), token); err != nil {
		return false
	}
	return true
}

// GetOAuth2RefreshToken retrieves an OAuth2 refresh token from the keychain,
// falling back to the legacy name-keyed item.
func GetOAuth2RefreshToken(id, legacyName string) string {
	return lookupWithLegacy(oauth2RefreshKey, id, legacyName)
}

// DeleteOAuth2Secrets removes an account's OAuth2 secrets from the keychain,
// both generations of the key.
func DeleteOAuth2Secrets(id, legacyName string) {
	if !keyringAvailable() {
		return
	}
	for _, key := range []func(string) string{oauth2SecretKey, oauth2RefreshKey} {
		if id != "" {
			_ = clearSecret(key(id))
		}
		if legacyName != "" && legacyName != id {
			_ = clearSecret(key(legacyName))
		}
	}
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
			if StoreAccountPassword(cfg.Accounts[i].ID, cfg.Accounts[i].Password) {
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
			StoreOAuth2RefreshToken(cfg.Accounts[i].ID, cfg.Accounts[i].RefreshToken) {
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
			if pw := GetAccountPassword(cfg.Accounts[i].ID, cfg.Accounts[i].Name); pw != "" {
				cfg.Accounts[i].Password = pw
			}
		}
		// OAuth secrets are restored only for accounts that explicitly opted in
		// (AuthMethod == "oauth2"). This is set by the account form or by
		// migrateAuthMethod at load — never inferred from a keychain entry
		// alone, so an orphaned oauth2_refresh:<name> item stays inert.
		if cfg.Accounts[i].AuthMethod == AuthOAuth2 {
			if cfg.Accounts[i].RefreshToken == "" {
				if t := GetOAuth2RefreshToken(cfg.Accounts[i].ID, cfg.Accounts[i].Name); t != "" {
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
