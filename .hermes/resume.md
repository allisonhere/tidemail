# Tidemail Test Suite — Resume Point

## Git State
- Branch: `main`, up to date with `origin/main`
- 5 files modified: `internal/ai/{claude,gemini,openai}.go` (extracted API URL vars), `internal/smtp/smtp.go` (injectable dialers), `internal/smtp/smtp_test.go` (new tests)
- 2 files created (unstaged): `internal/ai/summarize_test.go`, `internal/imap/client_test.go`
- 306 insertions, 149 deletions total

## Completed

### AI Provider Summarize Tests (`internal/ai/summarize_test.go`)
- 14 tests: 4 providers × 3 scenarios (OK, API error, empty response) + New_NoProvider + TruncateContent + Ollama connection error
- Coverage: 65.4%
- Commands: `go test -count=1 -v ./internal/ai/` → 22/22 pass

### SMTP Send Tests (`internal/smtp/smtp_test.go`) — 10 tests all passing
- `TestSendMail` — full SMTP transaction via net.Pipe (220 → EHLO → MAIL FROM → RCPT TO → DATA → content)
- `TestSendSTARTTLS_NoTLS` — pipe overrides smtpDial & smtpNewClient, tests non-TLS auth path using localhost (PlainAuth exemption)
- `TestSendSTARTTLS_NoRecipients` — validation error
- `TestSend_STARTTLS_DialError` — smtpDial returns error
- `TestSend_TLS_DialError` — tlsDial returns error
- `TestSend_FromFallback` — From empty, User used as fallback (test via dial error for coverage)
- `TestSendMail_MAILFROMError` — 550 response
- `TestSendMail_RCPTError` — 550 response
- `TestSendMail_DATAError` — 554 response
- `TestBuildRaw_MultilineBody` — multi-line body formatting
- **Critical Go 1.26 detail**: `smtp.NewClient` reads 220 greeting AND `client.Mail()` calls `hello()` (sends EHLO) before MAIL FROM. All pipe tests must handle this sequence.
- Coverage: 72.5%
- Commands: `go test -count=1 -v ./internal/smtp/` → 10/10 pass

## In Progress — `internal/imap/client_test.go`

### File exists at `/home/allie/Projects/tidemail/internal/imap/client_test.go` but DOES NOT COMPILE

### Known compile errors (from `go test ./internal/imap/`):
1. Line 76: `imapclient.New(conn, nil)` returns `*Client` (1 value), not `(*Client, error)` — fix: remove `err`
2. Line 92: `strings` undefined — needs `import "strings"` in file
3. Line 277: `msg.Seen` — `db.Message` has field `Read bool` not `Seen` → use `msg.Read`
4. Unused imports: `"crypto/tls"` (line 5), `"github.com/allisonhere/tide/internal/db"` (line 12)

### Test structure:
- `startTestServer(t)` helper: starts imapmemserver on `127.0.0.1:0`, creates user "testuser"/"testpass" with INBOX/Sent/Archive mailboxes
- `appendTestMessage(t, port, mailbox, seen)` helper: dials server, creates raw imapclient, APPENDs a message with optional \Seen flag
- 11 test functions planned: Connect_OK, Connect_BadAuth, Connect_BadHost, ListMailboxes, FetchMessages_Empty, FetchMessages_WithMessages, MarkSeen, MoveMessage, DeleteMessage, Close_NotConnected, DoubleClose, NotConnectedError

### Dependencies used in IMAP tests:
```
"github.com/emersion/go-imap/v2"
imapclient "github.com/emersion/go-imap/v2/imapclient"
"github.com/emersion/go-imap/v2/imapserver"
"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
```
`imapserver.Options.InsecureAuth: true` required for non-TLS logins.

## Remaining Work (priority order)

### 1. Fix IMAP tests (compile)
Fix the 4 compile errors above, run `go test -count=1 -v ./internal/imap/` to verify all pass.

### 2. Feature: Attachments
- Compose UI: `internal/ui/compose.go` already has attachment logic stubs — need to check current state
- Sending: need MIME multipart handling in `internal/smtp/` when attachments present
- MIME: use `mime/multipart` or `github.com/emersion/go-message/mail`

### 3. Feature: Keychain/Secret-Service
- Integrate with `libsecret` (D-Bus) or `pass` for password storage
- File to check: `internal/config/` for how passwords are currently stored
- `go-keyring` or `github.com/zalando/go-keyring` for cross-platform keyring

### 4. Final Review
- `go vet ./...`
- Full test suite: `go test -count=1 ./...`
- `go build ./...`
- Audit dependencies in `go.mod` (gofeed still present from RSS reader days)
- Update `STATUS.md` with new test coverage
- Remove stale RSS docs if any
- `git diff --stat` to review, commit

## Project Structure
```
/home/allie/Projects/tidemail/
├── internal/
│   ├── ai/        → openai.go, claude.go, gemini.go, ollama.go, summarize.go
│   ├── smtp/      → smtp.go (send, sendSTARTTLS, sendTLS, buildRaw)
│   ├── imap/      → client.go (Connect, ListMailboxes, FetchMessages, etc.), parse.go
│   ├── config/    → AccountConfig, AIConfig
│   ├── db/        → Message struct (Read, UID, Flags, Subject, etc.)
│   ├── ui/        → compose.go, model.go, account_manager.go, validate_cmd.go
│   └── ...
├── go.mod         → Go 1.26.1 module
└── STATUS.md      → needs update with test results
```
