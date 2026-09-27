package plugin

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Setting types a manifest may declare. TideMail renders each generically;
// plugins cannot supply their own UI.
const (
	SettingBool   = "bool"
	SettingSelect = "select"
	// SettingSecret values live in the system keychain, never in config.toml,
	// and reach the plugin only through its own process environment.
	SettingSecret = "secret"
)

// Limits on declared settings.
const (
	MaxSettings        = 32
	MaxSettingLabelLen = 48
	MaxSettingHelpLen  = 240
	MaxSelectOptions   = 16
	MaxSelectOptionLen = 48
)

// CapabilityTest lets a plugin answer plugin.test, which TideMail offers as
// "Test plugin configuration". The response is informational only.
const CapabilityTest = "plugin.test"

// MethodTest is the request method for CapabilityTest.
const MethodTest = "plugin.test"

// knownCapabilities are the capability names a manifest may declare.
var knownCapabilities = map[string]bool{CapabilityTest: true}

// settingKeyPattern keeps setting keys short identifiers that are also safe as
// environment variable suffixes.
var settingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// SettingSpec is one [[settings]] entry in a manifest.
type SettingSpec struct {
	Key     string   `toml:"key"`
	Label   string   `toml:"label"`
	Type    string   `toml:"type"`
	Default any      `toml:"default"`
	Options []string `toml:"options"`
	// Help is an optional sentence shown under the setting, e.g. what a
	// secret is used for.
	Help string `toml:"help"`
}

// validateSettings checks declared settings and capabilities.
func validateSettings(specs []SettingSpec) []error {
	var errs []error
	if len(specs) > MaxSettings {
		errs = append(errs, fmt.Errorf("too many settings: %d (limit %d)", len(specs), MaxSettings))
	}
	seen := map[string]bool{}
	for i, s := range specs {
		where := fmt.Sprintf("setting %d", i+1)
		if s.Key != "" {
			where = fmt.Sprintf("setting %q", s.Key)
		}
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf(where+": "+format, args...))
		}
		switch {
		case !settingKeyPattern.MatchString(s.Key):
			fail("key must be 1-32 characters of a-z, 0-9 or '_', starting with a letter")
		case seen[s.Key]:
			fail("duplicate key")
		}
		seen[s.Key] = true
		if strings.TrimSpace(s.Label) == "" || utf8.RuneCountInString(s.Label) > MaxSettingLabelLen || hasHiddenRunes(s.Label) {
			fail("label is required, at most %d characters, with no control characters", MaxSettingLabelLen)
		}
		if utf8.RuneCountInString(s.Help) > MaxSettingHelpLen || hasHiddenRunes(s.Help) {
			fail("help must be at most %d characters with no control characters", MaxSettingHelpLen)
		}
		switch s.Type {
		case SettingBool:
			if _, ok := s.Default.(bool); s.Default != nil && !ok {
				fail("bool default must be true or false")
			}
			if len(s.Options) > 0 {
				fail("options are only for select settings")
			}
		case SettingSelect:
			if len(s.Options) == 0 || len(s.Options) > MaxSelectOptions {
				fail("select needs 1-%d options", MaxSelectOptions)
			}
			optSeen := map[string]bool{}
			for _, o := range s.Options {
				if o == "" || utf8.RuneCountInString(o) > MaxSelectOptionLen || hasHiddenRunes(o) || optSeen[o] {
					fail("option %q must be unique, non-empty, and at most %d characters", o, MaxSelectOptionLen)
				}
				optSeen[o] = true
			}
			if s.Default != nil {
				d, ok := s.Default.(string)
				if !ok || !optSeen[d] {
					fail("default must be one of the options")
				}
			}
		case SettingSecret:
			if s.Default != nil {
				fail("a secret cannot have a default value")
			}
			if len(s.Options) > 0 {
				fail("options are only for select settings")
			}
		default:
			fail("unsupported type %q (use bool, select, or secret)", s.Type)
		}
	}
	return errs
}

func validateCapabilities(caps []string) []error {
	var errs []error
	seen := map[string]bool{}
	for _, c := range caps {
		switch {
		case !knownCapabilities[c]:
			errs = append(errs, fmt.Errorf("unknown capability %q (supported: %s)", c, CapabilityTest))
		case seen[c]:
			errs = append(errs, fmt.Errorf("capability %q is listed twice", c))
		}
		seen[c] = true
	}
	return errs
}

// HasCapability reports whether the manifest declares capability.
func (m Manifest) HasCapability(capability string) bool {
	for _, c := range m.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// Setting returns the declared setting with key.
func (m Manifest) Setting(key string) (SettingSpec, bool) {
	for _, s := range m.Settings {
		if s.Key == key {
			return s, true
		}
	}
	return SettingSpec{}, false
}

// DefaultValue is the value a non-secret setting has when nothing is stored:
// the manifest default, or false / the first option.
func (s SettingSpec) DefaultValue() any {
	switch s.Type {
	case SettingBool:
		b, _ := s.Default.(bool)
		return b
	case SettingSelect:
		if d, ok := s.Default.(string); ok {
			return d
		}
		if len(s.Options) > 0 {
			return s.Options[0]
		}
	}
	return nil
}

// Resolve returns the effective value for a stored raw value: the stored one
// when it has the right type (and, for a select, is still an option), else the
// default. Secrets never resolve here.
func (s SettingSpec) Resolve(stored any) any {
	switch s.Type {
	case SettingBool:
		if b, ok := stored.(bool); ok {
			return b
		}
	case SettingSelect:
		if v, ok := stored.(string); ok {
			for _, o := range s.Options {
				if o == v {
					return v
				}
			}
		}
	case SettingSecret:
		return nil
	}
	return s.DefaultValue()
}

// ResolveSettings returns the plugin's effective non-secret settings from its
// stored values. Only declared keys appear: stale or foreign keys are
// dropped, so a plugin only ever sees its own declared settings.
func ResolveSettings(specs []SettingSpec, stored map[string]any) map[string]any {
	out := map[string]any{}
	for _, s := range specs {
		if s.Type == SettingSecret {
			continue
		}
		out[s.Key] = s.Resolve(stored[s.Key])
	}
	return out
}

// SecretEnvVar is the environment variable a secret setting reaches the
// plugin through, e.g. jev_api_key -> TIDEMAIL_SECRET_JEV_API_KEY.
func SecretEnvVar(key string) string {
	return "TIDEMAIL_SECRET_" + strings.ToUpper(key)
}

// SettingsSource supplies a plugin's stored settings and secrets when
// TideMail runs it. Implementations must be safe for concurrent use: automatic
// events call it from worker goroutines.
type SettingsSource interface {
	// StoredSettings returns the raw stored non-secret values for a plugin.
	StoredSettings(pluginID string) map[string]any
	// Secret returns one stored secret and whether it exists.
	Secret(pluginID, key string) (string, bool, error)
}
