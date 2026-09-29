// Command tidemail-plugin-example is the smallest useful TideMail plugin. It
// is a teaching example: read one JSON request from stdin, write one JSON
// response to stdout, exit.
//
// It answers ping, and classifies message metadata with one deterministic
// rule: a subject containing "invoice" gets category=billing. It uses only
// the standard library, so it can be copied out of this repository as is.
// See docs/plugins/getting-started.md.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// apiVersion is the TideMail plugin API this plugin speaks.
const apiVersion = 1

// request is the envelope TideMail writes to stdin.
type request struct {
	API       int             `json:"api"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Method    string          `json:"method"`
	Settings  map[string]any  `json:"settings,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// response is the envelope this plugin writes to stdout: data when ok,
// error when not.
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

// metadata is the part of the message.metadata payload this plugin uses.
// TideMail sends more fields; unknown ones are simply ignored.
type metadata struct {
	ID      int64  `json:"id"`
	Subject string `json:"subject"`
}

type annotation struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"`
}

// result is the data of a message.metadata response. TideMail stores
// annotations; everything else in data is only shown to the user.
type result struct {
	Annotations []annotation `json:"annotations"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr))
}

// run handles one request. Diagnostics go to stderr; stdout carries only the
// JSON response.
func run(stdin io.Reader, stdout, stderr io.Writer) int {
	var req request
	if err := json.NewDecoder(stdin).Decode(&req); err != nil {
		// Without a request ID no reply could match, so report and exit.
		_, _ = fmt.Fprintln(stderr, "example: cannot read request:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(handle(req)); err != nil {
		_, _ = fmt.Fprintln(stderr, "example: cannot write response:", err)
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
	case "message.metadata", "message.received":
		// message.received carries the same payload; it arrives only if the
		// manifest declares the event and the user switches it on.
		var meta metadata
		if err := json.Unmarshal(req.Data, &meta); err != nil {
			return fail(resp, "bad_metadata", "cannot read message metadata")
		}
		resp.OK, resp.Data = true, classify(meta)
	default:
		return fail(resp, "unsupported_method", req.Method)
	}
	return resp
}

// classify is deterministic: the same metadata always gives the same
// answer, so rerunning it (Reclassify) is safe.
func classify(meta metadata) result {
	// An empty list is a real answer: it clears this plugin's earlier
	// annotations on the message.
	res := result{Annotations: []annotation{}}
	if strings.Contains(strings.ToLower(meta.Subject), "invoice") {
		res.Annotations = append(res.Annotations, annotation{Key: "category", Value: "billing", Confidence: 0.9})
	}
	return res
}

func fail(resp response, code, message string) response {
	resp.OK = false
	resp.Error = &errorBody{Code: code, Message: message}
	return resp
}
