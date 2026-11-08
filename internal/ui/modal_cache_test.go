package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestRenderCacheOwnership(t *testing.T) {
	stats := &renderCacheStats{}
	c := renderCache[int, string]{stats: stats}
	builds := 0
	build := func() string { builds++; return "value" }
	c.Get(1, build)
	c.Get(1, build)
	c.Get(2, build)
	c.Invalidate()
	c.Get(2, build)
	if builds != 3 || stats.Hits != 1 || stats.Misses != 3 {
		t.Fatalf("builds=%d stats=%+v", builds, stats)
	}
}

func TestModalCompositeMatchesReference(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	for _, profile := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.Ascii} {
		lipgloss.SetColorProfile(profile)
		for _, bg := range []lipgloss.Color{"#192a37", ""} {
			for _, shadow := range []bool{false, true} {
				base := strings.Repeat("\x1b[31m背景 wide 🌊 text\x1b[0m\n", 18)
				var c modalComposite
				var finish modalFinish
				for _, box := range []string{"modal\nsecond row", "changed\nselected\nrow", strings.Repeat("超", 23) + "\nbox", "x"} {
					got := c.View(base, box, 42, 16, bg, shadow)
					want := overlayOnBase(base, box, 42, 16, bg, shadow)
					if got != want {
						t.Fatalf("composite differs: profile=%v bg=%q shadow=%v box=%q", profile, bg, shadow, box)
					}
					got = finish.View(got, 42, 16, bg)
					want = lipgloss.NewStyle().Width(42).Height(16).Background(bg).Render(clampView(want, 42, 16, bg))
					if got != want {
						t.Fatalf("finish differs: profile=%v bg=%q shadow=%v", profile, bg, shadow)
					}
				}
			}
		}
	}
}

func TestComposeCacheDisplayDependencies(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(120, 50, false)
	m.sizeCompose()
	stats := &renderCacheStats{}
	m.composeBackdrop.paneStage.stats = stats
	_ = m.View()
	next, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 60, Y: 20})
	m = next.(Model)
	assertComposeBackdropMatchesFresh(t, m)
	next, _ = m.Update(struct{}{})
	m = next.(Model)
	assertComposeBackdropMatchesFresh(t, m)
	if stats.Misses != 1 {
		t.Fatal("unchanged mouse motion rebuilt panes")
	}
	for _, mutate := range []func(){
		func() { m.messages[0].Subject = "in-place new subject"; m.applyFilter() },
		func() { m.adjustMailboxUnreadCount(m.mailboxes[0].ID, 1) },
		func() { m.setStatus("new status", false) },
		func() { m.syncVisible = map[int64]bool{7: true} },
		func() { m.cfg.Display.Shadow = !m.cfg.Display.Shadow },
		func() { m.setStyles(BuildStyles(BuiltinThemes[len(BuiltinThemes)-1], "compact", "square")) },
		func() { m.styles.Theme.Bg = "" },
		func() { m.contentSearchMatches = []int{0}; m.contentSearchIdx = 0; m.invalidateComposePanes() },
		func() { m.width = 98; m.height = 38; m.sizeCompose() },
		func() { m.compose.maximized = true; m.sizeCompose() },
		func() { m.compose.maximized = false; m.sizeCompose() },
	} {
		mutate()
		assertComposeBackdropMatchesFresh(t, m)
	}
}

func TestComposeCacheColorProfileChanges(t *testing.T) {
	preserveComposeEditorState(t)
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	m := composeEventModel(120, 50, false)
	m.sizeCompose()
	for _, profile := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(profile)
		assertComposeBackdropMatchesFresh(t, m)
	}
}

func TestComposeCacheStatusStageIndependent(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(120, 50, false)
	m.sizeCompose()
	panes, status, composition := &renderCacheStats{}, &renderCacheStats{}, &renderCacheStats{}
	c := m.composeBackdrop
	c.paneStage.stats = panes
	c.statusStage.stats = status
	c.composite.prepared.stats = composition
	_ = m.View()
	_ = m.View()
	if panes.Misses != 1 || status.Misses != 1 || composition.Misses != 1 {
		t.Fatal("unchanged screen missed")
	}
	m.setStatus("background notification", false)
	assertComposeBackdropMatchesFresh(t, m)
	if panes.Misses != 1 || status.Misses != 2 || composition.Misses != 2 {
		t.Fatalf("status invalidation: panes=%+v status=%+v composition=%+v", *panes, *status, *composition)
	}
	c.Invalidate()
	if c.valid || c.panes != "" || c.paneStage.valid || c.composite.prepared.valid || c.finish.rows != nil {
		t.Fatal("modal lifetime retained artifacts")
	}
}

// Upstream's transparency setting is a final terminal transformation, independent
// of theme background color. Cached compose must keep that newer behavior.
func TestComposeCacheTransparencySetting(t *testing.T) {
	preserveComposeEditorState(t)
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	m := composeEventModel(120, 50, false)
	m.sizeCompose()
	for _, transparent := range []bool{false, true, false} {
		m.cfg.Display.Transparent = transparent
		assertComposeBackdropMatchesFresh(t, m)
		if transparent && strings.Contains(m.View(), themeBgParams(m.styles.Theme.Bg)) {
			t.Fatal("cached compose retained opaque theme background")
		}
	}
}
