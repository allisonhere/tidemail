package smtp

import (
	"bytes"
	"github.com/allisonhere/tidemail/internal/config"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestMultipartBytesStableAndNestedBodyStartsWithBoundary(t *testing.T) {
	for _, attachments := range []bool{false, true} {
		msg := OutgoingMessage{MessageID: "<stable@example.com>", Date: time.Unix(1700000000, 0), To: []string{"to@example.com"}, Body: "plain", HTMLBody: "<p>html</p>"}
		if attachments {
			msg.Attachments = []Attachment{{Name: "a.txt", Data: []byte("file")}}
		}
		cfg := config.AccountConfig{User: "from@example.com"}
		raw := BuildRaw(cfg, msg)
		if !bytes.Equal(raw, BuildRaw(cfg, msg)) {
			t.Fatal("rebuilding changed MIME bytes")
		}
		parsed, err := mail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		_, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		outer := multipart.NewReader(parsed.Body, params["boundary"])
		if attachments {
			part, err := outer.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			_, p, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(part)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(body, []byte("--"+p["boundary"]+"\r\n")) {
				t.Fatalf("redundant alternative preamble: %q", body)
			}
			if p["boundary"] == params["boundary"] {
				t.Fatal("nested boundaries collide")
			}
			outer = multipart.NewReader(bytes.NewReader(body), p["boundary"])
		}
		for _, want := range []string{"text/plain", "text/html"} {
			part, err := outer.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(part.Header.Get("Content-Type"), want) {
				t.Fatal(part.Header)
			}
		}
		if _, err := outer.NextPart(); err != io.EOF {
			t.Fatalf("unexpected alternative part: %v", err)
		}
	}
}
