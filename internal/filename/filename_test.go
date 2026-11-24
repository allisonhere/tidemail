package filename

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSafeKeepsUnicode(t *testing.T) {
	for in, want := range map[string]string{
		"报告 最终版.pdf":       "报告-最终版.pdf",
		"Élodie CV.docx":   "élodie-cv.docx",
		"Report 2026.PDF":  "report-2026.pdf",
		"../../etc/passwd": "....etcpasswd",
		"..":               "attachment",
		"?":                "attachment",
	} {
		if got := Safe(in); got != want {
			t.Errorf("Safe(%q) = %q, want %q", in, got, want)
		}
	}
	got := Safe(strings.Repeat("张", 80) + ".pdf")
	if !utf8.ValidString(got) || !strings.HasSuffix(got, ".pdf") || len(got) > 100 {
		t.Errorf("long name truncated badly: %q (%d bytes)", got, len(got))
	}
}
