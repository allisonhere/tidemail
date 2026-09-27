package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Reports: the one way a plugin reads data beyond a single message.
//
// The process model stays one-shot. TideMail sends report.run; the plugin
// answers with either queries (plus opaque state) or a final report. TideMail
// validates and permission-checks every query, runs them itself, and starts
// the plugin again with the results and the state echoed back. Each round is
// an ordinary invocation with the usual timeout, environment, and output
// limits; a report is bounded in rounds, queries, state size, result size,
// and total time.

// CapabilityReport is the manifest capability that opts a plugin into
// report.run. It grants no data by itself: every query needs its permission.
const CapabilityReport = "report.run"

// MethodReportRun is sent for every round of a report.
const MethodReportRun = "report.run"

// Report limits.
const (
	MaxReportRounds          = 8
	MaxReportQueriesPerRound = 8
	MaxReportStateBytes      = 64 << 10
	MaxReportResultsBytes    = 4 << 20
	ReportTimeout            = 30 * time.Second
)

// ReportContext is what TideMail tells the plugin about where the report was
// started. It also resolves the current_account and current_mailbox scopes.
type ReportContext struct {
	// AccountName and MailboxName name the folder the user was looking at,
	// when there is one.
	AccountName string `json:"account_name,omitempty"`
	MailboxName string `json:"mailbox_name,omitempty"`
	// Now is when the report started (RFC 3339, local time); every round of
	// one report uses the same value, so ranges do not drift.
	Now string `json:"now"`
	// Timezone is the IANA name analytics buckets use.
	Timezone string `json:"timezone"`
}

// ReportRequest is report.run's data.
type ReportRequest struct {
	Round   int                        `json:"round"`
	Context ReportContext              `json:"context"`
	State   json.RawMessage            `json:"state,omitempty"`
	Results map[string]json.RawMessage `json:"results,omitempty"`
}

type reportResponse struct {
	Queries map[string]json.RawMessage `json:"queries"`
	State   json.RawMessage            `json:"state"`
	Report  json.RawMessage            `json:"report"`
}

// ReportResult is a finished report.
type ReportResult struct {
	// Report is the plugin's final output. It is display-only: TideMail
	// sanitizes and draws it and never acts on it.
	Report  json.RawMessage
	Rounds  int
	Queries int
}

// ReportOptions configures RunReport.
type ReportOptions struct {
	// Timeout bounds each round's process. Zero means DefaultTimeout.
	Timeout time.Duration
	// Settings and Secrets are the plugin's own resolved settings and secrets.
	Settings map[string]any
	Secrets  map[string]string

	// acquire, when set, is held for each round's process.
	acquire func(ctx context.Context) (func(), error)
}

// Report runs a report for a plugin that declares the report.run
// capability, executing its queries with exec.
func (m *Manager) Report(ctx context.Context, pluginID string, rc ReportContext, exec QueryExecutor) (ReportResult, error) {
	p, ok := m.Plugin(pluginID)
	if !ok {
		return ReportResult{}, fmt.Errorf("%w %q", ErrUnknownPlugin, pluginID)
	}
	var req Request
	secrets := m.applySettings(p, &req)
	return RunReport(ctx, p, rc, exec, ReportOptions{
		Timeout:  m.timeout(),
		Settings: req.Settings,
		Secrets:  secrets,
		acquire:  func(ctx context.Context) (func(), error) { return m.acquire(ctx, pluginID) },
	})
}

// RunReport drives one report through TideMail's normal process runner. The
// Manager uses it with the plugin's stored settings; developer tooling uses it
// with fixtures.
func RunReport(ctx context.Context, p Plugin, rc ReportContext, exec QueryExecutor, opts ReportOptions) (ReportResult, error) {
	id := p.Manifest.ID
	if !p.Manifest.HasCapability(CapabilityReport) {
		return ReportResult{}, fmt.Errorf("plugin %q: %w: %s is not declared", id, ErrPermissionDenied, CapabilityReport)
	}
	if exec == nil {
		return ReportResult{}, fmt.Errorf("plugin %q: no query executor", id)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, ReportTimeout)
	defer cancel()

	result := ReportResult{}
	next := ReportRequest{Round: 1, Context: rc}
	for {
		result.Rounds = next.Round
		resp, err := runReportRound(ctx, p, next, timeout, opts)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return result, fmt.Errorf("plugin %q: report did not finish within %s", id, ReportTimeout)
			}
			return result, err
		}
		if len(resp.Report) > 0 {
			result.Report = resp.Report
			return result, nil
		}
		if next.Round >= MaxReportRounds {
			return result, fmt.Errorf("plugin %q: report did not finish within %d rounds", id, MaxReportRounds)
		}
		queries, err := checkReportQueries(p, resp.Queries)
		if err != nil {
			return result, fmt.Errorf("plugin %q: %w", id, err)
		}
		results, err := runReportQueries(ctx, exec, queries, rc)
		if err != nil {
			return result, fmt.Errorf("plugin %q: %w", id, err)
		}
		result.Queries += len(queries)
		next = ReportRequest{Round: next.Round + 1, Context: rc, State: resp.State, Results: results}
	}
}

func runReportRound(ctx context.Context, p Plugin, rr ReportRequest, timeout time.Duration, opts ReportOptions) (reportResponse, error) {
	id := p.Manifest.ID
	data, err := json.Marshal(rr)
	if err != nil {
		return reportResponse{}, fmt.Errorf("plugin %q: encode report request: %w", id, err)
	}
	req, err := NewRequest(MethodReportRun, data)
	if err != nil {
		return reportResponse{}, err
	}
	req.Settings = opts.Settings
	release := func() {}
	if opts.acquire != nil {
		if release, err = opts.acquire(ctx); err != nil {
			return reportResponse{}, fmt.Errorf("plugin %q: %w", id, err)
		}
	}
	resp, err := invoke(ctx, p, req, timeout, opts.Secrets)
	release()
	if err != nil {
		return reportResponse{}, err
	}
	if !resp.OK {
		return reportResponse{}, fmt.Errorf("plugin %q: %w", id, resp.Error)
	}
	var out reportResponse
	if err := json.Unmarshal(resp.Data, &out); err != nil || resp.Data == nil {
		return reportResponse{}, fmt.Errorf("plugin %q: report.run data must be an object with queries or report", id)
	}
	if isJSONNull(out.Report) {
		out.Report = nil
	}
	switch {
	case len(out.Report) > 0 && len(out.Queries) > 0:
		return reportResponse{}, fmt.Errorf("plugin %q: a report.run response has either queries or report, not both", id)
	case len(out.Report) == 0 && len(out.Queries) == 0:
		return reportResponse{}, fmt.Errorf("plugin %q: a report.run response needs queries or a report", id)
	case len(out.State) > MaxReportStateBytes:
		return reportResponse{}, fmt.Errorf("plugin %q: report state exceeds %d bytes", id, MaxReportStateBytes)
	}
	return out, nil
}

// checkReportQueries validates every query and its permission before any of
// them runs, so a bad query discloses nothing.
func checkReportQueries(p Plugin, raw map[string]json.RawMessage) ([]Query, error) {
	if len(raw) > MaxReportQueriesPerRound {
		return nil, fmt.Errorf("a round may ask at most %d queries, got %d", MaxReportQueriesPerRound, len(raw))
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	queries := make([]Query, 0, len(raw))
	for _, name := range names {
		q, err := ParseQuery(name, raw[name])
		if err != nil {
			return nil, err
		}
		if ok, missing := QueryPermission(p.Manifest.Permissions, q.Method); !ok {
			return nil, fmt.Errorf("%w: %w", ErrPermissionDenied, &QueryError{Name: name, Code: QueryErrPermission, Message: q.Method + " needs " + missing})
		}
		queries = append(queries, q)
	}
	return queries, nil
}

func runReportQueries(ctx context.Context, exec QueryExecutor, queries []Query, rc ReportContext) (map[string]json.RawMessage, error) {
	results := make(map[string]json.RawMessage, len(queries))
	total := 0
	for _, q := range queries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := exec.ExecuteQuery(ctx, q, rc)
		if err != nil {
			var qe *QueryError
			if errors.As(err, &qe) {
				if qe.Name == "" {
					qe.Name = q.Name
				}
				return nil, qe
			}
			return nil, &QueryError{Name: q.Name, Code: QueryErrFailed, Message: err.Error()}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, &QueryError{Name: q.Name, Code: QueryErrFailed, Message: "encode result: " + err.Error()}
		}
		if total += len(raw); total > MaxReportResultsBytes {
			return nil, fmt.Errorf("query results for one round exceed %d bytes; ask for fewer rows", MaxReportResultsBytes)
		}
		results[q.Name] = raw
	}
	return results, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
