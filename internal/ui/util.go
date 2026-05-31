package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/allisonhere/tide/internal/db"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func formatFileSize(size int64) string {
	switch {
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
}

func fileTypeIcon(filename, contentType string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return "▤"
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg":
		return "▣"
	case ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar", ".tgz":
		return "⊞"
	case ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".odt", ".ods", ".odp":
		return "◇"
	case ".go", ".py", ".js", ".ts", ".rs", ".c", ".cpp", ".h", ".hpp",
		".java", ".rb", ".sh", ".bash", ".zsh", ".fish", ".swift", ".kt",
		".css", ".scss", ".less", ".html", ".htm", ".xml", ".yaml", ".yml",
		".json", ".toml", ".ini", ".cfg", ".conf", ".sql", ".r", ".m",
		".ex", ".exs", ".php", ".pl", ".lua", ".vue", ".svelte", ".tsx", ".jsx":
		return "⌘"
	case ".txt", ".md", ".rst", ".adoc", ".org", ".log":
		return "≡"
	case ".mp3", ".wav", ".flac", ".ogg", ".aac", ".m4a", ".opus", ".wma":
		return "♫"
	case ".mp4", ".avi", ".mkv", ".mov", ".webm", ".m4v", ".flv", ".wmv":
		return "▶"
	case ".csv", ".tsv":
		return "≡"
	default:
		if strings.HasPrefix(contentType, "image/") {
			return "▣"
		}
		if strings.HasPrefix(contentType, "text/") {
			return "≡"
		}
		if strings.HasPrefix(contentType, "audio/") {
			return "♫"
		}
		if strings.HasPrefix(contentType, "video/") {
			return "▶"
		}
		if strings.Contains(contentType, "pdf") ||
			strings.Contains(contentType, "postscript") {
			return "▤"
		}
		if strings.Contains(contentType, "zip") ||
			strings.Contains(contentType, "compress") ||
			strings.Contains(contentType, "tar") ||
			strings.Contains(contentType, "gzip") {
			return "⊞"
		}
		return "○"
	}
}

func collectSearchMatches(content, query string) []int {
	if query == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	matches := make([]int, 0)
	for i, line := range lines {
		if strings.Contains(strings.ToLower(ansi.Strip(line)), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

func messageFocusableLines(content string) []bool {
	lines := strings.Split(ansi.Strip(content), "\n")
	focusable := make([]bool, len(lines))
	nonEmpty := 0
	pastHeader := false
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			if nonEmpty >= 2 {
				pastHeader = true
			}
			continue
		}
		if pastHeader {
			focusable[i] = true
		}
		nonEmpty++
	}
	return focusable
}

func firstFocusableLine(focusable []bool) int {
	for i, ok := range focusable {
		if ok {
			return i
		}
	}
	return 0
}

func nextContentFocusLine(current, delta int, focusable []bool, lineCount int) int {
	if delta == 0 || lineCount <= 0 {
		return clamp(current, 0, max(0, lineCount-1))
	}
	current = clamp(current, 0, max(0, lineCount-1))
	for next := current + delta; next >= 0 && next < lineCount; next += delta {
		if next < len(focusable) && focusable[next] {
			return next
		}
	}
	return current
}

func overlayOnBase(base, box string, width, height int, bg lipgloss.Color) string {
	base = clampView(base, width, height, bg)

	boxLines := strings.Split(box, "\n")
	boxH := len(boxLines)
	boxW := 0
	for _, l := range boxLines {
		if w := lipgloss.Width(l); w > boxW {
			boxW = w
		}
	}

	// Center position — matches lipgloss.Center, lipgloss.Center
	overlayX := (width - boxW) / 2
	overlayY := (height - boxH) / 2
	if overlayX < 0 {
		overlayX = 0
	}
	if overlayY < 0 {
		overlayY = 0
	}
	rightStart := overlayX + boxW

	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}

	result := make([]string, height)
	for y := 0; y < height; y++ {
		baseLine := baseLines[y]
		boxRow := y - overlayY
		if boxRow < 0 || boxRow >= boxH {
			result[y] = baseLine
			continue
		}
		left := ansi.Cut(baseLine, 0, overlayX)
		right := ansi.Cut(baseLine, rightStart, width)
		result[y] = left + boxLines[boxRow] + right
	}
	return strings.Join(result, "\n")
}

func safeFilename(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '-':
			if b.Len() > 0 {
				s := b.String()
				if s[len(s)-1] != '-' {
					b.WriteByte('-')
				}
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "attachment"
	}
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

func summaryFilename(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '-':
			if b.Len() > 0 {
				s := b.String()
				if s[len(s)-1] != '-' {
					b.WriteByte('-')
				}
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "summary"
	}
	if len(s) > 50 {
		s = s[:50]
	}
	return s + ".md"
}

func sectionKey(accountID int64, isSystem bool) string {
	if isSystem {
		return fmt.Sprintf("system:%d", accountID)
	}
	return fmt.Sprintf("personal:%d", accountID)
}

func mailboxRank(name string) int {
	lower := strings.ToLower(name)
	if i := strings.LastIndex(lower, "/"); i >= 0 {
		lower = strings.TrimSpace(lower[i+1:])
	}
	switch lower {
	case "inbox":
		return 0
	case "sent", "sent items", "sent mail":
		return 1
	case "drafts":
		return 2
	case "archive", "all mail":
		return 3
	case "trash", "deleted items", "deleted messages":
		return 4
	case "junk", "spam", "junk e-mail", "junk email":
		return 5
	}
	return 6
}

func cleanDisplayName(name string) string {
	// Capitalize INBOX -> Inbox (Gmail returns it uppercase)
	if strings.EqualFold(name, "INBOX") {
		return "Inbox"
	}

	// Strip Gmail's [Gmail]/ prefix (also covers [Google]/ etc.)
	cleaned := name
	if idx := strings.Index(name, "]/"); idx >= 0 {
		cleaned = strings.TrimSpace(name[idx+2:])
	}

	// Strip "INBOX." or "INBOX/" prefix for custom accounts
	// (e.g. Courier/Dovecot: INBOX.Sent -> Sent, INBOX/Drafts -> Drafts)
	upper := strings.ToUpper(cleaned)
	if strings.HasPrefix(upper, "INBOX.") {
		cleaned = strings.TrimSpace(cleaned[6:])
	} else if strings.HasPrefix(upper, "INBOX/") {
		cleaned = strings.TrimSpace(cleaned[6:])
	}

	return cleaned
}

func hasFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, flag) {
			return true
		}
	}
	return false
}

func isGmailSystemFolder(name string) bool {
	// Gmail system labels live under the [Gmail]/ namespace on English
	// accounts. The prefix varies by locale (e.g. [Google], [Gmail]),
	// so we match the segment after "]".
	idx := strings.Index(name, "]/")
	if idx < 0 {
		return false
	}
	rest := strings.TrimSpace(name[idx+2:])

	// Hide: All Mail (duplicates everything), Starred/Important
	// (no star/importance UI), Spam/Trash (handled by actions),
	// and all Categories/* (Gmail auto-categorization noise).
	// Keep: Drafts, Sent Mail — these are useful.
	lower := strings.ToLower(rest)
	switch lower {
	case "all mail", "starred", "important", "spam", "trash":
		return true
	}
	if strings.HasPrefix(lower, "categories/") {
		return true
	}
	return false
}

func isInboxMailbox(mb db.Mailbox) bool {
	if strings.EqualFold(strings.TrimSpace(mb.Name), "inbox") || strings.EqualFold(strings.TrimSpace(mb.DisplayName), "inbox") {
		return true
	}
	for _, flag := range mb.Flags {
		if strings.EqualFold(flag, `\Inbox`) {
			return true
		}
	}
	return false
}

func terminalColorAsColor(c lipgloss.TerminalColor) lipgloss.Color {
	if c == nil {
		return ""
	}
	return lipgloss.Color(fmt.Sprint(c))
}

func keyMatches(msg tea.KeyMsg, bindings ...key.Binding) bool {
	for _, b := range bindings {
		if key.Matches(msg, b) {
			return true
		}
	}
	return false
}

func truncate(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW <= 1 {
		return ansi.Truncate(s, maxW, "")
	}
	return ansi.Truncate(s, maxW, "…")
}

func (m Model) formatTime(t time.Time) string {
	switch m.cfg.Display.DateFormat {
	case "absolute":
		return t.Format("Jan 2, 2006")
	case "none":
		return ""
	default:
		return relativeTime(t)
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 4*7*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return t.Format("Jan 2")
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

func viewLineCount(parts []string) int {
	n := 0
	for _, p := range parts {
		if p == "" {
			n++
			continue
		}
		n += strings.Count(p, "\n") + 1
	}
	return n
}

func clampView(view string, width, height int, bg lipgloss.Color) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	bgStyle := lipgloss.NewStyle().Background(bg)
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		line = ansi.Truncate(line, width, "")
		if !strings.HasSuffix(line, ansi.ResetStyle) {
			line += ansi.ResetStyle
		}
		pad := width - lipgloss.Width(line)
		if pad > 0 {
			line += bgStyle.Render(strings.Repeat(" ", pad))
		}
		lines[i] = line
	}
	for len(lines) < height {
		lines = append(lines, bgStyle.Render(strings.Repeat(" ", width)))
	}
	return strings.Join(lines, "\n")
}

func fillViewWidth(view string, width int, bg lipgloss.Color) string {
	if width <= 0 || view == "" {
		return view
	}
	return clampView(view, width, strings.Count(view, "\n")+1, bg)
}

func collapseQuoteBlocks(body string, collapsed bool) string {
	if !collapsed {
		return body
	}
	// Blockquote lines are rendered with ANSI dimmed styling, so │
	// is preceded by escape codes, not at position 0. Use Contains.
	quoteRune := "│"
	if !strings.Contains(body, quoteRune) {
		return body
	}
	lines := strings.Split(body, "\n")
	var result []string
	i := 0
	for i < len(lines) {
		if strings.Contains(lines[i], quoteRune) {
			start := i
			for i < len(lines) && strings.Contains(lines[i], quoteRune) {
				i++
			}
			count := i - start
			result = append(result, fmt.Sprintf("│  [+%d quoted lines — press z to expand]", count))
		} else {
			result = append(result, lines[i])
			i++
		}
	}
	return strings.Join(result, "\n")
}

func indentBlock(view string, pad int) string {
	if view == "" || pad <= 0 {
		return view
	}
	prefix := strings.Repeat(" ", pad)
	lines := strings.Split(view, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
