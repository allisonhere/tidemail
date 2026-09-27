# Build your first TideMail plugin

This tutorial builds a plugin that tags any message whose subject mentions
"invoice" with `category=billing`. It takes a few minutes and needs no
TideMail source code. The finished, tested version is
[`examples/plugins/example/`](../../examples/plugins/example/).

The examples use Go, but nothing here is Go-specific; a
[Python version](#the-same-plugin-in-python) is at the end.

## Faster start: scaffold a plugin

If the TideMail binary is available, the recommended contributor workflow is:

```sh
tidemail plugin scaffold invoice-tagger
cd invoice-tagger
go build -buildvcs=false -o invoice-tagger .
tidemail plugin validate .
tidemail plugin test .
```

The generated starter tags subjects containing “invoice” as
`category=billing`. These commands are headless: they do not need a TideMail
configuration, database, mail account, or running TUI. See
[developer-tools.md](developer-tools.md) for fixtures, settings, and CI.

## 1. Create a directory and an executable

A plugin is a directory containing a manifest and an executable:

```sh
mkdir -p ~/src/tidemail-plugin-example && cd ~/src/tidemail-plugin-example
go mod init example.com/tidemail-plugin-example
```

## 2. Create the manifest

Save this as `plugin.toml`:

```toml
id = "example"
name = "Example Plugin"
version = "0.1.0"
api = 1
command = "tidemail-plugin-example"

[permissions]
message_metadata = true
annotations = true
```

- `id` is the stable identifier (lowercase letters, digits, `_`, `-`).
- `api = 1` is the protocol version this plugin speaks.
- `command` is the executable, relative to the plugin directory. It is never
  looked up on `$PATH`.
- `message_metadata = true` lets the plugin receive message metadata;
  `annotations = true` lets TideMail store what it returns.

Every field is described in [manifest.md](manifest.md).

## 3. Read one request from stdin

TideMail starts the executable, writes one JSON object and a newline to stdin,
and closes stdin. For example:

```json
{"api": 1, "type": "request", "request_id": "5c1e0b9a6f2d7e3a4b8c9d01", "method": "ping"}
```

Decode exactly one value:

```go
type request struct {
	API       int             `json:"api"`
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Method    string          `json:"method"`
	Settings  map[string]any  `json:"settings,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

var req request
if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
	fmt.Fprintln(os.Stderr, "example: cannot read request:", err)
	os.Exit(1)
}
```

## 4. Respond with JSON, and only JSON, on stdout

Every response echoes `api`, sets `type` to `"response"`, copies the
request's `request_id`, and says whether it succeeded:

```go
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
```

**stdout is for the one JSON response only.** Any banner, log line, or
progress output there makes the response invalid. Write diagnostics to stderr;
TideMail shows a short, sanitized excerpt of stderr when a call fails.

## 5. Implement `ping`

```go
resp := response{API: 1, Type: "response", RequestID: req.RequestID}
switch req.Method {
case "ping":
	resp.OK, resp.Data = true, map[string]string{"message": "pong"}
default:
	resp.Error = &errorBody{Code: "unsupported_method", Message: req.Method}
}
json.NewEncoder(os.Stdout).Encode(resp)
```

An error response has `"ok": false` and an `error` object. Exit with status 0
either way; a non-zero exit is reported as a crash.

## 6. Implement `message.metadata`

When the user runs your plugin on a message, `data` holds that message's
metadata (the full field list is in [protocol.md](protocol.md#messagemetadata)):

```json
{"id": 1234, "from": "Billing <billing@example.com>", "subject": "Invoice 2026-09", "date": "2026-09-26T10:00:00Z", "read": false, "starred": false, "has_attachment": true, "mailbox_name": "INBOX"}
```

Decode the fields you need; ignore the rest:

```go
type metadata struct {
	ID      int64  `json:"id"`
	Subject string `json:"subject"`
}
```

## 7. Return an annotation

Annotations go in `data.annotations`:

```go
type annotation struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"`
}

case "message.metadata":
	var meta metadata
	if err := json.Unmarshal(req.Data, &meta); err != nil {
		resp.Error = &errorBody{Code: "bad_metadata", Message: "cannot read metadata"}
		break
	}
	anns := []annotation{}
	if strings.Contains(strings.ToLower(meta.Subject), "invoice") {
		anns = append(anns, annotation{Key: "category", Value: "billing", Confidence: 0.9})
	}
	resp.OK, resp.Data = true, map[string]any{"annotations": anns}
```

Two rules matter here:

- Your answer **replaces** everything this plugin previously stored on the
  message. Returning `[]` clears it. That is how a message that is no longer
  an invoice loses its tag.
- Users can rerun your plugin on old mail at any time (**Reclassify**), so the
  same input should give the same answer. See
  [annotations.md › Reclassification](annotations.md#reclassification-and-idempotency).

Limits and conventional keys are in [annotations.md](annotations.md).

Try it without TideMail:

```sh
go build -o tidemail-plugin-example .
echo '{"api":1,"type":"request","request_id":"t1","method":"message.metadata","data":{"id":1,"subject":"Your invoice"}}' | ./tidemail-plugin-example
```

```json
{"api":1,"type":"response","request_id":"t1","ok":true,"data":{"annotations":[{"key":"category","value":"billing","confidence":0.9}]}}
```

## 8. Install it

```sh
mkdir -p ~/.config/tidemail/plugins/example
cp plugin.toml tidemail-plugin-example ~/.config/tidemail/plugins/example/
chmod +x ~/.config/tidemail/plugins/example/tidemail-plugin-example
```

From a TideMail checkout, the example builds straight into place:

```sh
mkdir -p ~/.config/tidemail/plugins/example
cp examples/plugins/example/plugin.toml ~/.config/tidemail/plugins/example/
go build -o ~/.config/tidemail/plugins/example/tidemail-plugin-example ./examples/plugins/example
```

## 9. Restart TideMail

Plugins are discovered at startup. Open the command palette (`:`) and choose
**Plugins (experimental)**: your plugin should be listed with its permissions.
If the manifest or executable is wrong, the list says why instead.

## 10. Run it by hand

Select a message whose subject contains "invoice", open the palette, and
choose **Run plugin on current message…**, then your plugin. The result window
shows the response, and the row gains a `BILLING` tag. **Message annotations**
lists what was stored.

To run it on a whole folder, choose **Run plugin on all N messages in …**
instead. TideMail asks once, shows progress, and ends with how many messages
succeeded, failed, changed, and stayed the same.

## 11. Optional: process new mail automatically

Add the event to the manifest:

```toml
events = ["message.received"]
```

`message.received` carries exactly the same metadata, and your response is
handled exactly like `message.metadata`, so the same code serves both
methods. After a restart, users turn it on per plugin: select it in
**Plugins (experimental)** and press `a`. Nothing is sent automatically until
they do. See [events.md](events.md).

## The same plugin in Python

```python
#!/usr/bin/env python3
import json
import sys

req = json.loads(sys.stdin.readline())
resp = {"api": 1, "type": "response", "request_id": req.get("request_id", "")}
method = req.get("method")
if method == "ping":
    resp.update(ok=True, data={"message": "pong"})
elif method in ("message.metadata", "message.received"):
    subject = (req.get("data") or {}).get("subject", "")
    anns = [{"key": "category", "value": "billing"}] if "invoice" in subject.lower() else []
    resp.update(ok=True, data={"annotations": anns})
else:
    resp.update(ok=False, error={"code": "unsupported_method", "message": str(method)})
sys.stdout.write(json.dumps(resp) + "\n")
```

Use `command = "plugin.py"`, make the file executable, and keep the `#!` line:
TideMail runs the file directly, without a shell.
