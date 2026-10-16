package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/plugincatalog"
	"github.com/allisonhere/tidemail/internal/update"
)

// Browsing, installing, updating and removing the official plugins. The
// catalog is fetched only when the user opens this view, and every change goes
// through plugincatalog, which downloads nothing from outside GitHub, checks
// the checksum, and never runs the plugin. After a change the plugin manager
// is rebuilt with LoadPlugins, so the plugin appears without a restart.

const (
	catalogRows        = 9
	catalogFetchTimout = 30 * time.Second
	catalogInstallTime = 5 * time.Minute
)

type catalogAction int

const (
	catalogInstall catalogAction = iota
	catalogUpdate
	catalogRemove
)

type catalogUI struct {
	// client is nil in production (the official catalog); tests inject one.
	client  *plugincatalog.Client
	entries []plugincatalog.Entry
	loading bool
	loaded  bool
	err     string
	cursor  int
	// confirm is the pending install, update or remove awaiting a yes.
	confirm *catalogConfirm
	// busy names the change in flight; keys are ignored until it finishes.
	busy    string
	message string
	// origin is where Esc returns to.
	origin overlayMode
}

type catalogConfirm struct {
	action catalogAction
	entry  plugincatalog.Entry
}

// CatalogLoadedMsg carries the fetched catalog.
type CatalogLoadedMsg struct {
	Entries []plugincatalog.Entry
	Err     error
}

// CatalogChangedMsg reports a finished install, update or remove.
type CatalogChangedMsg struct {
	ID     string
	Name   string
	Action catalogAction
	Err    error
}

// catalogRow is one catalog entry joined with what is installed.
type catalogRow struct {
	entry         plugincatalog.Entry
	installed     bool
	installedVer  string
	installedDir  string
	updateOffered bool
}

func (m Model) catalogRowsList() []catalogRow {
	rows := make([]catalogRow, 0, len(m.catalog.entries))
	for _, e := range m.catalog.entries {
		r := catalogRow{entry: e}
		if p, ok := m.plugins.manager.Plugin(e.ID); ok {
			r.installed, r.installedVer, r.installedDir = true, p.Manifest.Version, p.Dir
			r.updateOffered = update.IsNewerVersion(e.Version, p.Manifest.Version) && e.Available()
		}
		rows = append(rows, r)
	}
	return rows
}

func (m *Model) openPluginCatalog(origin overlayMode) tea.Cmd {
	m.catalog.origin = origin
	m.catalog.confirm = nil
	m.catalog.message = ""
	m.overlay = overlayPluginCatalog
	return m.fetchCatalog()
}

func (m *Model) fetchCatalog() tea.Cmd {
	if m.catalog.loading {
		return nil
	}
	m.catalog.loading, m.catalog.err = true, ""
	client := m.catalog.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), catalogFetchTimout)
		defer cancel()
		cat, err := client.Fetch(ctx)
		return CatalogLoadedMsg{Entries: cat.Plugins, Err: err}
	}
}

func (m Model) handleCatalogLoaded(msg CatalogLoadedMsg) (tea.Model, tea.Cmd) {
	m.catalog.loading = false
	if msg.Err != nil {
		m.catalog.err = sanitizePluginLine(msg.Err.Error())
		return m, nil
	}
	m.catalog.loaded = true
	m.catalog.entries = msg.Entries
	m.catalog.cursor = clamp(m.catalog.cursor, 0, max(0, len(msg.Entries)-1))
	return m, nil
}

func (m Model) handleCatalogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := &m.catalog
	if c.busy != "" {
		return m, nil
	}
	if c.confirm != nil {
		switch {
		case keyMatches(msg, m.keys.Confirm, m.keys.Yes):
			return m.runCatalogChange(*c.confirm)
		case keyMatches(msg, m.keys.Cancel, m.keys.No, m.keys.Back):
			c.confirm = nil
		}
		return m, nil
	}
	rows := m.catalogRowsList()
	var row *catalogRow
	if len(rows) > 0 {
		row = &rows[clamp(c.cursor, 0, len(rows)-1)]
	}
	switch {
	case keyMatches(msg, m.keys.Cancel, m.keys.Back):
		m.overlay = c.origin
	case keyMatches(msg, m.keys.Up):
		if n := len(rows); n > 0 {
			c.cursor = (c.cursor - 1 + n) % n
		}
	case keyMatches(msg, m.keys.Down):
		if n := len(rows); n > 0 {
			c.cursor = (c.cursor + 1) % n
		}
	case msg.String() == "r":
		return m, m.fetchCatalog()
	case row == nil:
	case msg.String() == "i" || msg.Type == tea.KeyEnter:
		switch {
		case !row.entry.Available():
			c.message = fmt.Sprintf("%s has no build for %s yet", row.entry.Name, plugincatalog.Platform())
		case row.updateOffered:
			c.confirm = &catalogConfirm{action: catalogUpdate, entry: row.entry}
		case row.installed:
			c.message = row.entry.Name + " is already installed"
		default:
			c.confirm = &catalogConfirm{action: catalogInstall, entry: row.entry}
		}
	case msg.String() == "u" && row.updateOffered:
		c.confirm = &catalogConfirm{action: catalogUpdate, entry: row.entry}
	case msg.String() == "x" && row.installed:
		c.confirm = &catalogConfirm{action: catalogRemove, entry: row.entry}
	}
	return m, nil
}

// runCatalogChange performs a confirmed install, update or remove in the
// background. Changes wait for any running plugin so its files are not swapped
// out from under it.
func (m Model) runCatalogChange(cf catalogConfirm) (tea.Model, tea.Cmd) {
	c := &m.catalog
	if m.plugins.running != "" || m.plugins.bulk != nil {
		c.confirm = nil
		c.message = "A plugin is running; try again when it finishes"
		return m, nil
	}
	root, err := config.PluginDir()
	if err != nil {
		c.confirm = nil
		c.message = "Can't find the plugins directory: " + sanitizePluginLine(err.Error())
		return m, nil
	}
	var replace string
	if p, ok := m.plugins.manager.Plugin(cf.entry.ID); ok {
		replace = p.Dir
	}
	c.confirm = nil
	client := c.client
	entry := cf.entry
	switch cf.action {
	case catalogRemove:
		c.busy = "Removing " + entry.Name + "…"
	case catalogUpdate:
		c.busy = "Updating " + entry.Name + "…"
	default:
		c.busy = "Installing " + entry.Name + "…"
	}
	action := cf.action
	return m, func() tea.Msg {
		done := CatalogChangedMsg{ID: entry.ID, Name: entry.Name, Action: action}
		if action == catalogRemove {
			done.Err = plugincatalog.Remove(replace, root)
			return done
		}
		ctx, cancel := context.WithTimeout(context.Background(), catalogInstallTime)
		defer cancel()
		_, done.Err = client.Install(ctx, entry, root, replace)
		return done
	}
}

func (m Model) handleCatalogChanged(msg CatalogChangedMsg) (tea.Model, tea.Cmd) {
	m.catalog.busy = ""
	if msg.Err != nil {
		verb := map[catalogAction]string{catalogInstall: "install", catalogUpdate: "update", catalogRemove: "remove"}[msg.Action]
		m.catalog.message = fmt.Sprintf("Couldn't %s %s: %s", verb, msg.Name, sanitizePluginLine(msg.Err.Error()))
		return m, nil
	}
	m.LoadPlugins()
	m.bodyCache.clear()
	m.viewportCache.clear()
	m.settings.pluginsAvailable = m.pluginsVisible()
	m.settings.pluginPaneLoaded = false
	switch msg.Action {
	case catalogRemove:
		m.catalog.message = msg.Name + " removed. Its settings and stored tags are kept."
	case catalogUpdate:
		m.catalog.message = msg.Name + " updated"
	default:
		m.catalog.message = msg.Name + " installed. Turn on automatic processing from the plugin list if it offers it."
	}
	return m, nil
}

// ── Rendering ────────────────────────────────────────────────────────────────

func (m Model) renderPluginCatalog(width int, chrome managerChrome) string {
	c := m.catalog
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	text := base.Foreground(chrome.text)
	muted := base.Foreground(chrome.muted)
	errStyle := base.Foreground(chrome.errorFg)
	inner := max(1, width-4)

	var rows []string
	wrap := func(style lipgloss.Style, s string) {
		for _, line := range strings.Split(ansi.Wrap(s, inner, ""), "\n") {
			rows = append(rows, style.Render(line))
		}
	}

	if c.confirm != nil {
		wrap(text.Bold(true), catalogConfirmTitle(*c.confirm))
		rows = append(rows, "")
		wrap(muted, sanitizePluginLine(c.confirm.entry.Description))
		if c.confirm.action != catalogRemove {
			rows = append(rows, "")
			wrap(text, "It will be able to:")
			for _, line := range permissionChecklist(c.confirm.entry.Permissions, m.styles.PlainUI) {
				wrap(text, "  "+line)
			}
			rows = append(rows, "")
			wrap(muted, "Downloaded from GitHub and checked against the release's checksum. Plugins run with your user's privileges and are not sandboxed, so only install ones you trust.")
			if p, ok := m.plugins.manager.Plugin(c.confirm.entry.ID); ok {
				rows = append(rows, "")
				if p.Manifest.Permissions != c.confirm.entry.Permissions {
					wrap(errStyle, "This version asks for different permissions than the one you have.")
				}
				wrap(muted, "It replaces the copy in "+p.Dir+".")
			}
		} else {
			rows = append(rows, "")
			wrap(muted, "Your mail is untouched. The plugin's settings and the tags it stored are kept.")
		}
		body := base.Width(width).Padding(1, 2).Render(strings.Join(rows, "\n"))
		return lipgloss.JoinVertical(lipgloss.Left, body, renderSoftHints(width, chrome, "enter", "confirm", "esc", "cancel"))
	}

	switch {
	case c.loading && !c.loaded:
		wrap(muted, "Loading the plugin list from GitHub…")
	case c.err != "" && !c.loaded:
		wrap(errStyle, "Couldn't load the plugin list: "+c.err)
		wrap(muted, "Press r to try again.")
	default:
		list := m.catalogRowsList()
		if len(list) == 0 {
			wrap(muted, "No plugins are published yet.")
			break
		}
		limit := min(catalogRows, len(list))
		start := clamp(c.cursor-limit/2, 0, len(list)-limit)
		labelW := max(1, width-4-2)
		for i := start; i < start+limit; i++ {
			rows = append(rows, m.catalogRowLine(list[i], i == c.cursor, labelW, chrome))
		}
		sel := list[clamp(c.cursor, 0, len(list)-1)]
		rows = append(rows, "")
		wrap(muted, sanitizePluginLine(sel.entry.Description))
		if sel.entry.Homepage != "" {
			wrap(muted, sel.entry.Homepage)
		}
	}
	if c.busy != "" {
		rows = append(rows, "", text.Render(c.busy))
	} else if c.message != "" {
		rows = append(rows, "")
		wrap(muted, c.message)
	} else if c.err != "" && c.loaded {
		rows = append(rows, "")
		wrap(errStyle, "Couldn't refresh: "+c.err)
	}
	body := base.Width(width).Padding(1, 2).Render(strings.Join(rows, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, body, renderSoftHints(width, chrome, m.catalogHintPairs()...))
}

func (m Model) catalogRowLine(r catalogRow, selected bool, labelW int, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	status := ""
	switch {
	case !r.entry.Available():
		status = "not available for " + plugincatalog.Platform()
	case r.updateOffered:
		status = "update from " + r.installedVer
	case r.installed:
		status = "installed"
	}
	label := fmt.Sprintf(" %s  %s", sanitizePluginLine(r.entry.Name), r.entry.Version)
	line := base.Foreground(chrome.text).Render(label)
	if status != "" {
		line += base.Foreground(chrome.muted).Render("  " + status)
	}
	return softRail(chrome, selected, chrome.baseBg) + padStyled(truncateStyled(line, labelW, chrome.baseBg), labelW, chrome.baseBg)
}

func (m Model) catalogHintPairs() []string {
	pairs := []string{"↑↓", "select"}
	rows := m.catalogRowsList()
	if len(rows) > 0 {
		r := rows[clamp(m.catalog.cursor, 0, len(rows)-1)]
		switch {
		case r.updateOffered:
			pairs = append(pairs, "u", "update")
		case !r.installed && r.entry.Available():
			pairs = append(pairs, "i", "install")
		}
		if r.installed {
			pairs = append(pairs, "x", "remove")
		}
	}
	return append(pairs, "r", "refresh", "esc", "back")
}

func catalogConfirmTitle(cf catalogConfirm) string {
	name := sanitizePluginLine(cf.entry.Name)
	switch cf.action {
	case catalogRemove:
		return "Remove " + name + "?"
	case catalogUpdate:
		return fmt.Sprintf("Update %s to %s?", name, cf.entry.Version)
	}
	return fmt.Sprintf("Install %s %s?", name, cf.entry.Version)
}
