package cli

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func manSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../man/tidemail.1")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The page must mention every command the CLI accepts, every send flag, and the
// exit codes, so it cannot quietly go stale.
func TestManPageCoversCommandsFlagsAndExitCodes(t *testing.T) {
	page := manSource(t)
	if !strings.HasPrefix(page, ".TH TIDEMAIL 1") {
		t.Errorf("page should start with a .TH line, got %q", strings.SplitN(page, "\n", 2)[0])
	}
	for _, cmd := range []string{"cli", "send", "draft", "messages", "accounts", "outbox", "templates", "man", "skill", "install-skill"} {
		if !IsMailCommand(cmd) {
			t.Errorf("%q is no longer a command; update this test and the page", cmd)
		}
		if !strings.Contains(page, cmd) {
			t.Errorf("man page does not mention the %q command", cmd)
		}
	}
	for _, sub := range []string{"validate", "test", "scaffold", "list", "search", "read", "status", "process", "retry", "cancel"} {
		if !strings.Contains(page, sub) {
			t.Errorf("man page does not mention %q", sub)
		}
	}

	var out, errs bytes.Buffer
	RunHeadless([]string{"send", "--help"}, strings.NewReader(""), &out, &errs)
	help := out.String() + errs.String()
	flags := regexp.MustCompile(`(?m)^\s+-(-?[a-z][a-z-]*)`).FindAllStringSubmatch(help, -1)
	if len(flags) < 10 {
		t.Fatalf("could not read the send flags from --help:\n%s", help)
	}
	escaped := strings.ReplaceAll(page, `\-`, "-")
	for _, f := range flags {
		name := strings.TrimPrefix(f[1], "-")
		if !strings.Contains(escaped, "--"+name) {
			t.Errorf("send flag --%s is not documented in the man page", name)
		}
	}
	for _, code := range []string{".B 0", ".B 1", ".B 2", ".B 3"} {
		if !strings.Contains(page, code+"\n") {
			t.Errorf("exit status %q missing", code)
		}
	}
}

// A line starting with "." or "'" is a roff request, so prose must not begin
// that way by accident.
func TestManPageHasNoStrayRoffRequests(t *testing.T) {
	known := regexp.MustCompile(`^\.(TH|SH|SS|B|BI|BR|I|IR|IB|RI|RB|TP|TQ|PP|P|RS|RE|nf|fi|br|UR|UE|IP|sp)\b`)
	for i, line := range strings.Split(manSource(t), "\n") {
		if strings.HasPrefix(line, ".") && !known.MatchString(line) {
			t.Errorf("line %d: unexpected roff request %q", i+1, line)
		}
		if strings.HasPrefix(line, "'") {
			t.Errorf("line %d starts with an apostrophe (a roff control character): %q", i+1, line)
		}
	}
}

func TestManCommandPrintsThePage(t *testing.T) {
	orig := ManPage
	defer func() { ManPage = orig }()
	ManPage = manSource(t)
	var out, errs bytes.Buffer
	if code := RunHeadless([]string{"man"}, strings.NewReader(""), &out, &errs); code != 0 || out.String() != ManPage {
		t.Errorf("man = %d, %d bytes (want %d)", code, out.Len(), len(ManPage))
	}
	ManPage = ""
	if code := RunHeadless([]string{"man"}, strings.NewReader(""), &out, &errs); code != 1 {
		t.Errorf("a build without a page should fail, got %d", code)
	}
}

func TestManPageHasNoGroffWarnings(t *testing.T) {
	groff, err := exec.LookPath("groff")
	if err != nil {
		t.Skip("groff not installed")
	}
	cmd := exec.Command(groff, "-man", "-Tutf8", "-ww", "-z", "../../man/tidemail.1")
	if out, err := cmd.CombinedOutput(); err != nil || len(bytes.TrimSpace(out)) > 0 {
		t.Errorf("groff: %v\n%s", err, out)
	}
}
