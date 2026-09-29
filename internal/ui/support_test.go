package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

func TestSupportMetadataUsesConfirmedProviders(t *testing.T) {
	if len(supportLinks) != 1 {
		t.Fatalf("supportLinks has %d providers; want only the confirmed Ko-fi provider", len(supportLinks))
	}
	link := supportLinks[0]
	if link.Name != "Ko-fi" || link.URL != koFiSupportURL {
		t.Fatalf("unexpected Ko-fi metadata: %+v", link)
	}
	if koFiSupportURL != "https://ko-fi.com/E1E31QQRI9" {
		t.Fatalf("Ko-fi URL = %q", koFiSupportURL)
	}
	for _, link := range supportLinks {
		if strings.Contains(strings.ToLower(link.Name), "github") {
			t.Fatalf("unconfirmed GitHub Sponsors provider should not be registered: %+v", link)
		}
	}
}

func TestSupportSettingsSectionRegistrationAndRendering(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	if got := settingsSectionLabels[ssSupport]; got != "SUPPORT" {
		t.Fatalf("Support section label = %q", got)
	}
	wantFields := []settingsField{supportOpenField(0), supportCopyField(0)}
	gotFields := s.sectionFields(ssSupport)
	if len(gotFields) != len(wantFields) {
		t.Fatalf("Support fields = %#v; want %#v", gotFields, wantFields)
	}
	for i := range wantFields {
		if gotFields[i] != wantFields[i] {
			t.Fatalf("Support fields = %#v; want %#v", gotFields, wantFields)
		}
	}

	s.setActiveSection(ssSupport)
	view := ansi.Strip(s.View(120, 24, newManagerChrome(120, CatppuccinMocha, false)))
	for _, want := range []string{"free and open source", "Support on Ko-fi", "Copy link", koFiSupportURL, "Thank you"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Support view missing %q: %q", want, view)
		}
	}
	if strings.Count(view, "Support Tidemail") != 1 || strings.Contains(view, "Back to sections") {
		t.Fatalf("Support page has a duplicate header or back control: %q", view)
	}
}

func TestSupportKeyboardNavigationAndActions(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssSupport)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(supportOpenField(0))

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, DefaultKeys)
	if next.focusedField != supportCopyField(0) {
		t.Fatalf("j focused %v; want copy action", next.focusedField)
	}
	next, _, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, DefaultKeys)
	if next.focusedField != supportOpenField(0) {
		t.Fatalf("k focused %v; want open action", next.focusedField)
	}
	next.setFocusedField(supportOpenField(0))
	next, _, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if got := next.takeAction(); got != settingsActionOpenSupport {
		t.Fatalf("Enter action = %v; want open support", got)
	}
	next.setFocusedField(supportCopyField(0))
	next, _, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if got := next.takeAction(); got != settingsActionCopySupport {
		t.Fatalf("copy Enter action = %v; want copy support", got)
	}

	next, _, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlC}, DefaultKeys)
	if got := next.takeAction(); got != settingsActionCopySupport {
		t.Fatalf("Ctrl+C action = %v; want copy support", got)
	}
	next, _, _ = next.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if next.focusedPane != settingsPaneSidebar {
		t.Fatalf("Esc left focus in pane %v; want Settings sections", next.focusedPane)
	}
}

func TestAboutSupportEntryNavigatesToUnifiedPage(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfAboutSupport)

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if next.activeSection != ssSupport || next.focusedField != supportOpenField(0) {
		t.Fatalf("About Support navigated to section=%v field=%v", next.activeSection, next.focusedField)
	}
}

func TestSupportCommandRegistrationAndNavigation(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	found := false
	for _, item := range m.mainCommandItems() {
		if item.id == "support" && item.enabled {
			found = true
		}
	}
	if !found {
		t.Fatal("support command is not registered")
	}

	nextModel, cmd := m.executeCommand("support")
	next := nextModel.(Model)
	if next.overlay != overlaySettings || next.settings.activeSection != ssSupport || next.settings.focusedField != supportOpenField(0) {
		t.Fatalf("support command did not open Support page: overlay=%v section=%v field=%v", next.overlay, next.settings.activeSection, next.settings.focusedField)
	}
	if cmd == nil {
		t.Fatal("support command did not start the heart animation")
	}
}

func TestSupportHeartbeatOnlyTicksWhileVisible(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssSupport)
	chrome := newManagerChrome(80, CatppuccinMocha, false)
	before := s.renderSupportHeart(40, chrome)

	next, cmd, _ := s.Update(settingsSupportPulseMsg{}, DefaultKeys)
	if next.supportPulseFrame != 1 || cmd == nil {
		t.Fatalf("heartbeat did not advance while Support was visible: frame=%d cmd=%v", next.supportPulseFrame, cmd)
	}
	if after := next.renderSupportHeart(40, chrome); after == before {
		t.Fatal("heartbeat frame did not change the rendered heart")
	}

	next.setActiveSection(ssDisplay)
	if next.supportPulseFrame != 0 {
		t.Fatalf("heartbeat frame was not reset after leaving Support: %d", next.supportPulseFrame)
	}
	after, cmd, _ := next.Update(settingsSupportPulseMsg{}, DefaultKeys)
	if after.supportPulseFrame != 0 || cmd != nil {
		t.Fatal("heartbeat continued after leaving Support")
	}
}

func TestSupportFrameUsesWhiteDotsAndKeepsFocusVisible(t *testing.T) {
	trueColor(t)
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssSupport)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(supportOpenField(0))
	chrome := newManagerChrome(80, CatppuccinMocha, false)
	view := s.View(80, 24, chrome)
	if !strings.Contains(view, foreground(t, chrome.errorFg)) {
		t.Fatal("animated heart is not using the theme's red error color")
	}
	if !strings.Contains(view, foreground(t, readableText(lipgloss.Color("#ffffff"), chrome.baseBg, 3))) {
		t.Fatal("dotted frame is not white")
	}
	if !strings.Contains(view, foreground(t, accentReadableOn(supportPacmanYellow, chrome.baseBg, 3))) {
		t.Fatal("Pac-Man is not yellow")
	}
	if !strings.Contains(ansi.Strip(view), "Support on Ko-fi") {
		t.Fatal("Support action lost its focused, filled button")
	}
	focused := renderSettingsLinkButton("Support on Ko-fi", 18, true, chrome.baseBg, chrome)
	unfocused := renderSettingsLinkButton("Support on Ko-fi", 18, false, chrome.baseBg, chrome)
	if focused == unfocused {
		t.Fatal("Support button does not use About's filled focus treatment")
	}
	lines := strings.Split(ansi.Strip(view), "\n")
	firstDot, lastDot := -1, -1
	for i, line := range lines {
		if strings.Contains(line, strings.Repeat("·", 12)) {
			if firstDot < 0 {
				firstDot = i
			}
			lastDot = i
		}
	}
	if firstDot < 0 || lastDot-firstDot < 8 || len(lines)-lastDot > 4 {
		t.Fatalf("dotted frame leaves excessive bottom space: first=%d last=%d total=%d", firstDot, lastDot, len(lines))
	}
	body := s.viewSectionBody(61, chrome)
	heartRow := len(body.lines) - 3
	if !strings.Contains(ansi.Strip(body.lines[heartRow]), "♥") {
		t.Fatal("heart is not at the bottom of the content")
	}
	thanksFound := false
	for _, line := range body.lines[:heartRow] {
		thanksFound = thanksFound || strings.Contains(ansi.Strip(line), "-allie")
	}
	if !thanksFound {
		t.Fatal("thank-you signoff is missing above the heart")
	}
	if strings.Contains(ansi.Strip(body.lines[len(body.lines)-2]), "♥") || !strings.Contains(ansi.Strip(body.lines[len(body.lines)-2]), "·") {
		t.Fatal("missing blank row between heart and bottom border")
	}
}

func TestSupportChaseCirclesAllSidesWithoutCatchingCoffee(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	chrome := newManagerChrome(40, CatppuccinMocha, false)
	const width, height, sideW = 25, 18, 2
	period := 2 * (width - sideW + 1 + height - 2)
	seen := make(map[int]bool)
	for frame := 0; frame <= period; frame++ {
		s.supportPulseFrame = frame
		pac := supportBorderPointAt(frame%period, width, height, sideW)
		cup := supportBorderPointAt((frame+8)%period, width, height, sideW)
		if pac == cup {
			t.Fatalf("Pac-Man caught the coffee at frame %d", frame)
		}
		seen[pac.direction] = true
		var frameText strings.Builder
		for row := range height {
			line := ansi.Strip(s.renderSupportFrameRow(width, height, sideW, row, "", "·", false, chrome))
			if got := ansi.StringWidth(line); got != width {
				t.Fatalf("frame %d row %d has width %d: %q", frame, row, got, line)
			}
			frameText.WriteString(line)
		}
		if strings.Count(frameText.String(), "☕") != 1 {
			t.Fatalf("frame %d does not show one coffee cup", frame)
		}
		pacCount := 0
		for _, glyph := range []string{"𜱭", "𜱮", "𜱫", "𜱬"} {
			pacCount += strings.Count(frameText.String(), glyph)
		}
		if pacCount != 1 {
			t.Fatalf("frame %d does not show one filled Pac-Man", frame)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("chase visited %d of four sides", len(seen))
	}
}

func TestSupportContentFillsAvailableHeight(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssSupport)
	chrome := newManagerChrome(80, CatppuccinMocha, false)
	_ = s.View(80, 30, chrome)
	body := s.viewSectionBody(61, chrome)
	if len(body.lines) != s.detailHeight {
		t.Fatalf("Support body uses %d of %d available rows", len(body.lines), s.detailHeight)
	}
	for _, field := range []settingsField{supportOpenField(0), supportCopyField(0)} {
		if body.anchors[field] <= 0 || body.anchors[field] >= len(body.lines)-1 {
			t.Fatalf("support action %d anchor is outside its full-height frame: %d", field, body.anchors[field])
		}
	}
}

func TestSupportContentFollowsCenteredSpacingMockup(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssSupport)
	chrome := newManagerChrome(100, CatppuccinMocha, false)
	_ = s.View(100, 34, chrome)
	body := s.viewSectionBody(70, chrome)
	labels := []string{
		"Support Tidemail",
		"Tidemail is free and open source.",
		"If it makes email a little better for you,",
		"you can support continued development.",
		"☕ Ko-fi",
		"Support on Ko-fi",
		koFiSupportURL,
		"Thank you for supporting Tidemail -allie",
		"♥",
	}
	last := -1
	for _, label := range labels {
		found := -1
		for row, styled := range body.lines {
			if strings.Contains(ansi.Strip(styled), label) {
				found = row
				break
			}
		}
		if found <= last {
			t.Fatalf("support content %q is out of order or missing (row %d after %d)", label, found, last)
		}
		line := ansi.Strip(body.lines[found])
		start := strings.Index(line, label)
		delta := start - (ansi.StringWidth(line)-lipgloss.Width(label))/2
		if label != "Support on Ko-fi" && (start < 0 || delta < -4 || delta > 4) {
			t.Fatalf("support content %q is not centered in %q", label, line)
		}
		last = found
	}
	if !strings.Contains(ansi.Strip(strings.Join(body.lines, "\n")), "Copy link") {
		t.Fatal("copy button is not visible")
	}
	for _, label := range []string{"One-time or recurring support", "Browser / ctrl-c"} {
		if strings.Contains(ansi.Strip(strings.Join(body.lines, "\n")), label) {
			t.Fatalf("old support row %q is still visible", label)
		}
	}
}

func TestSupportButtonsMatchAboutAndStackWhenNarrow(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssSupport)
	s.setFocusedField(supportOpenField(0))
	chrome := newManagerChrome(80, CatppuccinMocha, false)
	wide, wideCopyRow := s.renderSupportButtons(supportLinks[0], 0, 60, chrome)
	if wideCopyRow != 0 || lipgloss.Height(wide) != 3 {
		t.Fatalf("wide support buttons should share one About-style row: offset=%d height=%d", wideCopyRow, lipgloss.Height(wide))
	}
	if !strings.Contains(ansi.Strip(wide), "╭") || !strings.Contains(ansi.Strip(wide), "Support on Ko-fi") || !strings.Contains(ansi.Strip(wide), "Copy link") {
		t.Fatalf("wide support buttons lost About-style rounded controls: %q", ansi.Strip(wide))
	}
	narrow, narrowCopyRow := s.renderSupportButtons(supportLinks[0], 0, 24, chrome)
	if narrowCopyRow <= 0 || lipgloss.Height(narrow) <= lipgloss.Height(wide) {
		t.Fatalf("narrow support buttons did not stack: offset=%d height=%d", narrowCopyRow, lipgloss.Height(narrow))
	}
	for i, line := range strings.Split(narrow, "\n") {
		if got := ansi.StringWidth(line); got > 24 {
			t.Fatalf("narrow support button row %d is %d columns wide", i, got)
		}
	}
}

func TestSupportHeartUsesPlainThemeFallback(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssSupport)
	s.themeName = ThemeNameVT52
	view := ansi.Strip(s.renderSupportHeart(30, newManagerChrome(30, CatppuccinMocha, true)))
	if !strings.Contains(view, "<3") || strings.Contains(view, "♥") {
		t.Fatalf("plain theme heart = %q", view)
	}
	if cmd := s.supportPulseCmd(); cmd == nil {
		t.Fatal("plain theme should animate the ASCII chase")
	}
	chase := ansi.Strip(s.renderSupportFrameRow(25, 18, 3, 0, "", ".", true, newManagerChrome(25, CatppuccinMocha, true)))
	if !strings.Contains(chase, "C") || !strings.Contains(chase, "[_]") || strings.Contains(chase, "☕") {
		t.Fatalf("plain theme chase = %q", chase)
	}
}

func TestSupportBrowserFailureShowsURL(t *testing.T) {
	original := browserOpen
	t.Cleanup(func() { browserOpen = original })
	browserOpen = func(configured, target string) error {
		if target != koFiSupportURL {
			t.Fatalf("browser target = %q; want %q", target, koFiSupportURL)
		}
		return errors.New("no launcher")
	}

	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.overlay = overlaySettings
	m.settings.setActiveSection(ssSupport)
	m.settings.setFocusedPane(settingsPaneDetail)
	m.settings.setFocusedField(supportOpenField(0))
	nextModel, cmd := m.handleSettings(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected browser command")
	}
	msg := cmd()
	nextModel, _ = nextModel.(Model).Update(msg)
	next := nextModel.(Model)
	if !next.statusErr || !strings.Contains(next.statusMsg, "Could not open your browser") || !strings.Contains(next.statusMsg, koFiSupportURL) {
		t.Fatalf("browser failure status = %q, error=%t", next.statusMsg, next.statusErr)
	}
}

func TestSupportPageFitsNarrowTerminal(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssSupport)
	view := ansi.Strip(s.View(40, 18, newManagerChrome(40, CatppuccinMocha, false)))
	for i, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width > 40 {
			t.Fatalf("narrow Support line %d is %d columns: %q", i, width, line)
		}
	}
}
