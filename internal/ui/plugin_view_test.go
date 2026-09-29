package ui

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

const viewFixture = `{"title":"Mail Analytics","subtitle":"last 30 days","blocks":[
 {"type":"stats","items":[{"label":"Received","value":"1,234"},{"label":"Sent","value":"56"},{"label":"Needs you","value":"12","tone":"attention"},{"label":"Urgent","value":"2","tone":"critical"},{"label":"Replied","value":"41","tone":"positive","note":"this month"}]},
 {"type":"sparkline","title":"Volume per day","note":"peak 30","series":[{"label":"in","values":[1,5,30,0,7,2,9,12,4,4,8,1,0,3]},{"label":"out","values":[0,1,2,0,3,0,1,1,0,0,2,0,0,1]}],"start":"Sep 14","end":"Sep 27"},
 {"type":"bars","title":"Categories","kind":"category","items":[{"label":"newsletter","count":471,"note":"47%"},{"label":"github","count":298,"note":"30%"},{"label":"none","count":87,"note":"9%"}]},
 {"type":"bars","title":"Top correspondents","items":[{"label":"Ann Lee","count":62},{"label":"very-long-correspondent@example.com","count":3}]},
 {"type":"heatmap","title":"Weekday rhythm","columns":["Mon","Tue","Wed","Thu","Fri","Sat","Sun"],"rows":[{"label":"week 1","values":[3,0,5,9,2,0,1]},{"label":"week 2","values":[1,4,0,2,8,3,0]}]},
 {"type":"table","title":"Busiest conversations","columns":["Msgs","Subject"],"cells":[["9","Quarterly plan"],["4","A very long subject line that will certainly need to be truncated to fit"]]},
 {"type":"text","title":"Notes","text":"Counts come from TideMail's cache.\nRead-only."}
]}`

var bgSGR = regexp.MustCompile(`(^|;)48;`)

// unpaintedCell returns the byte offset of the first visible character
// drawn without a background, or -1.
func unpaintedCell(line string) int {
	bg := false
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			end := strings.IndexByte(line[i:], 'm')
			if end < 0 {
				return -1
			}
			params := strings.TrimPrefix(line[i:i+end], "\x1b[")
			switch {
			case params == "0" || params == "":
				bg = false
			case bgSGR.MatchString(params):
				bg = true
			}
			i += end + 1
			continue
		}
		if !bg {
			return i
		}
		i++
	}
	return -1
}

func TestPluginViewRendersEveryBlockPainted(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	v, err := plugin.ParseView(json.RawMessage(viewFixture))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := newMailboxListModel(1, 1)
	for _, width := range []int{40, 72, 106} {
		chrome := newManagerChrome(width+4, m.styles.Theme, false)
		lines := m.pluginViewLines(&v, width, chrome)
		text := ansi.Strip(strings.Join(lines, "\n"))
		for _, want := range []string{"Mail Analytics", "RECEIVED", "1,234", "NEEDS YOU", "newsletter", "471", "Weekday rhythm", "Quarterly plan", "Read-only.", "Sep 14"} {
			if !strings.Contains(text, want) {
				t.Fatalf("width %d: missing %q in\n%s", width, want, text)
			}
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > width {
				t.Fatalf("width %d: line is %d wide: %q", width, w, ansi.Strip(l))
			}
			if i := unpaintedCell(l); i >= 0 && strings.TrimSpace(ansi.Strip(l)) != "" {
				t.Fatalf("width %d: unpainted cell at byte %d in %q", width, i, l)
			}
		}
	}
}

func TestPluginViewPlainUIHasNoColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	v, _ := plugin.ParseView(json.RawMessage(viewFixture))
	m, _ := newMailboxListModel(1, 1)
	chrome := newManagerChrome(80, m.styles.Theme, true)
	for _, l := range m.pluginViewLines(&v, 76, chrome) {
		if strings.Contains(l, "\x1b[") && strings.Contains(l, "38;2") {
			t.Fatalf("plain UI line has color: %q", l)
		}
	}
}
