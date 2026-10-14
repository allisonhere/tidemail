package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

const sampleChangelog = "# Changelog\nPreamble that should be dropped.\n\n## Unreleased\n\n## v2.0.0\n\n### Added\n\n" +
	"- A **bold** feature with `code`\n  that continues here.\n- Second item\n\n### Fixed\n\n- A fix\n\n## v1.0.0\n\n### Added\n\n- Old thing\n"

func TestParseChangelog(t *testing.T) {
	blocks := parseChangelog(sampleChangelog)
	var got []string
	for _, b := range blocks {
		got = append(got, fmt.Sprintf("%d:%s", b.kind, b.text))
	}
	want := []string{
		"0:v2.0.0", "1:Added", "2:A bold feature with code that continues here.", "2:Second item",
		"1:Fixed", "2:A fix", "0:v1.0.0", "1:Added", "2:Old thing",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("parseChangelog =\n%v\nwant\n%v", got, want)
	}
}

func TestChangelogSectionRegisteredAndRendered(t *testing.T) {
	SetChangelog(sampleChangelog)
	t.Cleanup(func() { SetChangelog("") })
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	if got := settingsSectionLabels[ssChangelog]; got != "CHANGELOG" {
		t.Fatalf("label = %q", got)
	}
	s.setActiveSection(ssChangelog)
	view := ansi.Strip(s.View(100, 30, newManagerChrome(100, CatppuccinMocha, false)))
	for _, want := range []string{"v2.0.0", "Added", "A bold feature with code", "v1.0.0"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Changelog view missing %q: %q", want, view)
		}
	}
	if strings.Contains(view, "Unreleased") {
		t.Fatalf("empty Unreleased section should be hidden: %q", view)
	}
}

func TestChangelogEmptyBuildShowsNotice(t *testing.T) {
	SetChangelog("")
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssChangelog)
	view := ansi.Strip(s.View(100, 20, newManagerChrome(100, CatppuccinMocha, false)))
	if !strings.Contains(view, "No changelog is bundled") {
		t.Fatalf("missing empty notice: %q", view)
	}
}

func TestChangelogScrolling(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("## v9.9.9\n\n### Added\n\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&sb, "- entry number %02d\n", i)
	}
	SetChangelog(sb.String())
	t.Cleanup(func() { SetChangelog("") })

	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssChangelog)
	s.setFocusedPane(settingsPaneDetail)
	chrome := newManagerChrome(100, CatppuccinMocha, false)
	render := func() string { return ansi.Strip(s.View(100, 20, chrome)) }
	press := func(k tea.KeyMsg) {
		s, _, _ = s.Update(k, DefaultKeys)
	}

	if v := render(); !strings.Contains(v, "entry number 00") || strings.Contains(v, "entry number 59") {
		t.Fatalf("top of changelog wrong: %q", v)
	}
	press(tea.KeyMsg{Type: tea.KeyPgDown})
	if v := render(); strings.Contains(v, "entry number 00") || s.changelogScroll == 0 {
		t.Fatalf("PgDn did not scroll: scroll=%d", s.changelogScroll)
	}
	press(tea.KeyMsg{Type: tea.KeyEnd})
	if v := render(); !strings.Contains(v, "entry number 59") {
		t.Fatalf("End did not reach the last entry: %q", v)
	}
	atEnd := s.changelogScroll
	press(tea.KeyMsg{Type: tea.KeyDown})
	render()
	if s.changelogScroll != atEnd {
		t.Fatalf("scrolled past the end: %d > %d", s.changelogScroll, atEnd)
	}
	press(tea.KeyMsg{Type: tea.KeyHome})
	if render(); s.changelogScroll != 0 {
		t.Fatalf("Home did not return to top: %d", s.changelogScroll)
	}
	press(tea.KeyMsg{Type: tea.KeyUp})
	if render(); s.changelogScroll != 0 {
		t.Fatalf("scrolled above the top: %d", s.changelogScroll)
	}
}
