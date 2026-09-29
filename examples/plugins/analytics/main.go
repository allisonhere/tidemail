// Command tidemail-plugin-analytics-example is the smallest useful report
// plugin. It is a teaching example for report.run: in round 1 it asks
// TideMail for three aggregate queries; in round 2 it turns the results into
// a plain-text report. It never sees a message record, only counts.
//
// It uses only the standard library, so it can be copied out of this
// repository as is. See docs/plugins/queries.md.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const apiVersion = 1

type request struct {
	API       int             `json:"api"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Method    string          `json:"method"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type response struct {
	API       int        `json:"api"`
	Type      string     `json:"type"`
	RequestID string     `json:"request_id"`
	OK        bool       `json:"ok"`
	Data      any        `json:"data,omitempty"`
	Error     *errorBody `json:"error,omitempty"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// reportRequest is report.run's data. Results are keyed by the names this
// plugin gave its queries in the previous round.
type reportRequest struct {
	Round   int                        `json:"round"`
	Results map[string]json.RawMessage `json:"results"`
}

// The parts of the query results this plugin reads.
type volume struct {
	Received int `json:"received"`
	Sent     int `json:"sent"`
	Buckets  []struct {
		Received int `json:"received"`
	} `json:"buckets"`
}

type categories struct {
	Categories []struct {
		Category string `json:"category"`
		Count    int    `json:"count"`
	} `json:"categories"`
}

type attention struct {
	NeedsYou int `json:"needs_you_current"`
	Waiting  int `json:"waiting_current"`
	Snoozed  int `json:"snoozed_messages_current"`
}

// queries is round 1's request: names chosen by the plugin, each a
// structured query TideMail validates and runs.
var queries = map[string]any{
	"volume":     map[string]any{"method": "analytics.volume", "range": "30d", "group_by": "day"},
	"categories": map[string]any{"method": "analytics.categories", "range": "30d"},
	"attention":  map[string]any{"method": "analytics.attention"},
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr))
}

func run(stdin io.Reader, stdout, stderr io.Writer) int {
	var req request
	if err := json.NewDecoder(stdin).Decode(&req); err != nil {
		_, _ = fmt.Fprintln(stderr, "analytics-example: cannot read request:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(handle(req)); err != nil {
		_, _ = fmt.Fprintln(stderr, "analytics-example: cannot write response:", err)
		return 1
	}
	return 0
}

func handle(req request) response {
	resp := response{API: apiVersion, Type: "response", RequestID: req.RequestID}
	if req.API != apiVersion {
		return fail(resp, "unsupported_api", fmt.Sprintf("this plugin speaks API %d", apiVersion))
	}
	switch req.Method {
	case "ping":
		resp.OK, resp.Data = true, map[string]string{"message": "pong"}
	case "report.run":
		var rr reportRequest
		if err := json.Unmarshal(req.Data, &rr); err != nil {
			return fail(resp, "bad_request", "cannot read report request")
		}
		if rr.Round == 1 {
			// Ask; TideMail answers in round 2.
			resp.OK, resp.Data = true, map[string]any{"queries": queries}
			return resp
		}
		text, err := render(rr.Results)
		if err != nil {
			return fail(resp, "bad_results", err.Error())
		}
		// A string report is shown as is, line by line.
		resp.OK, resp.Data = true, map[string]any{"report": text}
	default:
		return fail(resp, "unsupported_method", req.Method)
	}
	return resp
}

func render(results map[string]json.RawMessage) (string, error) {
	var v volume
	var c categories
	var a attention
	for name, dst := range map[string]any{"volume": &v, "categories": &c, "attention": &a} {
		if err := json.Unmarshal(results[name], dst); err != nil {
			return "", fmt.Errorf("result %q: %v", name, err)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Last 30 days: %d received, %d sent\n", v.Received, v.Sent)
	daily := make([]int, len(v.Buckets))
	for i, bucket := range v.Buckets {
		daily[i] = bucket.Received
	}
	fmt.Fprintf(&b, "Received per day: %s\n\n", sparkline(daily))
	for _, cat := range c.Categories {
		fmt.Fprintf(&b, "%-12s %4d\n", cat.Category, cat.Count)
	}
	fmt.Fprintf(&b, "\nNeeds You %d · Waiting on Them %d · Snoozed %d", a.NeedsYou, a.Waiting, a.Snoozed)
	return b.String(), nil
}

// sparkline draws values with block characters. Plain text only: TideMail
// strips escape sequences, and colors are TideMail's to choose.
func sparkline(values []int) string {
	const blocks = "▁▂▃▄▅▆▇█"
	peak := 0
	for _, v := range values {
		peak = max(peak, v)
	}
	var b strings.Builder
	for _, v := range values {
		i := 0
		if peak > 0 {
			i = v * 7 / peak
		}
		b.WriteRune([]rune(blocks)[i])
	}
	return b.String()
}

func fail(resp response, code, message string) response {
	resp.OK = false
	resp.Error = &errorBody{Code: code, Message: message}
	return resp
}
