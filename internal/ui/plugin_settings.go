package ui

// Generic plugin settings: a form rendered from a plugin's declared
// [[settings]] (bool, select, secret), plus its automatic-events switch and,
// when declared, "Test plugin configuration". Non-secret values are saved in
// config.toml; secrets go to the system keychain and the model only ever
// knows whether one is set.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// secretMask is how a stored secret is displayed.
const secretMask = "************"

// pluginSecretStore is where plugin secrets live. Tests replace it; the
// default is the system keychain with no plaintext fallback.
type pluginSecretStore interface {
	Get(pluginID, key string) (string, bool)
	Set(pluginID, key, value string) error
	Delete(pluginID, key string) error
}

type keyringPluginSecrets struct{}

func (keyringPluginSecrets) Get(pluginID, key string) (string, bool) {
	return config.GetPluginSecret(pluginID, key)
}

func (keyringPluginSecrets) Set(pluginID, key, value string) error {
	return config.StorePluginSecret(pluginID, key, value)
}

func (keyringPluginSecrets) Delete(pluginID, key string) error {
	return config.DeletePluginSecret(pluginID, key)
}

var pluginSecrets pluginSecretStore = keyringPluginSecrets{}

// pluginSettingsSource gives the plugin manager each plugin's stored
// settings. It holds a copy of the non-secret values, updated from Update,
// because automatic events read it from worker goroutines.
type pluginSettingsSource struct {
	mu     sync.RWMutex
	stored map[string]map[string]any
}

func (s *pluginSettingsSource) StoredSettings(pluginID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]any, len(s.stored[pluginID]))
	for k, v := range s.stored[pluginID] {
		out[k] = v
	}
	return out
}

func (s *pluginSettingsSource) Secret(pluginID, key string) (string, bool, error) {
	v, ok := pluginSecrets.Get(pluginID, key)
	return v, ok, nil
}

func (s *pluginSettingsSource) update(cfg config.Config) {
	next := map[string]map[string]any{}
	for id := range cfg.Plugins {
		next[id] = cfg.PluginStoredSettings(id)
	}
	s.mu.Lock()
	s.stored = next
	s.mu.Unlock()
}

// Messages for the settings form's background work.
type (
	pluginSecretPresenceMsg struct {
		PluginID string
		Set      map[string]bool
	}
	pluginSecretSavedMsg struct {
		PluginID string
		Key      string
		Cleared  bool
		Err      error
	}
	pluginTestResultMsg struct {
		PluginID string
		Response plugin.Response
		Err      error
	}
)

// pluginSettingsState is the settings form's state.
type pluginSettingsState struct {
	pluginID  string
	cursor    int
	secretSet map[string]bool
	editing   bool // entering a secret
	input     textinput.Model
	testing   bool
	origin    overlayMode
}

// settingsRowKind is one row of the form.
type settingsRowKind int

const (
	rowAutoEvents settingsRowKind = iota
	rowSetting
	rowTest
)

type settingsRow struct {
	kind settingsRowKind
	spec plugin.SettingSpec
}

// hasPluginSettingsPage reports whether a plugin has anything to configure:
// declared settings, automatic events, or a configuration test.
func (m Model) hasPluginSettingsPage(pluginID string) bool {
	return len(m.pluginSettingsRows(pluginID)) > 0
}

func (m Model) pluginSettingsRows(pluginID string) []settingsRow {
	p, ok := m.plugins.manager.Plugin(pluginID)
	if !ok {
		return nil
	}
	var rows []settingsRow
	if plugin.EventCapable(p) && m.plugins.events != nil {
		rows = append(rows, settingsRow{kind: rowAutoEvents})
	}
	for _, s := range p.Manifest.Settings {
		rows = append(rows, settingsRow{kind: rowSetting, spec: s})
	}
	if p.Manifest.HasCapability(plugin.CapabilityTest) {
		rows = append(rows, settingsRow{kind: rowTest})
	}
	return rows
}

// openPluginSettings opens the form and checks which secrets are set in the
// background (the keychain can be slow).
func (m Model) openPluginSettings(pluginID string) (Model, tea.Cmd) {
	if !m.hasPluginSettingsPage(pluginID) {
		return m, nil
	}
	m.plugins.settings = pluginSettingsState{pluginID: pluginID, origin: m.overlay}
	m.overlay = overlayPluginSettings
	return m, loadSecretPresenceCmd(m.plugins.manager, pluginID)
}

func loadSecretPresenceCmd(mgr *plugin.Manager, pluginID string) tea.Cmd {
	p, ok := mgr.Plugin(pluginID)
	if !ok {
		return nil
	}
	var keys []string
	for _, s := range p.Manifest.Settings {
		if s.Type == plugin.SettingSecret {
			keys = append(keys, s.Key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return func() tea.Msg {
		set := map[string]bool{}
		for _, k := range keys {
			_, ok := pluginSecrets.Get(pluginID, k)
			set[k] = ok
		}
		return pluginSecretPresenceMsg{PluginID: pluginID, Set: set}
	}
}

// saveSecretCmd stores or clears a secret off the Update loop. The value is
// captured here only; the result message carries no secret.
func saveSecretCmd(pluginID, key, value string, clear bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if clear {
			err = pluginSecrets.Delete(pluginID, key)
		} else {
			err = pluginSecrets.Set(pluginID, key, value)
		}
		return pluginSecretSavedMsg{PluginID: pluginID, Key: key, Cleared: clear, Err: err}
	}
}

func testPluginCmd(ctx context.Context, mgr *plugin.Manager, pluginID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := mgr.Test(ctx, pluginID)
		return pluginTestResultMsg{PluginID: pluginID, Response: resp, Err: err}
	}
}

func (m Model) handlePluginSettingsMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	st := &m.plugins.settings
	switch msg := msg.(type) {
	case pluginSecretPresenceMsg:
		if msg.PluginID == st.pluginID {
			st.secretSet = msg.Set
		}
		return m, nil
	case pluginSecretSavedMsg:
		if msg.Err != nil {
			m.setStatus("plugin secret not saved: "+sanitizePluginLine(msg.Err.Error()), true)
			return m, m.clearStatusCmd()
		}
		if msg.PluginID == st.pluginID {
			if st.secretSet == nil {
				st.secretSet = map[string]bool{}
			}
			st.secretSet[msg.Key] = !msg.Cleared
		}
		verb := "saved"
		if msg.Cleared {
			verb = "cleared"
		}
		m.setStatus(fmt.Sprintf("%s for plugin %s %s", msg.Key, msg.PluginID, verb), false)
		return m, m.clearStatusCmd()
	case pluginTestResultMsg:
		if msg.PluginID == st.pluginID {
			st.testing = false
		}
		if msg.Err != nil {
			m.setStatus("plugin test failed: "+sanitizePluginLine(msg.Err.Error()), true)
			return m, m.clearStatusCmd()
		}
		summary := pluginTestSummary(msg.Response)
		m.plugins.result = &pluginResult{
			pluginID: msg.PluginID, pluginName: m.pluginDisplayName(msg.PluginID),
			subject: "configuration test", body: formatPluginData(msg.Response.Data),
		}
		m.setStatus("plugin "+msg.PluginID+": "+summary, false)
		// Show details only if the user is still on this plugin's settings.
		if m.overlay == overlayPluginSettings && msg.PluginID == st.pluginID {
			m.plugins.scroll = 0
			m.overlay = overlayPluginResult
		}
		return m, m.clearStatusCmd()
	}
	return m, nil
}

// pluginTestSummary is the response's "message" field, sanitized, or a
// generic line.
func pluginTestSummary(resp plugin.Response) string {
	var data struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(resp.Data, &data); err == nil && strings.TrimSpace(data.Message) != "" {
		return truncate(sanitizePluginLine(data.Message), 160)
	}
	return "test completed"
}

func (m Model) handlePluginSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := &m.plugins.settings
	if st.editing {
		switch {
		case keyMatches(msg, m.keys.Cancel):
			st.editing = false
			st.input.Reset()
			st.input.Blur()
		case keyMatches(msg, m.keys.Confirm):
			value := strings.TrimSpace(st.input.Value())
			st.editing = false
			st.input.Reset() // drop the typed secret from model state
			st.input.Blur()
			rows := m.pluginSettingsRows(st.pluginID)
			if value == "" || st.cursor >= len(rows) {
				return m, nil
			}
			return m, saveSecretCmd(st.pluginID, rows[st.cursor].spec.Key, value, false)
		default:
			var cmd tea.Cmd
			st.input, cmd = st.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	rows := m.pluginSettingsRows(st.pluginID)
	switch {
	case keyMatches(msg, m.keys.Cancel, m.keys.Back):
		m.overlay = st.origin
		if m.overlay == overlayPluginSettings {
			m.overlay = overlayPlugins
		}
		return m, nil
	case keyMatches(msg, m.keys.Up):
		st.cursor = max(0, st.cursor-1)
		return m, nil
	case keyMatches(msg, m.keys.Down):
		st.cursor = min(len(rows)-1, st.cursor+1)
		return m, nil
	}
	if len(rows) == 0 {
		return m, nil
	}
	st.cursor = clamp(st.cursor, 0, len(rows)-1)
	row := rows[st.cursor]
	activate := keyMatches(msg, m.keys.Confirm, m.keys.Space)
	right := keyMatches(msg, m.keys.Right)
	left := keyMatches(msg, m.keys.Left)

	switch row.kind {
	case rowAutoEvents:
		if activate || left || right {
			if m.cfg.PluginAutoEvents(st.pluginID) {
				m.setAutoEvents(st.pluginID, false)
				return m, m.clearStatusCmd()
			}
			m.confirmPluginAction(pluginConfirmAction{kind: enableAutoEvents, pluginID: st.pluginID, label: m.pluginDisplayName(st.pluginID)})
		}
	case rowTest:
		if activate && !st.testing {
			st.testing = true
			m.setStatus("testing plugin "+st.pluginID+"…", false)
			return m, testPluginCmd(m.plugins.ctx, m.plugins.manager, st.pluginID)
		}
	case rowSetting:
		s := row.spec
		switch s.Type {
		case plugin.SettingBool:
			if activate || left || right {
				current, _ := m.pluginSettingValue(st.pluginID, s).(bool)
				m.savePluginSetting(st.pluginID, s.Key, !current)
			}
		case plugin.SettingSelect:
			if activate || right || left {
				step := 1
				if left {
					step = -1
				}
				current, _ := m.pluginSettingValue(st.pluginID, s).(string)
				idx := 0
				for i, o := range s.Options {
					if o == current {
						idx = i
					}
				}
				next := s.Options[(idx+step+len(s.Options))%len(s.Options)]
				m.savePluginSetting(st.pluginID, s.Key, next)
			}
		case plugin.SettingSecret:
			switch {
			case activate:
				in := textinput.New()
				in.EchoMode = textinput.EchoPassword
				in.EchoCharacter = '*'
				in.Placeholder = "paste value, enter to save"
				in.CharLimit = 512
				in.Focus()
				st.input = in
				st.editing = true
			case msg.String() == "x" && st.secretSet[s.Key]:
				return m, saveSecretCmd(st.pluginID, s.Key, "", true)
			}
		}
	}
	return m, nil
}

// pluginSettingValue is a non-secret setting's effective value.
func (m Model) pluginSettingValue(pluginID string, s plugin.SettingSpec) any {
	return s.Resolve(m.cfg.PluginStoredSettings(pluginID)[s.Key])
}

// savePluginSetting stores a non-secret value in config.toml and hands the
// new values to the settings source the plugin manager reads.
func (m *Model) savePluginSetting(pluginID, key string, value any) {
	previous := m.cfg
	m.cfg.SetPluginSetting(pluginID, key, value)
	if err := m.saveConfig(); err != nil {
		m.cfg = previous
		return
	}
	m.plugins.settingsSrc.update(m.cfg)
}

func (m Model) renderPluginSettings() string {
	winW := max(1, min(m.width-4, 76))
	winH := max(1, min(m.height-4, 30))
	chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
	st := m.plugins.settings
	rows := m.pluginSettingsRows(st.pluginID)
	bodyW := max(1, winW-4)
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	text := base.Foreground(chrome.text)
	muted := base.Foreground(chrome.muted)
	labelW := min(28, max(10, bodyW/2))

	var lines []string
	lines = append(lines, base.Foreground(chrome.accent).Bold(true).Render(truncate(m.pluginDisplayName(st.pluginID)+"  ("+st.pluginID+")", bodyW)), "")
	for i, row := range rows {
		label, value := m.settingsRowText(st.pluginID, row)
		line := padRight(truncate(label, labelW), labelW) + "  " + truncate(value, max(1, bodyW-2-labelW-2))
		lines = append(lines, softRail(chrome, i == st.cursor, chrome.baseBg)+text.Render(line))
	}
	lines = append(lines, "")
	if st.editing {
		in := st.input
		in.Width = max(1, bodyW-4)
		in.PromptStyle = base.Foreground(chrome.accent)
		in.TextStyle = text
		in.PlaceholderStyle = muted
		lines = append(lines, text.Render("new value: ")+in.View())
	} else if st.cursor < len(rows) && rows[st.cursor].kind == rowSetting && rows[st.cursor].spec.Help != "" {
		for _, part := range strings.Split(ansi.Wrap(sanitizePluginLine(rows[st.cursor].spec.Help), bodyW, ""), "\n") {
			lines = append(lines, muted.Render(part))
		}
	}

	pairs := []string{"↑↓", "select", "enter", "change", "esc", "back"}
	if st.editing {
		pairs = []string{"enter", "save", "esc", "cancel"}
	} else if st.cursor < len(rows) && rows[st.cursor].kind == rowSetting && rows[st.cursor].spec.Type == plugin.SettingSecret && st.secretSet[rows[st.cursor].spec.Key] {
		pairs = []string{"↑↓", "select", "enter", "replace", "x", "clear", "esc", "back"}
	}
	inner := m.renderPluginScroll(lines, winW, winH, chrome, pairs...)
	inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
	return renderSoftPanelBox(inner, winW, "tidemail", "plugin settings", chrome)
}

// settingsRowText is a row's label and displayed value. Secrets show only
// whether they are set.
func (m Model) settingsRowText(pluginID string, row settingsRow) (string, string) {
	onOff := func(b bool) string {
		if b {
			return "[on]"
		}
		return "[off]"
	}
	switch row.kind {
	case rowAutoEvents:
		v := onOff(m.cfg.PluginAutoEvents(pluginID))
		if m.plugins.eventStatus[pluginID].Paused {
			v += " paused"
		}
		return "Auto-process new mail", v
	case rowTest:
		if m.plugins.settings.testing {
			return "Test plugin configuration", "testing…"
		}
		return "Test plugin configuration", "enter to run"
	}
	s := row.spec
	label := sanitizePluginLine(s.Label)
	switch s.Type {
	case plugin.SettingBool:
		b, _ := m.pluginSettingValue(pluginID, s).(bool)
		return label, onOff(b)
	case plugin.SettingSelect:
		v, _ := m.pluginSettingValue(pluginID, s).(string)
		return label, "‹ " + sanitizePluginLine(v) + " ›"
	case plugin.SettingSecret:
		set, known := m.plugins.settings.secretSet[s.Key]
		switch {
		case !known:
			return label, "…"
		case set:
			return label, secretMask
		default:
			return label, "not set"
		}
	}
	return label, ""
}
