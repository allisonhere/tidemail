package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSkillCommands(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if code := RunSkill(nil, &out, &diagnostic); code != 0 || !strings.HasPrefix(out.String(), "---\nname: tidemail\n") || diagnostic.Len() != 0 {
		t.Fatalf("skill: code=%d, diagnostic=%q", code, diagnostic.String())
	}
	out.Reset()
	if code := RunSkill([]string{"--help"}, &out, &diagnostic); code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("skill help: %d %q", code, out.String())
	}
	if code := RunSkill([]string{"unexpected"}, &out, &diagnostic); code != 2 {
		t.Fatalf("unexpected args: %d", code)
	}
}

func TestSkillDestination(t *testing.T) {
	home := t.TempDir()
	for agent, subpath := range map[string]string{
		"codex":  ".agents/skills/tidemail/SKILL.md",
		"pi":     ".agents/skills/tidemail/SKILL.md",
		"claude": ".claude/skills/tidemail/SKILL.md",
		"hermes": ".hermes/skills/tidemail/SKILL.md",
	} {
		got, err := skillDestination(agent, home, "")
		if err != nil || got != filepath.Join(home, filepath.FromSlash(subpath)) {
			t.Fatalf("%s: %q %v", agent, got, err)
		}
	}
	profile := t.TempDir()
	got, err := skillDestination("hermes", home, profile)
	if err != nil || got != filepath.Join(profile, "skills", "tidemail", "SKILL.md") {
		t.Fatalf("profile: %q %v", got, err)
	}
	if _, err := skillDestination("unknown", home, ""); err == nil {
		t.Fatal("unknown agent accepted")
	}
}

func TestSkillInstallerDryRunAndOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out, diagnostic bytes.Buffer
	path := filepath.Join(home, ".agents", "skills", "tidemail", "SKILL.md")
	if code := RunInstallSkill([]string{"codex", "--dry-run"}, &out, &diagnostic); code != 0 || !strings.Contains(out.String(), path) {
		t.Fatalf("dry run: %d %q %q", code, out.String(), diagnostic.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run wrote directories: %v", err)
	}
	if code := RunInstallSkill([]string{"codex"}, &out, &diagnostic); code != 0 {
		t.Fatalf("install: %d %q", code, diagnostic.String())
	}
	if err := os.WriteFile(path, []byte("user guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := RunInstallSkill([]string{"codex"}, &out, &diagnostic); code != 1 {
		t.Fatalf("overwrite without consent: %d", code)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "user guidance" {
		t.Fatal("existing skill changed")
	}
	if code := RunInstallSkill([]string{"codex", "--force"}, &out, &diagnostic); code != 0 {
		t.Fatalf("force: %d %q", code, diagnostic.String())
	}
	got, _ = os.ReadFile(path)
	if string(got) != mailSkill {
		t.Fatal("installed guidance differs")
	}
}

func TestSkillInstallerConcurrentNoClobber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tidemail", "SKILL.md")
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- installSkillFile(path, []byte(mailSkill), false)
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful installs, want 1", successes)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != mailSkill {
		t.Fatalf("partial publication: %v", err)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary files remain: %v %v", files, err)
	}
}
