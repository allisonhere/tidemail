package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/charmbracelet/x/ansi"
)

func TestMessageHTMLPreservesLongContent(t *testing.T) {
	var raw strings.Builder
	raw.WriteString("<div>Opening details</div>")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&raw, "<p>Section%02d %s</p><p>Repeated notice.</p>", i, strings.Repeat("useful information ", 10))
	}
	raw.WriteString("<div>Final details</div><p>You are receiving this email because you subscribed.</p>")
	for _, width := range []int{24, 80} {
		m := NewModel(nil, config.DefaultConfig(), "dev", false)
		got := ansi.Strip(m.renderMessageForDisplay(db.Message{BodyHTML: raw.String()}, width).body)
		last := -1
		for i := 0; i < 40; i++ {
			marker := fmt.Sprintf("Section%02d", i)
			pos := strings.Index(got, marker)
			if pos <= last || strings.Count(got, marker) != 1 {
				t.Fatalf("lost or reordered %s at width %d", marker, width)
			}
			last = pos
		}
		if strings.Count(got, "Repeated notice.") != 40 || !strings.Contains(got, "Final details") {
			t.Fatalf("content lost: %q", got)
		}
		assertHTMLWidth(t, got, width)
	}
}

func TestHTMLNormalizationConservativeVisibility(t *testing.T) {
	raw := `<div class="preheader">Visible class content</div><div style="font-size:0">hidden text<span>hidden inherited</span><span style="font-size:16px">Restored text</span></div><div style="display:none"><span style="font-size:16px">Hidden subtree</span></div>`
	got := normalizeHTMLForRendering(raw)
	for _, want := range []string{"Visible class content", "Restored text"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lost %q: %s", want, got)
		}
	}
	for _, unwanted := range []string{"hidden text", "hidden inherited", "Hidden subtree"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("kept %q: %s", unwanted, got)
		}
	}
}

func TestMessageHTMLPreservesStructuredContent(t *testing.T) {
	fixtures := []struct {
		name, html string
		ordered    []string
	}{
		{"receipt", `<table><tr><td>Apples</td><td>$4</td></tr><tr><td>Total</td><td>$8</td></tr></table>`, []string{"Apples", "$4", "Total", "$8"}},
		{"merged", `<table><tr><th colspan="2">Invoice</th></tr><tr><td rowspan="2">Service</td><td>January</td></tr><tr><td>February</td></tr></table>`, []string{"Invoice", "Service", "January", "February"}},
		{"linked layout", `<a href="https://example.com/report"><table><tr><td><h2>Report</h2><p>Introduction</p><ul><li>Alpha</li><li>Beta</li></ul><img alt="Quarterly chart" src="https://example.com/chart.png"></td></tr></table></a>`, []string{"Report", "Introduction", "Alpha", "Beta", "Quarterly chart"}},
		{"cell blocks", `<table><tr><th>Details</th><th>Status</th></tr><tr><td><p>First</p><p>Second<br>Third</p></td><td>Done</td></tr></table>`, []string{"First", "Second", "Third"}},
		{"anchor without href", `<a name="section"><p>Visible anchor</p></a>`, []string{"Visible anchor"}},
	}
	for _, fixture := range fixtures {
		for _, width := range []int{24, 80} {
			t.Run(fmt.Sprintf("%s/%d", fixture.name, width), func(t *testing.T) {
				m := NewModel(nil, config.DefaultConfig(), "dev", false)
				got := ansi.Strip(m.renderMessageForDisplay(db.Message{BodyHTML: fixture.html}, width).body)
				previous := -1
				for _, text := range fixture.ordered {
					pos := strings.Index(got, text)
					if pos <= previous || strings.Count(got, text) != 1 {
						t.Fatalf("lost, duplicated or reordered %q: %q", text, got)
					}
					previous = pos
				}
				if fixture.name == "cell blocks" {
					for _, line := range strings.Split(got, "\n") {
						if strings.Contains(line, "First") && strings.Contains(line, "Second") {
							t.Fatalf("merged paragraphs: %q", got)
						}
					}
				}
				assertHTMLWidth(t, got, width)
			})
		}
	}
}

func assertHTMLWidth(t *testing.T, body string, width int) {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if ansi.StringWidth(line) > width {
			t.Fatalf("line exceeds %d columns: %q", width, line)
		}
	}
}
