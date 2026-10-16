package plugin

import (
	"regexp"
	"strings"
)

// MessageAuth is the sender-check outcome of a message, as the user's own mail
// server recorded it in the message headers. It is enum-only derived data, not
// the headers themselves. Anything the server did not report is empty.
//
// A server that does not strip a forged Authentication-Results header lets a
// sender claim "pass", so treat these as evidence, not proof.
type MessageAuth struct {
	// SPF, DKIM and DMARC are one of: pass, fail, softfail, neutral, none,
	// temperror, permerror, policy, other.
	SPF   string `json:"spf,omitempty"`
	DKIM  string `json:"dkim,omitempty"`
	DMARC string `json:"dmarc,omitempty"`
	// SpamFlag is true when the server's own spam filter marked the message
	// (an X-Spam-Status header beginning "Yes").
	SpamFlag bool `json:"spam_flag,omitempty"`
}

var authResult = regexp.MustCompile(`(?i)\b(spf|dkim|dmarc)\s*=\s*([a-z]+)`)

var authValues = map[string]bool{
	"pass": true, "fail": true, "softfail": true, "neutral": true, "none": true,
	"temperror": true, "permerror": true, "policy": true,
}

func authValue(s string) string {
	s = strings.ToLower(s)
	if authValues[s] {
		return s
	}
	return "other"
}

// AuthFromHeaders reads the sender-check results out of a message's cached
// auth-header text (TideMail keeps Authentication-Results, Received-SPF and
// X-Spam-Status as alternating "Label" and "value" lines). It returns nil when
// the message carries none.
func AuthFromHeaders(headers string) *MessageAuth {
	var a MessageAuth
	lines := strings.Split(headers, "\n")
	for i := 0; i+1 < len(lines); i++ {
		value := strings.TrimSpace(lines[i+1])
		switch lines[i] {
		case "Authentication-Results":
			for _, m := range authResult.FindAllStringSubmatch(value, -1) {
				res := authValue(m[2])
				switch strings.ToLower(m[1]) {
				case "spf":
					if a.SPF == "" {
						a.SPF = res
					}
				case "dmarc":
					if a.DMARC == "" {
						a.DMARC = res
					}
				case "dkim":
					// A message can carry several signatures, and a third
					// party's failing one says nothing about the sender's own.
					if a.DKIM == "" || (res == "pass" && a.DKIM != "pass") {
						a.DKIM = res
					}
				}
			}
		case "Received-SPF":
			if a.SPF == "" {
				if f := strings.Fields(value); len(f) > 0 {
					a.SPF = authValue(f[0])
				}
			}
		case "X-Spam-Status":
			if strings.HasPrefix(strings.ToLower(value), "yes") {
				a.SpamFlag = true
			}
		}
	}
	if a == (MessageAuth{}) {
		return nil
	}
	return &a
}
