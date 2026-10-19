// Package mailtemplate holds TideMail's canned replies, shared by the compose
// screen and the command line. Built-ins are embedded; any .md or .txt file
// in the templates folder next to config.toml adds one, and a file named like
// a built-in replaces it. The filename (minus extension) is the template's
// name. A file may start with a "---" block holding "subject: ..."; the rest is
// the body. Templates are plain text with a few {placeholders}.
package mailtemplate

import (
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxBytes = 64 << 10

type Template struct {
	Name    string
	Subject string
	Body    string
}

var Builtins = []Template{
	{Name: "Thanks", Body: "Hi {first_name},\n\nThanks so much, I really appreciate it.\n\nBest,\n{my_name}\n"},
	{Name: "Got it", Body: "Hi {first_name},\n\nGot it, thanks. I'll get back to you properly soon.\n\nBest,\n{my_name}\n"},
	{Name: "Out of office", Body: "Hi {first_name},\n\nThanks for your message. I'm away from my inbox and will reply when I'm back.\n\nBest,\n{my_name}\n"},
	{Name: "Propose a meeting", Body: "Hi {first_name},\n\nWould you be free for a short call this week? Let me know a couple of times that suit you and I'll make one work.\n\nBest,\n{my_name}\n"},
	{Name: "Polite decline", Body: "Hi {first_name},\n\nThank you for thinking of me. Unfortunately I can't take this on right now, but I appreciate the invitation.\n\nBest,\n{my_name}\n"},
	{Name: "Follow up", Body: "Hi {first_name},\n\nJust following up on my earlier message. Do you have any update?\n\nBest,\n{my_name}\n"},
}

// Load returns the built-ins plus the template files in dir, sorted by
// name. A missing or unreadable dir yields just the built-ins; unreadable,
// oversized, hidden and symlinked-out-of-dir files are skipped.
func Load(dir string) []Template {
	byName := map[string]Template{}
	for _, t := range Builtins {
		byName[strings.ToLower(t.Name)] = t
	}
	root, err := filepath.EvalSymlinks(dir)
	if err == nil {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if strings.HasPrefix(e.Name(), ".") || (ext != ".md" && ext != ".txt") {
				continue
			}
			real, err := filepath.EvalSymlinks(filepath.Join(root, e.Name()))
			if err != nil || filepath.Dir(real) != root {
				continue
			}
			info, err := os.Stat(real)
			if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBytes {
				continue
			}
			data, err := os.ReadFile(real)
			if err != nil || !utf8.Valid(data) {
				continue
			}
			t := Parse(string(data))
			t.Name = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			byName[strings.ToLower(t.Name)] = t
		}
	}
	out := make([]Template, 0, len(byName))
	for _, t := range byName {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Parse splits an optional leading "---" block (only "subject:" is
// read) from the body.
func Parse(text string) Template {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	head, rest, ok := strings.Cut(text, "\n")
	if !ok || strings.TrimSpace(head) != "---" {
		return Template{Body: text}
	}
	block, body, found := strings.Cut(rest, "\n---\n")
	if !found {
		if b, ok := strings.CutSuffix(rest, "\n---"); ok {
			block, body, found = b, "", true
		}
	}
	if !found {
		return Template{Body: text}
	}
	t := Template{Body: body}
	for _, line := range strings.Split(block, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "subject") {
			t.Subject = strings.TrimSpace(v)
		}
	}
	return t
}

var TagRe = regexp.MustCompile(`\{[a-z_]+\}`)

// Fill replaces the known {placeholders}; unknown ones stay as written.
func Fill(s string, vars map[string]string) string {
	return TagRe.ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := vars[m[1:len(m)-1]]; ok {
			return v
		}
		return m
	})
}

// Vars builds the placeholder values from the To field's first
// recipient and the sending account's name.
func Vars(to, myName string, now time.Time) map[string]string {
	name, first := "", ""
	if addrs, err := mail.ParseAddressList(to); err == nil && len(addrs) > 0 {
		name = addrs[0].Name
	}
	if fields := strings.Fields(name); len(fields) > 0 {
		first = fields[0]
	}
	if first == "" {
		first = "there"
	}
	return map[string]string{
		"name":       name,
		"first_name": first,
		"my_name":    myName,
		"date":       now.Format("January 2, 2006"),
	}
}

// Serialize is the file form of t: a "subject:" block when there is a
// subject, then the body. It is the inverse of Parse.
func Serialize(t Template) string {
	if t.Subject == "" {
		return t.Body
	}
	return "---\nsubject: " + t.Subject + "\n---\n" + t.Body
}
