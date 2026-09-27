// Package plugin discovers and runs TideMail plugins.
//
// A plugin is an external executable plus a plugin.toml manifest in its own
// directory. TideMail talks to it with one JSON request on stdin and one JSON
// response on stdout per invocation (see protocol.go). Plugins never receive
// database handles, credentials, or the Bubble Tea model; they only see what a
// request carries. The package does not import internal/ui, so the application
// can own a *Manager without plugins depending on UI code.
//
// Plugins are experimental and not yet wired into normal TideMail behavior.
package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// APIVersion is the plugin protocol version this build of TideMail speaks.
const APIVersion = 1

// ManifestFile is the manifest file name inside each plugin directory.
const ManifestFile = "plugin.toml"

// idPattern keeps plugin IDs short, lowercase, and safe to reuse as keys in
// storage and file names.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Manifest describes one plugin, as read from its plugin.toml.
type Manifest struct {
	// ID is the stable identifier TideMail uses for the plugin. It is not a
	// display name.
	ID      string `toml:"id"`
	Name    string `toml:"name"`
	Version string `toml:"version"`
	// API is the protocol version the plugin speaks.
	API int `toml:"api"`
	// Command is the plugin executable, relative to the plugin directory.
	Command     string      `toml:"command"`
	Permissions Permissions `toml:"permissions"`
	// Events lists the automatic events the plugin wants. Declaring one only
	// makes the plugin eligible; the user still has to enable it.
	Events []string `toml:"events"`
}

// EventMessageReceived is sent automatically for newly received mail. It is
// the only automatic event in API v1.
const EventMessageReceived = "message.received"

// knownEvents are the event names a manifest may declare.
var knownEvents = map[string]bool{EventMessageReceived: true}

// WantsEvent reports whether the manifest declares event.
func (m Manifest) WantsEvent(event string) bool {
	for _, e := range m.Events {
		if e == event {
			return true
		}
	}
	return false
}

// ParseManifest decodes and validates manifest TOML. Unknown keys are
// rejected so a typo such as [permisions] cannot silently leave a setting at
// its default.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	md, err := toml.Decode(string(data), &m)
	if err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		return Manifest{}, fmt.Errorf("manifest has unknown keys: %s", strings.Join(keys, ", "))
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// LoadManifest reads and validates the manifest at path.
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	return ParseManifest(data)
}

// Validate checks the fields TideMail relies on.
func (m Manifest) Validate() error {
	var errs []error
	switch {
	case m.ID == "":
		errs = append(errs, errors.New("id is required"))
	case !idPattern.MatchString(m.ID):
		errs = append(errs, fmt.Errorf("id %q must be 1-64 characters of a-z, 0-9, '-' or '_', starting with a letter or digit", m.ID))
	}
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, errors.New("name is required"))
	}
	if m.API != APIVersion {
		errs = append(errs, fmt.Errorf("unsupported api version %d (this TideMail supports %d)", m.API, APIVersion))
	}
	if err := validateCommand(m.Command); err != nil {
		errs = append(errs, err)
	}
	seenEvents := map[string]bool{}
	for _, e := range m.Events {
		switch {
		case !knownEvents[e]:
			errs = append(errs, fmt.Errorf("unknown event %q (supported: %s)", e, EventMessageReceived))
		case seenEvents[e]:
			errs = append(errs, fmt.Errorf("event %q is listed twice", e))
		}
		seenEvents[e] = true
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid manifest: %w", errors.Join(errs...))
	}
	return nil
}

// validateCommand keeps the executable inside the plugin directory. The
// command is a file path, never a shell string, and it is never looked up on
// $PATH.
func validateCommand(command string) error {
	if command == "" {
		return errors.New("command is required")
	}
	if filepath.IsAbs(command) || !filepath.IsLocal(command) {
		return fmt.Errorf("command %q must be a relative path inside the plugin directory", command)
	}
	return nil
}
