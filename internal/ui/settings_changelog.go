package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Settings → Changelog: a read-only, scrollable view of CHANGELOG.md. The
// text is embedded into the binary by main (see SetChangelog), so it always
// matches the running version and works offline.

var changelogSource string

// SetChangelog supplies the CHANGELOG.md text shown in Settings → Changelog.
func SetChangelog(markdown string) { changelogSource = markdown }

type changelogKind int

const (
	changelogRelease changelogKind = iota
	changelogGroup
	changelogItem
)

type changelogBlock struct {
	kind changelogKind
	text string
}

// parseChangelog flattens the Keep-a-Changelog style markdown into release
// headings, group headings, and bullet items. The preamble before the first
// "## " heading is dropped, and releases with no entries (an empty
// "## Unreleased") are omitted.
func parseChangelog(src string) []changelogBlock {
	var blocks []changelogBlock
	started := false
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		switch {
		case strings.HasPrefix(line, "## "):
			started = true
			blocks = append(blocks, changelogBlock{changelogRelease, cleanChangelogInline(line[3:])})
		case !started, strings.TrimSpace(line) == "":
		case strings.HasPrefix(line, "### "):
			blocks = append(blocks, changelogBlock{changelogGroup, cleanChangelogInline(line[4:])})
		case strings.HasPrefix(line, "- "):
			blocks = append(blocks, changelogBlock{changelogItem, cleanChangelogInline(line[2:])})
		case len(blocks) > 0 && blocks[len(blocks)-1].kind == changelogItem && strings.HasPrefix(line, " "):
			last := &blocks[len(blocks)-1]
			last.text += " " + cleanChangelogInline(strings.TrimSpace(line))
		}
	}
	out := blocks[:0:0]
	for i, b := range blocks {
		if b.kind == changelogRelease && (i+1 == len(blocks) || blocks[i+1].kind == changelogRelease) {
			continue
		}
		out = append(out, b)
	}
	return out
}

func cleanChangelogInline(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(s)
}

func (s Settings) renderChangelogSection(width int, chrome managerChrome) settingsSectionBody {
	bg := chrome.baseBg
	line := func(text string, fg lipgloss.Color, bold bool) string {
		st := lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(bold)
		return padStyled(st.Render(text), width, bg)
	}
	blank := padStyled("", width, bg)

	blocks := parseChangelog(changelogSource)
	if len(blocks) == 0 {
		return settingsSectionBody{lines: []string{line("No changelog is bundled with this build.", chrome.muted, false)}}
	}

	var lines []string
	for _, b := range blocks {
		switch b.kind {
		case changelogRelease:
			if len(lines) > 0 {
				lines = append(lines, blank)
			}
			lines = append(lines, line("  "+b.text, chrome.accent, true))
		case changelogGroup:
			lines = append(lines, blank, line("  "+b.text, chrome.muted, true))
		case changelogItem:
			wrapped := strings.Split(wrapWords(b.text, max(1, width-6)), "\n")
			for i, part := range wrapped {
				prefix := "  "
				if i == 0 {
					prefix = "• "
				}
				lines = append(lines, line("    "+prefix+part, chrome.text, false))
			}
		}
	}
	return settingsSectionBody{lines: lines}
}

// handleChangelogKey scrolls the changelog; it reports whether it consumed the key.
func (s *Settings) handleChangelogKey(key string) bool {
	page := max(1, s.changelogPage-1)
	switch key {
	case "up", "k":
		s.changelogScroll--
	case "down", "j":
		s.changelogScroll++
	case "pgup", "ctrl+u":
		s.changelogScroll -= page
	case "pgdown", "ctrl+d", " ":
		s.changelogScroll += page
	case "home", "g":
		s.changelogScroll = 0
	case "end", "G":
		s.changelogScroll = 1 << 30 // clamped to the last page when drawn
	default:
		return false
	}
	s.changelogScroll = max(0, s.changelogScroll)
	return true
}
