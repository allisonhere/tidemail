package config

import (
	"net/mail"
	"strings"
)

// NormalizeImageSender extracts and lowercases the bare address from a From
// header value such as `"News" <News@Example.com>`. It returns "" when no
// address can be found.
func NormalizeImageSender(from string) string {
	from = strings.TrimSpace(from)
	if from == "" {
		return ""
	}
	if addr, err := mail.ParseAddress(from); err == nil {
		return strings.ToLower(addr.Address)
	}
	if i, j := strings.LastIndex(from, "<"), strings.LastIndex(from, ">"); i >= 0 && j > i {
		from = from[i+1 : j]
	}
	from = strings.ToLower(strings.TrimSpace(from))
	if !strings.Contains(from, "@") {
		return ""
	}
	return from
}

// IsTrustedImageSender reports whether from matches an entry in trusted. An
// entry matches the exact (case-insensitive) address, or, when it starts with
// "@", every address at that domain.
func IsTrustedImageSender(trusted []string, from string) bool {
	addr := NormalizeImageSender(from)
	if addr == "" {
		return false
	}
	for _, entry := range trusted {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == addr || (strings.HasPrefix(entry, "@") && strings.HasSuffix(addr, entry)) {
			return true
		}
	}
	return false
}

// ToggleTrustedImageSender adds the sender's exact address to trusted, or
// removes it when already present. Domain entries are left alone. It returns
// the new list, whether the sender was added, and the normalized address ("" if
// from has no usable address, in which case trusted is returned unchanged).
func ToggleTrustedImageSender(trusted []string, from string) ([]string, bool, string) {
	addr := NormalizeImageSender(from)
	if addr == "" {
		return trusted, false, ""
	}
	out := make([]string, 0, len(trusted)+1)
	removed := false
	for _, entry := range trusted {
		if strings.ToLower(strings.TrimSpace(entry)) == addr {
			removed = true
			continue
		}
		out = append(out, entry)
	}
	if removed {
		return out, false, addr
	}
	return append(out, addr), true, addr
}
