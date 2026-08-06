package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/ai"
	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/filter"
	imapClient "github.com/allisonhere/tide/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
)

// compiledRule pairs a stored rule record with its decoded definition.
type compiledRule struct {
	rec  db.RuleRecord
	rule filter.Rule
}

// compileRules decodes all enabled rules, in evaluation (priority) order.
func compileRules(records []db.RuleRecord) []compiledRule {
	out := make([]compiledRule, 0, len(records))
	for _, rec := range records {
		if !rec.Enabled {
			continue
		}
		var r filter.Rule
		if err := json.Unmarshal([]byte(rec.JSON), &r); err != nil {
			continue
		}
		out = append(out, compiledRule{rec: rec, rule: r})
	}
	return out
}

// firstMatch returns the first enabled rule (priority order) whose account scope
// includes accountID and whose conditions match msg.
func firstMatch(rules []compiledRule, accountID int64, msg db.Message) (compiledRule, bool) {
	for _, cr := range rules {
		if cr.rec.AccountID != 0 && cr.rec.AccountID != accountID {
			continue
		}
		if cr.rule.Matches(msg) {
			return cr, true
		}
	}
	return compiledRule{}, false
}

// accountFolderNames returns the folder names for an account, used to validate
// AI-generated "move" targets.
func (m Model) accountFolderNames(accountID int64) []string {
	var names []string
	for _, mb := range m.mailboxes {
		if mb.AccountID == accountID {
			names = append(names, mb.Name)
		}
	}
	return names
}

// allAccountFolderNames returns the de-duplicated union of folder names across
// all accounts, used to validate "move" targets for an All-accounts rule.
func (m Model) allAccountFolderNames() []string {
	seen := map[string]struct{}{}
	var names []string
	for _, mb := range m.mailboxes {
		key := strings.ToLower(mb.Name)
		if _, ok := seen[key]; ok || mb.Name == "" {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, mb.Name)
	}
	return names
}

// generateFilterCmd asks the configured AI provider to turn an English
// description into a structured rule, validated against the account's folders.
func (m *Model) generateFilterCmd(english string, accountID int64) tea.Cmd {
	aicfg := m.cfg.AI
	folders := m.accountFolderNames(accountID)
	if accountID == 0 {
		// All-accounts rule: offer the union of every account's folders so the AI
		// can target one that exists somewhere.
		folders = m.allAccountFolderNames()
	}
	return func() tea.Msg {
		provider, err := ai.New(aicfg)
		if err != nil {
			return FilterGeneratedMsg{English: english, Err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		raw, err := provider.Complete(ctx, filter.Prompt(english, folders))
		if err != nil {
			return FilterGeneratedMsg{English: english, Err: err}
		}
		rule, err := filter.Parse(raw, folders)
		if err != nil {
			return FilterGeneratedMsg{English: english, Err: err}
		}
		return FilterGeneratedMsg{English: english, Rule: rule}
	}
}

// mailboxTarget bundles a mailbox with its account config for a run command.
type mailboxTarget struct {
	mailbox db.Mailbox
	acfg    config.AccountConfig
}

type filterFolderTarget struct {
	account    db.Account
	acfg       config.AccountConfig
	configured bool
}

// prepareFilterFoldersCmd resolves or creates a move rule's target folder in
// every account covered by the scope selected during rule creation.
func (m *Model) prepareFilterFoldersCmd(intent filterSaveIntent) tea.Cmd {
	database := m.db
	sessions := m.sessions
	name := strings.TrimSpace(m.filterManager.draft.Action.Target)
	targets := make([]filterFolderTarget, 0, len(m.accounts))
	for _, account := range m.accounts {
		if m.filterManager.draftAcct != 0 && account.ID != m.filterManager.draftAcct {
			continue
		}
		var acfg config.AccountConfig
		configured := false
		for _, candidate := range m.cfg.Accounts {
			if candidate.Name == account.Name {
				acfg = candidate
				configured = true
				break
			}
		}
		targets = append(targets, filterFolderTarget{account: account, acfg: acfg, configured: configured})
	}
	return func() tea.Msg {
		result := FilterFoldersPreparedMsg{Intent: intent}
		if name == "" {
			result.Err = fmt.Errorf("move action requires a target folder")
			return result
		}
		if len(targets) == 0 {
			result.Err = fmt.Errorf("no accounts are available for this rule")
			return result
		}
		for _, target := range targets {
			if mb, found, err := resolveFolder(database, target.account.ID, name); err != nil {
				result.Err = fmt.Errorf("prepare folder for %s: %w", target.account.Name, err)
				return result
			} else if found {
				result.Mailboxes = append(result.Mailboxes, mb)
				continue
			}
			if !target.configured {
				result.Err = fmt.Errorf("create folder for %s: account configuration not found", target.account.Name)
				return result
			}

			var mailbox db.Mailbox
			var err error
			if target.acfg.IMAPHost == "" {
				mailbox, err = resolveOrCreateFolder(context.Background(), database, nil, target.account.ID, name)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				err = sessions.Do(ctx, target.acfg, func(client *imapClient.Client) error {
					mailbox, err = resolveOrCreateFolder(ctx, database, client, target.account.ID, name)
					return err
				})
				cancel()
			}
			if err != nil {
				result.Err = fmt.Errorf("create folder for %s: %w", target.account.Name, err)
				return result
			}
			result.Mailboxes = append(result.Mailboxes, mailbox)
		}
		return result
	}
}

// applyRulesCmd applies enabled rules to the messages in the given mailboxes.
// When dryRun is true it only counts matches without changing anything. A
// non-zero onlyRuleID narrows the run to that single rule, so a per-rule
// test/run reports what that rule does rather than what the whole set does.
func (m *Model) applyRulesCmd(mailboxIDs []int64, dryRun bool, onlyRuleID int64) tea.Cmd {
	database := m.db
	records, _ := database.ListRules()
	if onlyRuleID != 0 {
		kept := records[:0]
		for _, rec := range records {
			if rec.ID == onlyRuleID {
				kept = append(kept, rec)
			}
		}
		records = kept
	}
	rules := compileRules(records)
	targets := make([]mailboxTarget, 0, len(mailboxIDs))
	for _, id := range mailboxIDs {
		if mb := m.mailboxByID(id); mb != nil {
			targets = append(targets, mailboxTarget{mailbox: *mb, acfg: m.accountCfgForMailbox(id)})
		}
	}
	sessions := m.sessions
	return func() tea.Msg {
		if len(rules) == 0 {
			return FilterRunMsg{DryRun: dryRun, Err: fmt.Errorf("no enabled rules")}
		}
		var matched, applied int
		var firstErr error
		recordErr := func(err error) {
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		for _, t := range targets {
			msgs, err := database.ListMessages(t.mailbox.ID)
			if err != nil {
				recordErr(err)
				continue
			}
			// Match first, then act: a connection is only needed (and the
			// account's pooled session only held) when something matched.
			type pendingAction struct {
				msg db.Message
				cr  compiledRule
			}
			var pending []pendingAction
			for _, msg := range msgs {
				cr, ok := firstMatch(rules, t.mailbox.AccountID, msg)
				if !ok {
					continue
				}
				matched++
				if dryRun {
					continue
				}
				pending = append(pending, pendingAction{msg: msg, cr: cr})
			}
			if len(pending) == 0 {
				continue
			}
			apply := func(client *imapClient.Client) error {
				for _, p := range pending {
					ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
					acted, err := applyFilterAction(ctx, database, client, t.mailbox, p.msg, p.cr.rule.Action)
					cancel()
					recordErr(err)
					if err == nil && acted {
						applied++
					}
				}
				return nil
			}
			if t.acfg.IMAPHost == "" {
				// Local-only account: applyFilterAction treats a nil client as local.
				recordErr(apply(nil))
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			recordErr(sessions.Do(ctx, t.acfg, apply))
			cancel()
		}
		return FilterRunMsg{Matched: matched, Applied: applied, DryRun: dryRun, Err: firstErr}
	}
}

// applyRulesOnArrival runs enabled rules against just-fetched messages during a
// sync, using the already-connected client. newMsgs may lack DB ids (they come
// straight off the wire), so it re-reads the mailbox to recover ids by UID.
//
// It returns the messages that should still count as new arrivals in this
// mailbox — i.e. those NOT acted on by a rule. A message that was moved,
// archived, sent to spam, deleted, or marked read is filtered out so the caller
// does not report it in NewCount or raise a "new mail" notification for it.
func applyRulesOnArrival(ctx context.Context, database *db.DB, client *imapClient.Client, mailbox db.Mailbox, newMsgs []db.Message) ([]db.Message, error) {
	if len(newMsgs) == 0 {
		return newMsgs, nil
	}
	records, err := database.ListRules()
	if err != nil || len(records) == 0 {
		return newMsgs, err
	}
	rules := compileRules(records)
	if len(rules) == 0 {
		return newMsgs, nil
	}
	stored, err := database.ListMessages(mailbox.ID)
	if err != nil {
		return newMsgs, err
	}
	byUID := make(map[uint32]db.Message, len(stored))
	for _, sm := range stored {
		byUID[sm.UID] = sm
	}
	survivors := make([]db.Message, 0, len(newMsgs))
	var firstErr error
	for _, nm := range newMsgs {
		sm, ok := byUID[nm.UID]
		if !ok {
			survivors = append(survivors, nm)
			continue
		}
		cr, ok := firstMatch(rules, mailbox.AccountID, sm)
		if !ok {
			survivors = append(survivors, nm)
			continue
		}
		acted, err := applyFilterAction(ctx, database, client, mailbox, sm, cr.rule.Action)
		if err != nil || !acted {
			// The action failed or was a no-op, so the message is still in the
			// mailbox — keep reporting it as new.
			if err != nil && firstErr == nil {
				firstErr = err
			}
			survivors = append(survivors, nm)
		}
	}
	return survivors, firstErr
}
