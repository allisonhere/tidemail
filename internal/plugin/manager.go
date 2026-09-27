package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrUnknownPlugin is returned when a call names a plugin that was not
// discovered.
var ErrUnknownPlugin = errors.New("unknown plugin")

// Plugin is a discovered plugin with a valid manifest.
type Plugin struct {
	Manifest Manifest
	// Dir is the plugin's directory. The plugin runs with it as the working
	// directory.
	Dir string
	// Executable is the absolute path of the plugin command.
	Executable string
}

// DiscoveryError explains why one plugin directory was skipped.
type DiscoveryError struct {
	Dir string
	Err error
}

func (e DiscoveryError) Error() string {
	return fmt.Sprintf("plugin %s: %v", filepath.Base(e.Dir), e.Err)
}

func (e DiscoveryError) Unwrap() error { return e.Err }

// Manager holds the plugins found in one directory and runs them. A nil
// *Manager behaves as a manager with no plugins.
type Manager struct {
	// Timeout bounds each invocation. Zero means DefaultTimeout.
	Timeout time.Duration

	dir     string
	plugins []Plugin
	byID    map[string]int
	errs    []DiscoveryError
}

// Discover loads every plugin under dir, one plugin per subdirectory, in
// directory-name order. A missing dir yields an empty manager and no error.
// A broken plugin is skipped and reported in Errors without affecting its
// siblings; the returned error is only for a dir that exists but cannot be
// read. Discovery reads manifests and checks executables but runs nothing.
func Discover(dir string) (*Manager, error) {
	m := &Manager{dir: dir, byID: map[string]int{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return m, nil
		}
		return m, fmt.Errorf("read plugin directory: %w", err)
	}

	var found []Plugin
	for _, entry := range entries { // os.ReadDir sorts by name.
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		pluginDir := filepath.Join(dir, entry.Name())
		// Stat follows symlinks, so a symlinked plugin directory works.
		if info, err := os.Stat(pluginDir); err != nil || !info.IsDir() {
			continue
		}
		p, err := loadPlugin(pluginDir)
		if err != nil {
			m.errs = append(m.errs, DiscoveryError{Dir: pluginDir, Err: err})
			continue
		}
		found = append(found, p)
	}

	// A duplicated ID disables every copy; keeping the first would let
	// directory names decide which code runs.
	count := map[string]int{}
	for _, p := range found {
		count[p.Manifest.ID]++
	}
	for _, p := range found {
		if n := count[p.Manifest.ID]; n > 1 {
			m.errs = append(m.errs, DiscoveryError{
				Dir: p.Dir,
				Err: fmt.Errorf("duplicate plugin id %q (used by %d plugins)", p.Manifest.ID, n),
			})
			continue
		}
		m.byID[p.Manifest.ID] = len(m.plugins)
		m.plugins = append(m.plugins, p)
	}
	return m, nil
}

func loadPlugin(dir string) (Plugin, error) {
	manifest, err := LoadManifest(filepath.Join(dir, ManifestFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Plugin{}, fmt.Errorf("missing %s", ManifestFile)
		}
		return Plugin{}, err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return Plugin{}, err
	}
	// The manifest already guarantees Command is a local relative path.
	exe := filepath.Join(absDir, manifest.Command)
	info, err := os.Stat(exe)
	switch {
	case err != nil:
		return Plugin{}, fmt.Errorf("command %q: %w", manifest.Command, err)
	case !info.Mode().IsRegular():
		return Plugin{}, fmt.Errorf("command %q is not a regular file", manifest.Command)
	case info.Mode().Perm()&0o111 == 0:
		return Plugin{}, fmt.Errorf("command %q is not executable", manifest.Command)
	}
	return Plugin{Manifest: manifest, Dir: absDir, Executable: exe}, nil
}

// Dir returns the directory the manager discovered plugins in.
func (m *Manager) Dir() string {
	if m == nil {
		return ""
	}
	return m.dir
}

// Plugins returns the valid plugins in discovery order.
func (m *Manager) Plugins() []Plugin {
	if m == nil {
		return nil
	}
	return append([]Plugin(nil), m.plugins...)
}

// Plugin returns the plugin with the given ID.
func (m *Manager) Plugin(id string) (Plugin, bool) {
	if m == nil {
		return Plugin{}, false
	}
	i, ok := m.byID[id]
	if !ok {
		return Plugin{}, false
	}
	return m.plugins[i], true
}

// Errors returns the problems found during discovery, in directory order.
func (m *Manager) Errors() []DiscoveryError {
	if m == nil {
		return nil
	}
	return append([]DiscoveryError(nil), m.errs...)
}

// Call runs one request against a plugin. The manager fills in the envelope
// (API version, type, and a fresh request ID). A response with ok=false is
// returned together with its *Error.
//
// Methods that carry mail data have their own permission-checked entry points
// (MessageMetadata) and are refused here, so the check cannot be bypassed.
func (m *Manager) Call(ctx context.Context, pluginID, method string, data json.RawMessage) (Response, error) {
	if method == MethodMessageMetadata {
		return Response{}, fmt.Errorf("plugin %q: %s must go through MessageMetadata", pluginID, method)
	}
	p, ok := m.Plugin(pluginID)
	if !ok {
		return Response{}, fmt.Errorf("%w %q", ErrUnknownPlugin, pluginID)
	}
	return m.call(ctx, p, method, data)
}

// MessageMetadata sends one message's metadata to a plugin. It refuses,
// without starting the plugin, unless the manifest declares message_metadata.
//
// Annotations in the response reach store only when the manifest also
// declares annotations and the whole set validates; store is then given the
// complete set (possibly empty) to replace the plugin's previous annotations
// on the message. This is the only path from plugin output to storage. The
// rest of the response is for display only. A failed run returns an error and
// never touches store.
func (m *Manager) MessageMetadata(ctx context.Context, pluginID string, meta MessageMetadata, store AnnotationStore) (MessageMetadataResult, error) {
	p, ok := m.Plugin(pluginID)
	if !ok {
		return MessageMetadataResult{}, fmt.Errorf("%w %q", ErrUnknownPlugin, pluginID)
	}
	if !p.Manifest.Permissions.MessageMetadata {
		return MessageMetadataResult{}, fmt.Errorf("plugin %q: %w: message_metadata is not declared", pluginID, ErrPermissionDenied)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return MessageMetadataResult{}, fmt.Errorf("plugin %q: encode metadata: %w", pluginID, err)
	}
	resp, err := m.call(ctx, p, MethodMessageMetadata, data)
	if err != nil {
		return MessageMetadataResult{}, err
	}

	result := MessageMetadataResult{Response: resp}
	anns, parseErr := ParseAnnotations(resp.Data)
	switch {
	case !p.Manifest.Permissions.Annotations:
		result.Annotations = anns
		result.Outcome = AnnotationsNotPermitted
	case parseErr != nil:
		result.Outcome = AnnotationsRejected
		result.AnnotationErr = parseErr
	case store == nil:
		result.Annotations = anns
		result.Outcome = AnnotationsNotStored
		result.AnnotationErr = errors.New("no annotation store")
	default:
		result.Annotations = anns
		if err := store.ReplaceAnnotations(pluginID, meta.ID, anns); err != nil {
			result.Outcome = AnnotationsNotStored
			result.AnnotationErr = err
		} else {
			result.Outcome = AnnotationsStored
		}
	}
	return result, nil
}

func (m *Manager) call(ctx context.Context, p Plugin, method string, data json.RawMessage) (Response, error) {
	pluginID := p.Manifest.ID
	req, err := NewRequest(method, data)
	if err != nil {
		return Response{}, err
	}
	resp, err := invoke(ctx, p, req, m.timeout())
	if err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("plugin %q: %w", pluginID, resp.Error)
	}
	return resp, nil
}

// Ping calls the ping method and returns the plugin's message, normally
// "pong".
func (m *Manager) Ping(ctx context.Context, pluginID string) (string, error) {
	resp, err := m.Call(ctx, pluginID, MethodPing, nil)
	if err != nil {
		return "", err
	}
	var result PingResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("plugin %q: malformed ping data: %w", pluginID, err)
	}
	return result.Message, nil
}

func (m *Manager) timeout() time.Duration {
	if m.Timeout > 0 {
		return m.Timeout
	}
	return DefaultTimeout
}
