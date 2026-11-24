package ui

import (
	"sort"
	"strings"
	"unicode"
)

// folderCandidate is one folder offered to the fuzzy folder filter.
type folderCandidate struct {
	mailboxID int64
	label     string // what the user types against and sees, e.g. "Projects/Acme/Invoices"
}

// fuzzyScore reports whether every non-space character of query appears in
// target in order (case-insensitively) and how good the match is. Higher is
// better. Matches that are consecutive, start a word or path segment, or form a
// contiguous substring score more, so "acin" ranks Projects/Acme/Invoices
// above a folder that only scatters those letters.
func fuzzyScore(query, target string) (int, bool) {
	q := []rune(strings.ToLower(strings.Join(strings.Fields(query), "")))
	if len(q) == 0 {
		return 0, true
	}
	t := []rune(strings.ToLower(target))
	score, qi, prev := 0, 0, -2
	for ti, r := range t {
		if qi >= len(q) || r != q[qi] {
			continue
		}
		score++
		if ti == prev+1 {
			score += 4
		}
		if ti == 0 || isFolderSeparator(t[ti-1]) {
			score += 6
		}
		prev = ti
		qi++
	}
	if qi < len(q) {
		return 0, false
	}
	if i := strings.Index(strings.ToLower(target), string(q)); i >= 0 {
		score += 10
		if i == 0 {
			score += 6
		}
	}
	return score, true
}

func isFolderSeparator(r rune) bool {
	return r == '/' || r == '.' || r == '-' || r == '_' || r == ' ' || unicode.IsPunct(r)
}

// rankFolders keeps the candidates that match query, best match first. Ties
// keep the shorter label, then the original order. An empty query returns the
// candidates unchanged.
func rankFolders(query string, candidates []folderCandidate) []folderCandidate {
	if strings.TrimSpace(query) == "" {
		return append([]folderCandidate(nil), candidates...)
	}
	type scored struct {
		folderCandidate
		score int
	}
	var matches []scored
	for _, c := range candidates {
		if s, ok := fuzzyScore(query, c.label); ok {
			matches = append(matches, scored{c, s})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return len(matches[i].label) < len(matches[j].label)
	})
	out := make([]folderCandidate, len(matches))
	for i, m := range matches {
		out[i] = m.folderCandidate
	}
	return out
}
