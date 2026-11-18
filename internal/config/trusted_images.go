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

// ImageSenderExclusionPrefix marks an entry that excludes one address from a
// trusted domain: "!person@example.com" next to "@example.com".
const ImageSenderExclusionPrefix = "!"

// IsTrustedImageSender reports whether from matches an entry in trusted. An
// entry matches the exact (case-insensitive) address, or, when it starts with
// "@", every address at that domain. An exclusion entry ("!address") always
// wins, so one sender can be kept out of a trusted domain.
func IsTrustedImageSender(trusted []string, from string) bool {
	addr := NormalizeImageSender(from)
	if addr == "" {
		return false
	}
	match := false
	for _, entry := range trusted {
		entry = strings.ToLower(strings.TrimSpace(entry))
		switch {
		case entry == "":
		case entry == ImageSenderExclusionPrefix+addr:
			return false
		case entry == addr, strings.HasPrefix(entry, "@") && strings.HasSuffix(addr, entry):
			match = true
		}
	}
	return match
}

// TrustChange says what ToggleTrustedImageSender did.
type TrustChange int

const (
	TrustUnchanged     TrustChange = iota // no usable address
	TrustAdded                            // exact address added
	TrustRemoved                          // exact address removed; no longer trusted
	TrustExcluded                         // address excluded from its trusted domain
	TrustExclusionDrop                    // exclusion removed; trusted again via its domain
)

// ToggleTrustedImageSender flips whether from is trusted, and returns the new
// list, what changed, and the normalized address ("" if from has no usable
// address, in which case trusted is returned unchanged):
//   - an exact entry for the address is removed;
//   - an address trusted only through an "@domain" entry gets an exclusion
//     ("!address"), because removing the domain would untrust everyone else;
//   - an excluded address has its exclusion removed (and is added exactly if
//     no domain entry trusts it any more);
//   - otherwise the exact address is added.
//
// Domain entries themselves are never changed.
func ToggleTrustedImageSender(trusted []string, from string) ([]string, TrustChange, string) {
	addr := NormalizeImageSender(from)
	if addr == "" {
		return trusted, TrustUnchanged, ""
	}
	exclusion := ImageSenderExclusionPrefix + addr
	without := func(target string) ([]string, bool) {
		out := make([]string, 0, len(trusted)+1)
		found := false
		for _, entry := range trusted {
			if strings.ToLower(strings.TrimSpace(entry)) == target {
				found = true
				continue
			}
			out = append(out, entry)
		}
		return out, found
	}
	if out, found := without(addr); found {
		if IsTrustedImageSender(out, addr) {
			// Still trusted through its domain: keep it out explicitly.
			return append(out, exclusion), TrustExcluded, addr
		}
		return out, TrustRemoved, addr
	}
	if out, found := without(exclusion); found {
		if IsTrustedImageSender(out, addr) {
			return out, TrustExclusionDrop, addr
		}
		return append(out, addr), TrustAdded, addr
	}
	if IsTrustedImageSender(trusted, addr) {
		return append(append([]string(nil), trusted...), exclusion), TrustExcluded, addr
	}
	return append(append([]string(nil), trusted...), addr), TrustAdded, addr
}
