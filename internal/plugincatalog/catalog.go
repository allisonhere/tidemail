// Package plugincatalog lists the plugins published in TideMail's official
// plugin repository and installs, updates and removes them.
//
// The repository's release carries a catalog.json describing each plugin and a
// tarball per plugin and platform. A tarball's SHA-256 is taken from the
// catalog, which comes from the same release, so this guards against corrupt
// or mismatched downloads, not against a compromised repository. Nothing here
// runs a plugin: install only downloads, verifies, validates the manifest, and
// moves files into the plugins directory.
package plugincatalog

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/update"
)

// DefaultURL is the catalog of the latest release of the official plugins.
const DefaultURL = "https://github.com/allisonhere/tidemail-plugins/releases/latest/download/catalog.json"

// SchemaVersion is the catalog format this build understands.
const SchemaVersion = 1

const (
	maxCatalogBytes = 1 << 20
	maxEntries      = 200
	maxNameLen      = 80
)

// Catalog is the validated list of available plugins.
type Catalog struct {
	Schema  int     `json:"schema"`
	Plugins []Entry `json:"plugins"`
}

// Entry is one available plugin.
type Entry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	API         int    `json:"api"`
	Homepage    string `json:"homepage,omitempty"`
	// Permissions is what the plugin's manifest asks for. Install refuses a
	// download whose manifest asks for anything different.
	Permissions plugin.Permissions `json:"permissions"`
	// Assets maps "<goos>-<goarch>" to the tarball for that platform.
	Assets map[string]Asset `json:"assets"`
}

// Asset is one downloadable tarball.
type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// Platform is the key into Entry.Assets for this machine.
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// Available reports whether the plugin has a build for this machine.
func (e Entry) Available() bool { _, ok := e.Assets[Platform()]; return ok }

// Client fetches catalogs and installs plugins. The zero value uses the
// official catalog and only trusts GitHub-hosted HTTPS URLs.
type Client struct {
	HTTP *http.Client
	// URL overrides DefaultURL.
	URL string
	// Trusted overrides the host check (for tests).
	Trusted func(rawURL string) bool
}

func (c *Client) url() string {
	if c != nil && c.URL != "" {
		return c.URL
	}
	return DefaultURL
}

func (c *Client) trusted(rawURL string) bool {
	if c != nil && c.Trusted != nil {
		return c.Trusted(rawURL)
	}
	return update.TrustedAssetHost(rawURL)
}

// httpClient refuses to follow a redirect to a host that is not trusted, so a
// download cannot be bounced somewhere else.
func (c *Client) httpClient() *http.Client {
	base := http.DefaultClient
	if c != nil && c.HTTP != nil {
		base = c.HTTP
	}
	clone := *base
	if clone.Timeout == 0 {
		clone.Timeout = 10 * time.Minute
	}
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if !c.trusted(req.URL.String()) {
			return fmt.Errorf("refusing redirect to untrusted host %s", req.URL.Host)
		}
		return nil
	}
	return &clone
}

// Fetch downloads and validates the catalog. Entries built for a plugin API
// this TideMail doesn't speak are dropped; entries with no build for this
// platform are kept, so the UI can show them as unavailable.
func (c *Client) Fetch(ctx context.Context) (Catalog, error) {
	u := c.url()
	if !c.trusted(u) {
		return Catalog{}, fmt.Errorf("refusing to fetch the plugin catalog from %s", u)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Catalog{}, err
	}
	req.Header.Set("User-Agent", "tide-plugin-catalog")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Catalog{}, fmt.Errorf("fetch the plugin catalog: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Catalog{}, fmt.Errorf("fetch the plugin catalog: HTTP %d (no plugin release published yet?)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return Catalog{}, fmt.Errorf("read the plugin catalog: %w", err)
	}
	if len(data) > maxCatalogBytes {
		return Catalog{}, errors.New("the plugin catalog is too large")
	}
	return c.parse(data)
}

func (c *Client) parse(data []byte) (Catalog, error) {
	var raw Catalog
	if err := json.Unmarshal(data, &raw); err != nil {
		return Catalog{}, fmt.Errorf("the plugin catalog is not valid JSON: %w", err)
	}
	if raw.Schema != SchemaVersion {
		return Catalog{}, fmt.Errorf("this TideMail understands plugin catalog schema %d, not %d: update TideMail", SchemaVersion, raw.Schema)
	}
	if len(raw.Plugins) > maxEntries {
		return Catalog{}, errors.New("the plugin catalog has too many entries")
	}
	seen := map[string]bool{}
	out := Catalog{Schema: raw.Schema}
	for _, e := range raw.Plugins {
		if err := c.validate(e); err != nil {
			return Catalog{}, fmt.Errorf("plugin %q in the catalog: %w", e.ID, err)
		}
		if seen[e.ID] {
			return Catalog{}, fmt.Errorf("plugin %q is listed twice in the catalog", e.ID)
		}
		seen[e.ID] = true
		if e.API != plugin.APIVersion {
			continue // built for a plugin API this TideMail doesn't speak
		}
		out.Plugins = append(out.Plugins, e)
	}
	sort.SliceStable(out.Plugins, func(i, j int) bool { return out.Plugins[i].ID < out.Plugins[j].ID })
	return out, nil
}

func (c *Client) validate(e Entry) error {
	if err := plugin.ValidateID(e.ID); err != nil {
		return err
	}
	if err := plainText(e.Name, maxNameLen, true); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	if err := plainText(e.Description, plugin.MaxDescription, false); err != nil {
		return fmt.Errorf("description: %w", err)
	}
	if !update.IsStableVersion(e.Version) {
		return fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", e.Version)
	}
	if e.Homepage != "" && (!strings.HasPrefix(e.Homepage, "https://") || len(e.Homepage) > 200 || strings.ContainsAny(e.Homepage, " \t\r\n")) {
		return errors.New("homepage must be an https:// URL")
	}
	for platform, a := range e.Assets {
		if !c.trusted(a.URL) {
			return fmt.Errorf("%s: download URL is not on a trusted host", platform)
		}
		if b, err := hex.DecodeString(a.SHA256); err != nil || len(b) != 32 {
			return fmt.Errorf("%s: sha256 is not 64 hex digits", platform)
		}
	}
	return nil
}

// plainText accepts short single-line text with no control characters: catalog
// text is shown in the terminal, so an escape sequence must never reach it.
func plainText(s string, max int, required bool) error {
	switch {
	case !utf8.ValidString(s):
		return errors.New("not valid UTF-8")
	case strings.TrimSpace(s) == "" && required:
		return errors.New("is required")
	case utf8.RuneCountInString(s) > max:
		return fmt.Errorf("longer than %d characters", max)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return errors.New("contains control characters")
		}
	}
	return nil
}
