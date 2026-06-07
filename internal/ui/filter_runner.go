package ui

import (
	"context"
	"encoding/json"
	"fmt"
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

// generateFilterCmd asks the configured AI provider to turn an English
// description into a structured rule, validated against the account's folders.
func (m *Model) generateFilterCmd(english string, accountID int64) tea.Cmd {
	aicfg := m.cfg.AI
	folders := m.accountFolderNames(accountID)
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

// applyRulesCmd applies all enabled rules to the messages in the given mailboxes.
// When dryRun is true it only counts matches without changing anything.
func (m *Model) applyRulesCmd(mailboxIDs []int64, dryRun bool) tea.Cmd {
	database := m.db
	records, _ := database.ListRules()
	rules := compileRules(records)
	targets := make([]mailboxTarget, 0, len(mailboxIDs))
	for _, id := range mailboxIDs {
		if mb := m.mailboxByID(id); mb != nil {
			targets = append(targets, mailboxTarget{mailbox: *mb, acfg: m.accountCfgForMailbox(id)})
		}
	}
	return func() tea.Msg {
		if len(rules) == 0 {
			return FilterRunMsg{DryRun: dryRun, Err: fmt.Errorf("no enabled rules")}
		}
		clients := map[int64]*imapClient.Client{}
		defer func() {
			for _, c := range clients {
				c.Close()
			}
		}()
		var matched, applied int
		var firstErr error
		for _, t := range targets {
			msgs, err := database.ListMessages(t.mailbox.ID)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			for _, msg := range msgs {
				cr, ok := firstMatch(rules, t.mailbox.AccountID, msg)
				if !ok {
					continue
				}
				matched++
				if dryRun {
					continue
				}
				client, err := clientForAccount(clients, t)
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				err = applyFilterAction(ctx, database, client, t.mailbox, msg, cr.rule.Action)
				cancel()
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				applied++
			}
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
		if err := applyFilterAction(ctx, database, client, mailbox, sm, cr.rule.Action); err != nil {
			// The action failed, so the message is still in the mailbox — keep
			// reporting it as new.
			if firstErr == nil {
				firstErr = err
			}
			survivors = append(survivors, nm)
			continue
		}
	}
	return survivors, firstErr
}

// clientForAccount lazily connects (and caches) one IMAP client per account for
// the duration of a run. Accounts without an IMAP host yield a nil client, which
// applyFilterAction treats as local-only.
func clientForAccount(clients map[int64]*imapClient.Client, t mailboxTarget) (*imapClient.Client, error) {
	if t.acfg.IMAPHost == "" {
		return nil, nil
	}
	if c, ok := clients[t.mailbox.AccountID]; ok {
		return c, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := imapClient.New(t.acfg)
	if err := c.Connect(ctx); err != nil {
		return nil, err
	}
	clients[t.mailbox.AccountID] = c
	return c, nil
}
