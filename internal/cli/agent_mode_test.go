package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentEnvironmentCannotBeOverridden(t *testing.T) {
	t.Setenv("TIDEMAIL_AGENT", "1")
	t.Run("read", func(t *testing.T) {
		id := mailReadFixture(t)
		for _, extra := range [][]string{nil, {"--agent=false"}} {
			var out, diag bytes.Buffer
			args := append([]string{"read", fmt.Sprint(id), "--json"}, extra...)
			if code := RunMailRead("messages", args, &out, &diag); code != 3 || !strings.Contains(out.String(), "permission_denied") || strings.Contains(out.String(), "private") {
				t.Fatalf("%v: %d %s %s", args, code, out.String(), diag.String())
			}
		}
		var out, diag bytes.Buffer
		if code := RunMailRead("messages", []string{"list", "--json"}, &out, &diag); code != 0 {
			t.Fatalf("allowed metadata denied: %d %s", code, out.String())
		}
	})
	t.Run("mutations", func(t *testing.T) {
		mailFixture(t)
		// No granted capabilities: all mutation families must honor forced mode.
		path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tidemail", "config.toml")
		config, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append([]byte("agent_permissions = []\n"), config...), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"send", "--to", "you@example.com", "--subject", "Hello", "--body", "body", "--yes", "--json"},
			{"draft", "create", "--to", "you@example.com", "--body", "body", "--agent=false", "--json"},
			{"outbox", "process", "--yes", "--agent=false", "--json"},
			{"outbox", "retry", "1", "--yes", "--json"},
			{"outbox", "cancel", "1", "--agent=false", "--json"},
			{"accounts", "list", "--agent=false", "--json"},
		} {
			var out, diag bytes.Buffer
			if code := RunHeadless(args, strings.NewReader(""), &out, &diag); code != 3 || !strings.Contains(out.String(), "permission_denied") {
				t.Fatalf("%v: %d %s %s", args, code, out.String(), diag.String())
			}
		}
	})
	t.Run("approval", func(t *testing.T) {
		mailFixture(t)
		path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tidemail", "config.toml")
		config, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append([]byte("agent_permissions = ['mail.send']\n"), config...), 0600); err != nil {
			t.Fatal(err)
		}
		var out, diag bytes.Buffer
		if code := RunHeadless([]string{"outbox", "process", "--agent=false", "--json"}, strings.NewReader(""), &out, &diag); code != 3 || !strings.Contains(out.String(), "approval_required") {
			t.Fatalf("%d %s %s", code, out.String(), diag.String())
		}
	})
}

func TestAgentEnvironmentOptIn(t *testing.T) {
	for _, value := range []string{"", "0", "true", "1"} {
		t.Setenv(agentEnv, value)
		if got := agentMode(false); got != (value == "1") {
			t.Fatalf("%s=%q: %v", agentEnv, value, got)
		}
		if !agentMode(true) {
			t.Fatal("explicit agent mode disabled")
		}
	}
	if !strings.Contains(mailSkill, "TIDEMAIL_AGENT=1") {
		t.Fatal("skill does not mention the environment variable")
	}
}
