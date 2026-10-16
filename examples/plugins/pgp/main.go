// Command tidemail-pgp is TideMail's OpenPGP plugin. It decrypts and verifies
// received mail (message.decode) and signs and encrypts outgoing mail
// (message.prepare) by running the user's own gpg against the user's own
// keyring. TideMail does all the MIME work and hands over only the bytes to
// protect or check, so this plugin is a thin, auditable wrapper: no key ever
// reaches TideMail, and no key material is kept here.
//
// It uses only the standard library. See README.md and
// docs/plugins/protocol.md.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const apiVersion = 1

type request struct {
	API       int             `json:"api"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Method    string          `json:"method"`
	Settings  map[string]any  `json:"settings,omitempty"`
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

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, execGPG{bin: "gpg"}))
}

func run(stdin io.Reader, stdout, stderr io.Writer, g gpg) int {
	var req request
	if err := json.NewDecoder(stdin).Decode(&req); err != nil {
		_, _ = fmt.Fprintln(stderr, "pgp: cannot read request:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(handle(req, g)); err != nil {
		_, _ = fmt.Fprintln(stderr, "pgp: cannot write response:", err)
		return 1
	}
	return 0
}

func handle(req request, g gpg) response {
	resp := response{API: apiVersion, Type: "response", RequestID: req.RequestID}
	if req.API != apiVersion {
		return fail(resp, "unsupported_api", fmt.Sprintf("this plugin speaks API %d", apiVersion))
	}
	trust, _ := req.Settings["trust"].(string)
	opts := options{AlwaysTrust: trust == "always"}
	switch req.Method {
	case "ping":
		resp.OK, resp.Data = true, map[string]string{"message": "pong"}
	case "message.decode":
		var in decodeIn
		if err := json.Unmarshal(req.Data, &in); err != nil {
			return fail(resp, "bad_request", "cannot read the decode request")
		}
		out, err := decode(g, in)
		return finish(resp, out, err)
	case "message.prepare":
		var in prepareIn
		if err := json.Unmarshal(req.Data, &in); err != nil {
			return fail(resp, "bad_request", "cannot read the prepare request")
		}
		out, err := prepare(g, in, opts)
		return finish(resp, out, err)
	default:
		return fail(resp, "unsupported_method", req.Method)
	}
	return resp
}

func finish(resp response, data any, err error) response {
	if err != nil {
		if ce, ok := err.(*codedError); ok {
			return fail(resp, ce.code, ce.msg)
		}
		return fail(resp, "gpg_failed", err.Error())
	}
	resp.OK, resp.Data = true, data
	return resp
}

func fail(resp response, code, message string) response {
	resp.OK = false
	resp.Error = &errorBody{Code: code, Message: message}
	return resp
}
