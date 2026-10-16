package ui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
)

func TestPluginMetadataCarriesSenderChecksButNotHeaders(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	msg := db.Message{
		ID: 1, MailboxID: 7, From: "a@example.com", Subject: "hi",
		Headers: "Authentication-Results\nmx; spf=pass; dkim=fail; dmarc=pass\nReturn-Path\n<secret-bounce@example.com>\nReceived\nfrom 10.0.0.1 by mx\n",
	}
	meta := m.pluginMessageMetadata(msg)
	want := &plugin.MessageAuth{SPF: "pass", DKIM: "fail", DMARC: "pass"}
	if !reflect.DeepEqual(meta.Auth, want) {
		t.Fatalf("auth = %+v, want %+v", meta.Auth, want)
	}
	// Only the derived values go out: nothing from the raw header text.
	encoded, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"secret-bounce", "10.0.0.1", "Authentication-Results"} {
		if strings.Contains(string(encoded), leak) {
			t.Errorf("metadata leaks %q: %s", leak, encoded)
		}
	}
	if m.pluginMessageMetadata(db.Message{ID: 2, From: "b@example.com"}).Auth != nil {
		t.Error("a message with no sender-check headers should carry no auth")
	}
}
