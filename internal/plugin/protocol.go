package plugin

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Envelope types.
const (
	TypeRequest  = "request"
	TypeResponse = "response"
)

// Methods in API v1.
const (
	// MethodPing checks that a plugin runs. A plugin answers it with
	// {"message": "pong"}.
	MethodPing = "ping"
	// MethodMessageMetadata sends one message's metadata (a MessageMetadata).
	// Only plugins with the message_metadata permission receive it. The
	// response data is informational: TideMail displays it and never acts on
	// it.
	MethodMessageMetadata = "message.metadata"
)

// MessageMetadata is the data of a message.metadata request. It holds only
// header-level facts about one message: never its body, raw headers,
// attachments, AI summary, or anything about the account beyond its display
// name.
type MessageMetadata struct {
	// ID is TideMail's identifier for the message, for correlating results.
	ID            int64    `json:"id"`
	MessageID     string   `json:"message_id,omitempty"`
	From          string   `json:"from,omitempty"`
	To            string   `json:"to,omitempty"`
	CC            string   `json:"cc,omitempty"`
	ReplyTo       string   `json:"reply_to,omitempty"`
	Subject       string   `json:"subject,omitempty"`
	Date          string   `json:"date,omitempty"` // RFC 3339
	Read          bool     `json:"read"`
	Starred       bool     `json:"starred"`
	HasAttachment bool     `json:"has_attachment"`
	Flags         []string `json:"flags,omitempty"`
	AccountName   string   `json:"account_name,omitempty"`
	MailboxName   string   `json:"mailbox_name,omitempty"`
}

// Request is the envelope TideMail writes to a plugin's stdin.
type Request struct {
	API       int    `json:"api"`
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Method    string `json:"method"`
	// Settings carries the plugin's own resolved non-secret settings. Secrets
	// are never in the request: they reach the plugin only through its process
	// environment (see SecretEnvVar).
	Settings map[string]any  `json:"settings,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// Response is the envelope a plugin writes to stdout. Exactly one of Data
// (when OK) or Error (when not OK) is meaningful.
type Response struct {
	API       int             `json:"api"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	OK        bool            `json:"ok"`
	Data      json.RawMessage `json:"data,omitempty"`
	Error     *Error          `json:"error,omitempty"`
}

// Error is the error a plugin reports in a response with ok=false.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// PingResult is the data of a successful ping response.
type PingResult struct {
	Message string `json:"message"`
}

// NewRequest builds a request envelope with a random request ID.
func NewRequest(method string, data json.RawMessage) (Request, error) {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Request{}, fmt.Errorf("generate request id: %w", err)
	}
	return Request{
		API:       APIVersion,
		Type:      TypeRequest,
		RequestID: hex.EncodeToString(id[:]),
		Method:    method,
		Data:      data,
	}, nil
}

func (r Request) validate() error {
	switch {
	case r.API != APIVersion:
		return fmt.Errorf("request api %d, want %d", r.API, APIVersion)
	case r.Type != TypeRequest:
		return fmt.Errorf("request type %q, want %q", r.Type, TypeRequest)
	case r.RequestID == "":
		return errors.New("request id is required")
	case r.Method == "":
		return errors.New("request method is required")
	}
	return nil
}

// decodeResponse parses plugin stdout as exactly one response envelope that
// answers req. Plugin output is untrusted, so every mismatch is an error.
func decodeResponse(out []byte, req Request) (Response, error) {
	dec := json.NewDecoder(bytes.NewReader(out))
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		if errors.Is(err, io.EOF) {
			return Response{}, errors.New("plugin wrote no response")
		}
		return Response{}, fmt.Errorf("malformed response: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Response{}, errors.New("malformed response: unexpected data after the response")
	}
	switch {
	case resp.API != APIVersion:
		return Response{}, fmt.Errorf("response api %d, want %d", resp.API, APIVersion)
	case resp.Type != TypeResponse:
		return Response{}, fmt.Errorf("response type %q, want %q", resp.Type, TypeResponse)
	case resp.RequestID != req.RequestID:
		return Response{}, fmt.Errorf("response request_id %q does not match request %q", resp.RequestID, req.RequestID)
	case resp.OK && resp.Error != nil:
		return Response{}, errors.New("response has ok=true and an error")
	case !resp.OK && resp.Error == nil:
		return Response{}, errors.New("response has ok=false and no error")
	}
	return resp, nil
}
