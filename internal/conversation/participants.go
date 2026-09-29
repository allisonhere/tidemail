package conversation

import (
	"net/mail"
	"regexp"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
)

// Participant is one parsed address. Addr is lowercase; Name is the raw
// display name and may need unescaping and sanitizing before display.
type Participant struct {
	Name, Addr string
}

// MyAddresses returns the lowercase bare addresses among raw (account logins
// and From headers). Display names never count.
func MyAddresses(raw ...string) map[string]bool {
	me := map[string]bool{}
	for _, r := range raw {
		if addr := BareAddress(r); addr != "" {
			me[addr] = true
		}
	}
	return me
}

// BareAddress returns the lowercase address in s, or "" if there is none.
func BareAddress(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if addr, err := mail.ParseAddress(s); err == nil {
		return strings.ToLower(addr.Address)
	}
	if strings.Contains(s, "@") && !strings.ContainsAny(s, " <>") {
		return strings.ToLower(s)
	}
	return ""
}

// ParseParticipants parses address header values, tolerating malformed ones.
func ParseParticipants(fields ...string) []Participant {
	var out []Participant
	for _, f := range fields {
		if strings.TrimSpace(f) == "" {
			continue
		}
		list, err := mail.ParseAddressList(f)
		if err != nil {
			// Sloppy or hostile headers (a control character in one display
			// name rejects the whole list): split on commas and pull out each
			// <address> and the name before it.
			for _, part := range strings.Split(f, ",") {
				if m := angleAddressRe.FindStringSubmatch(part); m != nil {
					name := strings.Trim(strings.TrimSpace(part[:strings.Index(part, "<")]), `"`)
					out = append(out, Participant{Name: name, Addr: strings.ToLower(m[1])})
				} else if a := BareAddress(part); a != "" {
					out = append(out, Participant{Addr: a})
				}
			}
			continue
		}
		for _, a := range list {
			out = append(out, Participant{Name: strings.TrimSpace(a.Name), Addr: strings.ToLower(a.Address)})
		}
	}
	return out
}

var angleAddressRe = regexp.MustCompile(`<([^<>\s@]+@[^<>\s]+)>`)

// automatedLocalParts mark addresses nobody reads replies from: no-reply
// senders, bounce handlers, and notification robots.
var automatedLocalParts = []string{
	"noreply", "no-reply", "no_reply", "donotreply", "do-not-reply", "do_not_reply",
	"mailer-daemon", "postmaster", "bounce", "bounces", "notifications", "notification",
}

// AutomatedAddress is a conservative heuristic for robots and mailing lists.
func AutomatedAddress(addr string) bool {
	at := strings.LastIndex(addr, "@")
	if at <= 0 {
		return false
	}
	local, domain := addr[:at], addr[at+1:]
	for _, p := range automatedLocalParts {
		if local == p || strings.HasPrefix(local, p+"+") || strings.HasPrefix(local, p+"-") || strings.HasPrefix(local, p+".") {
			return true
		}
	}
	// Obvious mailing lists.
	if strings.HasSuffix(local, "-list") || local == "list" || strings.HasPrefix(domain, "lists.") ||
		domain == "googlegroups.com" || domain == "groups.io" {
		return true
	}
	return false
}

// Meaningful reports whether a message can decide a conversation's state:
// drafts and messages from robots (bounces, notifications) cannot.
func Meaningful(msg db.Message) bool {
	if hasFlag(msg.Flags, `\Draft`) {
		return false
	}
	from := BareAddress(msg.From)
	return from != "" && !AutomatedAddress(from)
}

func hasFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, flag) {
			return true
		}
	}
	return false
}
