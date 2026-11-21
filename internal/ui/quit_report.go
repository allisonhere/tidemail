package ui

import (
	"fmt"
	"strings"
)

// maxQuitReportItems bounds how many subjects a quit-time error lists, and
// maxQuitSubjectRunes how much of each subject it shows.
const (
	maxQuitReportItems  = 3
	maxQuitSubjectRunes = 60
)

// quitFailures gathers what went wrong while flushing pending work on quit, so
// the one error printed afterwards says how much failed, which items, and why,
// instead of only the first cause.
type quitFailures struct {
	total int
	items []string
	first error
}

func (f *quitFailures) add(item string, err error) {
	f.items = append(f.items, item)
	f.addCause(err)
}

// addCause records a failure that names no item, such as a cache write that
// failed after the server had already accepted the action.
func (f *quitFailures) addCause(err error) {
	if f.first == nil {
		f.first = err
	}
}

// collect records one destructive action's outcome. Failed messages are given
// the server's error when there was one: within an action, Err is the first
// error of any batch and may be an unrelated cache write. A cache-only failure
// names no message, since the server already applied the action.
func (f *quitFailures) collect(result DestructiveActionResultMsg) {
	cause := result.RemoteErr
	if cause == nil {
		cause = result.Err
	}
	for _, msg := range result.Failed {
		f.add(string(result.Kind)+" "+quotedSubject(msg.Subject), cause)
	}
	if len(result.Failed) == 0 && result.Err != nil {
		f.addCause(fmt.Errorf("%s done on the server, local cache not updated (next sync reconciles): %w", result.Kind, result.Err))
	}
}

// collectSend records one delivery. A message that went out but whose cleanup
// failed is labelled as sent, so it is not mistaken for one to retry.
func (f *quitFailures) collectSend(subject string, result MessageSentMsg) {
	switch {
	case result.Err != nil:
		f.add(quotedSubject(subject), result.Err)
	case result.CleanupErr != nil:
		f.add(quotedSubject(subject)+" (sent; cleanup failed)", result.CleanupErr)
	}
}

// error renders "2 of 5 <what>: item, item: first cause", to follow a caller's
// "pending … failed:" prefix. It wraps the first cause, so errors.Is still sees it.
func (f *quitFailures) error(what string) error {
	if f.first == nil {
		return nil
	}
	n := len(f.items)
	if n == 0 {
		return f.first
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d %s: ", n, f.total, what)
	shown := f.items
	if n > maxQuitReportItems {
		shown = shown[:maxQuitReportItems]
	}
	b.WriteString(strings.Join(shown, ", "))
	if n > maxQuitReportItems {
		fmt.Fprintf(&b, " and %d more", n-maxQuitReportItems)
	}
	return fmt.Errorf("%s: %w", b.String(), f.first)
}

// quotedSubject quotes a subject for a one-line report; %q also escapes any
// control characters, so a subject cannot write escape codes to the terminal.
func quotedSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "(no subject)"
	}
	if runes := []rune(subject); len(runes) > maxQuitSubjectRunes {
		subject = string(runes[:maxQuitSubjectRunes-1]) + "…"
	}
	return fmt.Sprintf("%q", subject)
}

// PendingSummary describes work that was still waiting when the program ended
// abnormally, and what became of it. It is "" when nothing was pending. Quitting
// that way skips the flush, so this tells the user where each kind stands.
func (m Model) PendingSummary() string {
	var parts []string
	if n := len(m.pendingSends); n > 0 {
		parts = append(parts, fmt.Sprintf("%d unsent message(s) stay in the Outbox (O) and are sent when due once TideMail runs again (a send that was in progress shows as unconfirmed there; check Sent before retrying it)", n))
	}
	actions := 0
	for _, a := range m.pendingDestructiveActions {
		actions += len(a.Entries)
	}
	if actions > 0 {
		parts = append(parts, fmt.Sprintf("%d archive/move/delete action(s) were pending and may not have been applied; the next sync shows the real state", actions))
	}
	return strings.Join(parts, "; ")
}
