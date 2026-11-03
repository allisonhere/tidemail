package mailtag

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Definition maps an IMAP keyword to a local name and color. The keyword is
// immutable; names and colors are not transmitted by IMAP.
type Definition struct {
	AccountID int64
	Keyword   string
	Name      string
	Color     string
}

// ValidateKeyword accepts IMAP flag-keywords, excluding protocol status flags.
func ValidateKeyword(keyword string) error {
	if keyword == "" {
		return fmt.Errorf("enter an IMAP keyword")
	}
	for _, b := range []byte(keyword) {
		if b <= 32 || b >= 127 || strings.ContainsRune("(){%*\"\\]", rune(b)) {
			return fmt.Errorf("keyword must be an ASCII IMAP atom without spaces or special characters")
		}
	}
	if IsStatus(keyword) {
		return fmt.Errorf("%s is a mail status keyword, not a tag", keyword)
	}
	return nil
}

// statusKeywords are set by servers and clients to describe a message rather
// than to tag it, so they are never shown or offered as tags.
var statusKeywords = []string{
	"junk", "nonjunk", "notjunk", "$junk", "$notjunk", "$forwarded", "$mdnsent", "$phishing",
	"$seen", "$deleted", "$answered", "$draft", "$flagged",
	"$hasattachment", "$hasnoattachment", "$submitpending", "$submitted",
}

// IsStatus reports whether keyword is a mail status keyword, not a tag.
func IsStatus(keyword string) bool {
	return slices.Contains(statusKeywords, strings.ToLower(keyword))
}

// StatusKeywords returns the lowercase status keywords.
func StatusKeywords() []string { return slices.Clone(statusKeywords) }

func Keywords(flags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, flag := range flags {
		key := strings.ToLower(flag)
		if ValidateKeyword(flag) == nil && !seen[key] {
			out = append(out, flag)
			seen[key] = true
		}
	}
	return out
}

func Has(flags []string, keyword string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, keyword) {
			return true
		}
	}
	return false
}

func Default(accountID int64, keyword string) Definition {
	d := Definition{AccountID: accountID, Keyword: keyword, Name: keyword}
	switch strings.ToLower(keyword) {
	case "$label1":
		d.Name, d.Color = "Important", "#ff0000"
	case "$label2":
		d.Name, d.Color = "Work", "#ff9900"
	case "$label3":
		d.Name, d.Color = "Personal", "#009900"
	case "$label4":
		d.Name, d.Color = "To Do", "#3333ff"
	case "$label5":
		d.Name, d.Color = "Later", "#993399"
	}
	return d
}

func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 80 || !utf8.ValidString(name) {
		return fmt.Errorf("tag name must contain 1 to 80 characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("tag name cannot contain control characters")
		}
	}
	return nil
}
