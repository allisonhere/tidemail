package ui

import (
	"html"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestReaderEscapedHrefSpellings(t *testing.T) {
	const decoded = "https://track.example/click?id=7&action=approve"
	const escaped = "https://track.example/click?id=7&amp;action=approve"
	const literalEntity = "https://track.example/click?id=7&amp;amp;action=approve"
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"named entity", `<a href="` + escaped + `">Approve</a>`, []string{decoded}},
		{"decimal entity", `<a href="https://track.example/click?id=7&#38;action=approve">Approve</a>`, []string{decoded}},
		{"hex entity", `<a href="https://track.example/click?id=7&#x26;action=approve">Approve</a>`, []string{decoded}},
		{"unquoted href", `<a href=` + escaped + `>Approve</a>`, []string{decoded}},
		{"uppercase tag", `<A HREF='` + escaped + `'>Approve</A>`, []string{decoded}},
		{"repeated href", `<a href="` + escaped + `">Approve</a><a href="` + decoded + `">Again</a>`, []string{decoded}},
		{"genuinely different literal entity", `<a href="` + escaped + `">First</a><a href="` + literalEntity + `">Second</a>`, []string{decoded, escaped}},
		{"different tracking tokens", `<a href="` + escaped + `">First</a><a href="https://track.example/click?id=8&amp;action=approve">Second</a>`, []string{decoded, "https://track.example/click?id=8&action=approve"}},
		{"query order is preserved", `<a href="` + escaped + `">First</a><a href="https://track.example/click?action=approve&amp;id=7">Second</a>`, []string{decoded, "https://track.example/click?action=approve&id=7"}},
		{"Outlook hidden href", `<a href="` + escaped + `" style="mso-hide:all">Approve</a>`, []string{decoded}},
		{"visible text is decoded without script URLs", `<a href="` + escaped + `">Approve</a><script>const u="` + escaped + `";</script><p>` + escaped + `</p>`, []string{decoded}},
		{"comments contribute no URLs", `<!-- <a href="` + escaped + `">Approve</a> -->`, nil},
		{"standalone images contribute no URLs", `<img src="` + escaped + `" alt="Banner">`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractActionableLinksFromHTML(tc.source, ""); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("targets = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestReaderButtonLabelFallback(t *testing.T) {
	const target = "https://example.com/action"
	for _, tc := range []struct{ name, markup, want string }{
		{"ordinary styled anchor", `<a href="` + target + `" style="display:inline-block;background:#080"><span>Approve</span></a>`, "Approve"},
		{"ordinary nested button", `<a href="` + target + `"><button>Deny</button></a>`, "Deny"},
		{"Outlook fallback anchor", `<a href="` + target + `" style="mso-hide:all">Approve</a>`, "Approve"},
		{"Outlook hidden parent", `<table><tr><td style="mso-hide:all"><a href="` + target + `">Deny</a></td></tr></table>`, "Deny"},
		{"button input", `<a href="` + target + `"><input type="button" value="Approve"></a>`, "Approve"},
		{"submit input", `<a href="` + target + `"><input type="SUBMIT" value="Deny"></a>`, "Deny"},
		{"Outlook button input", `<a href="` + target + `" style="mso-hide:all"><input type="button" value="Approve &amp; review"></a>`, "Approve & review"},
		{"visible label wins over earlier fallback", `<a href="` + target + `" style="mso-hide:all">Fallback</a><a href="` + target + `">Visible action</a>`, "Visible action"},
		{"visible label wins over later fallback", `<a href="` + target + `">Visible action</a><a href="` + target + `"><input type="button" value="Fallback"></a>`, "Visible action"},
		{"display none is not Outlook fallback", `<a href="` + target + `" style="display:none">Hidden</a>`, ""},
		{"Outlook plus display none", `<a href="` + target + `" style="mso-hide:all;display:none">Hidden</a>`, ""},
		{"aria hidden parent", `<div aria-hidden="true"><a href="` + target + `"><input type="button" value="Hidden"></a></div>`, ""},
		{"hidden input is not a caption", `<a href="` + target + `"><input type="hidden" value="Hidden"></a>`, ""},
		{"text input is not a button caption", `<a href="` + target + `"><input type="text" value="User data"></a>`, ""},
		{"VML only is unsupported", `<v:roundrect href="` + target + `"><center>Approve</center></v:roundrect>`, ""},
		{"unlabelled anchor", `<a href="` + target + `"></a>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targets := extractActionableLinksFromHTML(tc.markup, "")
			wantTargets := []string{target}
			switch tc.name {
			case "display none is not Outlook fallback", "Outlook plus display none", "aria hidden parent", "VML only is unsupported":
				wantTargets = nil
			}
			if !reflect.DeepEqual(targets, wantTargets) {
				t.Fatalf("fixture targets = %#v, want %#v", targets, wantTargets)
			}
			got := extractLinkLabels(tc.markup, targets)
			if got[target] != tc.want {
				t.Fatalf("label = %q, want %q", got[target], tc.want)
			}
			// Label discovery must not invent a destination that was not extracted.
			if labels := extractLinkLabels(tc.markup, []string{"https://other.example/"}); len(labels) != 0 {
				t.Fatalf("unexpected labels: %#v", labels)
			}
		})
	}
}

func TestReaderRecoveredButtonsPreserveNavigationAndTargets(t *testing.T) {
	approve := "https://track.example/click?id=" + strings.Repeat("abcdef", 500) + "&action=approve"
	deny := "https://track.example/click?id=7&action=deny"
	source := `<p>Workflow request.</p><a href="` + html.EscapeString(approve) + `" style="mso-hide:all">Approve</a><a href="` + html.EscapeString(deny) + `"><input type="button" value="Deny"></a>`
	// The plain-text alternative must not reorder HTML targets. The existing
	// HTML extractor finds the visible anchor before its hidden-source fallback.
	m := readerLinksModel(t, source, approve+"\n"+deny)
	if !reflect.DeepEqual(m.contentLinks, []string{deny, approve}) {
		t.Fatalf("targets = %#v", m.contentLinks)
	}
	footer := ansi.Strip(m.renderContentLinks(m.contentBodyWidth()))
	if !strings.Contains(footer, "Approve") || !strings.Contains(footer, "Deny") || strings.Contains(footer, "https://") || strings.Contains(footer, "action=") {
		t.Fatalf("footer = %q", footer)
	}
	// Caption recovery is metadata-only: the existing body renderer continues
	// to omit the Outlook-hidden anchor and the input control.
	body := ansi.Strip(m.renderMessageForDisplay(m.messages[0], m.contentBodyWidth()).body)
	if !strings.Contains(body, "Workflow request.") || strings.Contains(body, "Approve") || strings.Contains(body, "Deny") {
		t.Fatalf("button recovery changed body rendering: %q", body)
	}
	originalOpen, originalCopy := browserOpen, clipboardWriteCmd
	t.Cleanup(func() { browserOpen, clipboardWriteCmd = originalOpen, originalCopy })
	var opened, copied string
	browserOpen = func(_, target string) error { opened = target; return nil }
	clipboardWriteCmd = func(text string) tea.Cmd { copied = text; return nil }
	for _, step := range []struct {
		key    tea.KeyMsg
		index  int
		target string
	}{
		{tea.KeyMsg{Type: tea.KeyCtrlN}, 0, deny},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true}, 1, approve},
	} {
		next, _ := m.Update(step.key)
		m = next.(Model)
		assertReaderLinkSelection(t, m, step.index)
		for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'o'}}} {
			_, cmd := m.Update(key)
			if cmd == nil {
				t.Fatal("activation produced no command")
			}
			cmd()
			if opened != step.target {
				t.Fatalf("opened %q, want %q", opened, step.target)
			}
		}
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if copied != step.target {
			t.Fatalf("copied %q, want %q", copied, step.target)
		}
	}
}

func TestReaderOmitsOpenTrackingAnchors(t *testing.T) {
	const fedex = "https://www.fedex.com/track?n=123"
	const pixel = "https://click.example/o?id=1"
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{
			"wrapped 1x1 pixel before the real link",
			`<a href="` + pixel + `"><img src="https://click.example/p.gif" width="1" height="1"></a><a href="` + fedex + `">Track your package</a>`,
			[]string{fedex},
		},
		{"hidden image", `<a href="` + pixel + `"><img src="https://click.example/p.gif" style="display:none"></a><a href="` + fedex + `">Track</a>`, []string{fedex}},
		{"styled 1px image", `<a href="` + pixel + `"><img src="https://click.example/p.gif" style="width:1px;height:1px"></a><a href="` + fedex + `">Track</a>`, []string{fedex}},
		{
			"logo link with alt is kept",
			`<a href="https://example.com/home"><img src="https://example.com/logo.png" width="120" height="40" alt="Example"></a>`,
			[]string{"https://example.com/home"},
		},
		{
			"text link that also holds a pixel is kept",
			`<a href="` + fedex + `">Track<img src="https://click.example/p.gif" width="1" height="1"></a>`,
			[]string{fedex},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := readerLinksModel(t, tc.source, "")
			if !reflect.DeepEqual(m.contentLinks, tc.want) {
				t.Fatalf("links = %#v, want %#v", m.contentLinks, tc.want)
			}
		})
	}
}
