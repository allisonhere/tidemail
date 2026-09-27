// Package cli contains headless developer commands. It imports no UI package
// and never opens the user's configuration or mail cache: plugin tooling must
// work on a clean machine. Report tests use a throwaway fixture database in
// the temporary directory.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/pluginquery"
)

const maxTestTimeout = time.Minute

// Run handles the arguments after the top-level "plugin" command.
func Run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printPluginHelp(out)
		return 0
	}
	switch args[0] {
	case "validate":
		return runValidate(args[1:], out, errOut)
	case "test":
		return runTest(args[1:], out, errOut)
	case "scaffold":
		return runScaffold(args[1:], out, errOut)
	default:
		_, _ = fmt.Fprintf(errOut, "unknown plugin command %q\n\n", args[0])
		printPluginHelp(errOut)
		return 2
	}
}

func printPluginHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: tidemail plugin <command> [options]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  validate   Validate a TideMail plugin manifest")
	_, _ = fmt.Fprintln(w, "  test       Test a TideMail plugin protocol")
	_, _ = fmt.Fprintln(w, "  scaffold   Create a starter TideMail plugin")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Run 'tidemail plugin <command> --help' for command examples.")
}

type validationReport struct {
	Valid    bool             `json:"valid"`
	Plugin   validationPlugin `json:"plugin,omitempty"`
	Errors   []string         `json:"errors"`
	Warnings []string         `json:"warnings"`
}

type validationPlugin struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	API          int      `json:"api"`
	Permissions  []string `json:"permissions,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func runValidate(args []string, out, errOut io.Writer) int {
	if hasHelpFlag(args) {
		printValidateHelp(out)
		return 0
	}
	fs := flag.NewFlagSet("tidemail plugin validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return usageError(fs, errOut, err, "tidemail plugin validate [--json] <plugin-dir|plugin.toml>")
	}
	if *jsonOutput && fs.NArg() != 1 {
		return usageError(fs, errOut, errors.New("exactly one plugin path is required"), "tidemail plugin validate [--json] <plugin-dir|plugin.toml>")
	}
	if fs.NArg() != 1 {
		if fs.NArg() == 0 {
			printValidateHelp(errOut)
			return 2
		}
		return usageError(fs, errOut, errors.New("exactly one plugin path is required"), "tidemail plugin validate [--json] <plugin-dir|plugin.toml>")
	}
	path := fs.Arg(0)
	p, err := plugin.LoadPlugin(path)
	report := validationReport{Valid: err == nil, Errors: nil}
	if err == nil {
		report.Plugin = validationPlugin{
			ID: p.Manifest.ID, Name: p.Manifest.Name, API: p.Manifest.API,
			Permissions: p.Manifest.Permissions.Names(), Capabilities: p.Manifest.Capabilities,
		}
	} else {
		report.Errors = []string{sanitizeError(err.Error())}
	}
	if *jsonOutput {
		return writeJSONResult(out, report, !report.Valid)
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "✗ %s\n", sanitizeError(err.Error()))
		return 1
	}
	for _, line := range []string{"✓ manifest", "✓ Plugin API v1", "✓ executable", "✓ permissions", "✓ events", "✓ capabilities", "✓ settings"} {
		_, _ = fmt.Fprintln(out, line)
	}
	if names := p.Manifest.Permissions.Names(); len(names) > 0 {
		_, _ = fmt.Fprintf(out, "\npermissions: %s\n", strings.Join(names, ", "))
	}
	if len(p.Manifest.Capabilities) > 0 {
		_, _ = fmt.Fprintf(out, "capabilities: %s\n", strings.Join(p.Manifest.Capabilities, ", "))
	}
	_, _ = fmt.Fprintln(out, "\nPlugin is valid.")
	return 0
}

func printValidateHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: tidemail plugin validate [--json] <plugin-dir|plugin.toml>")
	_, _ = fmt.Fprintln(w, "Validate a plugin manifest, executable, permissions, events, capabilities, and settings.")
	_, _ = fmt.Fprintln(w, "\nExamples:")
	_, _ = fmt.Fprintln(w, "  tidemail plugin validate ./my-plugin")
	_, _ = fmt.Fprintln(w, "  tidemail plugin validate --json ./my-plugin/plugin.toml")
}

type testOptions struct {
	jsonOutput   bool
	verbose      bool
	metadata     string
	reportData   string
	sets         []string
	secretEnvs   []string
	timeout      time.Duration
	allowNetwork bool
	configTest   bool
}

type testCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Elapsed string `json:"elapsed,omitempty"`
}

type testReport struct {
	Valid  bool        `json:"valid"`
	Checks []testCheck `json:"checks"`
}

func runTest(args []string, out, errOut io.Writer) int {
	if hasHelpFlag(args) {
		printTestHelp(out)
		return 0
	}
	fs := flag.NewFlagSet("tidemail plugin test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts testOptions
	fs.BoolVar(&opts.jsonOutput, "json", false, "print machine-readable JSON")
	fs.BoolVar(&opts.verbose, "verbose", false, "show elapsed time and sanitized diagnostics")
	fs.StringVar(&opts.metadata, "metadata", "", "JSON metadata fixture path")
	fs.StringVar(&opts.reportData, "report-fixture", "", "JSON synthetic mailbox for report.run (default: built-in)")
	fs.Var(stringList(&opts.sets), "set", "override a non-secret setting: key=value")
	fs.Var(stringList(&opts.secretEnvs), "secret-env", "inject a secret from the environment: key=ENV_VAR")
	fs.DurationVar(&opts.timeout, "timeout", plugin.DefaultTimeout, "maximum time per plugin call (1ms-1m)")
	fs.BoolVar(&opts.allowNetwork, "allow-network", false, "allow the plugin to make network requests")
	fs.BoolVar(&opts.configTest, "config-test", false, "run plugin.test after the offline protocol checks")
	if err := fs.Parse(args); err != nil {
		return usageError(fs, errOut, err, "tidemail plugin test [options] <plugin-dir|plugin.toml>")
	}
	if fs.NArg() != 1 {
		printTestHelp(errOut)
		return 2
	}
	if opts.timeout <= 0 || opts.timeout > maxTestTimeout {
		return usageError(fs, errOut, errors.New("timeout must be positive and no greater than 1m"), "tidemail plugin test [options] <plugin-dir|plugin.toml>")
	}
	if opts.configTest && !opts.allowNetwork {
		return usageError(fs, errOut, errors.New("--config-test requires --allow-network"), "tidemail plugin test [options] <plugin-dir|plugin.toml>")
	}
	p, err := plugin.LoadPlugin(fs.Arg(0))
	report := testReport{}
	if err != nil {
		report.Checks = append(report.Checks, testCheck{Name: "manifest", Status: "fail", Error: sanitizeError(err.Error())})
		return finishTest(out, errOut, opts, report)
	}
	report.Checks = append(report.Checks, testCheck{Name: "manifest", Status: "pass"})
	secrets, err := parseSecretEnvs(p.Manifest, opts.secretEnvs)
	if err != nil {
		report.Checks = append(report.Checks, testCheck{Name: "settings", Status: "fail", Error: sanitizeError(err.Error())})
		return finishTest(out, errOut, opts, report)
	}
	settings, err := parseSettingOverrides(p.Manifest, opts.sets)
	if err != nil {
		report.Checks = append(report.Checks, testCheck{Name: "settings", Status: "fail", Error: sanitizeError(err.Error())})
		return finishTest(out, errOut, opts, report)
	}
	if opts.allowNetwork {
		// Network permission is declarative only in API v1. The flag is still
		// required for config-test so a normal protocol test stays offline.
		_ = p.Manifest.Permissions.Network
	}
	ctx := context.Background()
	check := func(name, method string, data json.RawMessage, validate func(plugin.Response) error) bool {
		started := time.Now()
		req, reqErr := plugin.NewRequest(method, data)
		if reqErr == nil {
			req.Settings = settings
		}
		var resp plugin.Response
		if reqErr == nil {
			resp, reqErr = plugin.Invoke(ctx, p, req, opts.timeout, secrets)
		}
		if reqErr == nil && !resp.OK {
			reqErr = resp.Error
		}
		if reqErr == nil && validate != nil {
			reqErr = validate(resp)
		}
		c := testCheck{Name: name, Status: "pass"}
		if opts.verbose {
			c.Elapsed = time.Since(started).Round(time.Millisecond).String()
		}
		if reqErr != nil {
			c.Status = "fail"
			c.Error = sanitizeError(reqErr.Error())
		}
		report.Checks = append(report.Checks, c)
		return reqErr == nil
	}
	if !check("ping", plugin.MethodPing, nil, func(resp plugin.Response) error {
		var ping plugin.PingResult
		if err := json.Unmarshal(resp.Data, &ping); err != nil {
			return fmt.Errorf("ping data: %w", err)
		}
		if ping.Message != "pong" {
			return fmt.Errorf("ping message %q, want %q", ping.Message, "pong")
		}
		return nil
	}) {
		return finishTest(out, errOut, opts, report)
	}
	meta, err := loadMetadata(opts.metadata)
	if err != nil {
		report.Checks = append(report.Checks, testCheck{Name: "metadata fixture", Status: "fail", Error: sanitizeError(err.Error())})
		return finishTest(out, errOut, opts, report)
	}
	if p.Manifest.Permissions.MessageMetadata {
		data, _ := json.Marshal(meta)
		if !check(plugin.MethodMessageMetadata, plugin.MethodMessageMetadata, data, func(resp plugin.Response) error {
			if p.Manifest.Permissions.Annotations {
				if _, err := plugin.ParseAnnotations(resp.Data); err != nil {
					return err
				}
			}
			return nil
		}) {
			return finishTest(out, errOut, opts, report)
		}
		if p.Manifest.WantsEvent(plugin.EventMessageReceived) && !check(plugin.EventMessageReceived, plugin.EventMessageReceived, data, func(resp plugin.Response) error {
			if p.Manifest.Permissions.Annotations {
				_, err := plugin.ParseAnnotations(resp.Data)
				return err
			}
			return nil
		}) {
			return finishTest(out, errOut, opts, report)
		}
	}
	if p.Manifest.HasCapability(plugin.CapabilityReport) {
		started := time.Now()
		detail, err := testReportRun(ctx, p, opts, settings, secrets)
		c := testCheck{Name: plugin.MethodReportRun, Status: "pass", Detail: detail}
		if opts.verbose {
			c.Elapsed = time.Since(started).Round(time.Millisecond).String()
		}
		if err != nil {
			c.Status, c.Error = "fail", sanitizeError(err.Error())
		}
		report.Checks = append(report.Checks, c)
		if err != nil {
			return finishTest(out, errOut, opts, report)
		}
	} else if opts.reportData != "" {
		report.Checks = append(report.Checks, testCheck{Name: plugin.MethodReportRun, Status: "fail", Error: "plugin does not declare capability report.run"})
		return finishTest(out, errOut, opts, report)
	}
	if opts.configTest {
		if !p.Manifest.HasCapability(plugin.CapabilityTest) {
			report.Checks = append(report.Checks, testCheck{Name: plugin.MethodTest, Status: "fail", Error: "plugin does not declare capability plugin.test"})
		} else {
			check(plugin.MethodTest, plugin.MethodTest, nil, nil)
		}
	}
	return finishTest(out, errOut, opts, report)
}

// testReportRun runs a full report against a synthetic mailbox loaded into a
// throwaway database, with TideMail's real query engine.
func testReportRun(ctx context.Context, p plugin.Plugin, opts testOptions, settings map[string]any, secrets map[string]string) (string, error) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := pluginquery.DefaultFixture(now)
	if opts.reportData != "" {
		var err error
		if fixture, err = pluginquery.ReadFixture(opts.reportData); err != nil {
			return "", err
		}
	}
	dir, err := os.MkdirTemp("", "tidemail-plugin-test-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	database, err := db.OpenPath(filepath.Join(dir, "mail.db"))
	if err != nil {
		return "", err
	}
	defer database.Close()
	if err := fixture.Load(database); err != nil {
		return "", fmt.Errorf("report fixture: %w", err)
	}
	rc := plugin.ReportContext{AccountName: fixture.Accounts[0].Name, Now: fixture.Now, Timezone: "UTC"}
	if rc.Now == "" {
		rc.Now = now.Format(time.RFC3339)
	}
	if mbs := fixture.Accounts[0].Mailboxes; len(mbs) > 0 {
		rc.MailboxName = mbs[0].Name
	}
	exec := &pluginquery.Executor{DB: database, Me: conversation.MyAddresses(fixture.Me...), Location: time.UTC}
	res, err := plugin.RunReport(ctx, p, rc, exec, plugin.ReportOptions{Timeout: opts.timeout, Settings: settings, Secrets: secrets})
	if err != nil {
		return "", err
	}
	return countNoun(res.Rounds, "round", "rounds") + ", " + countNoun(res.Queries, "query", "queries"), nil
}

func countNoun(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func printTestHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: tidemail plugin test [options] <plugin-dir|plugin.toml>")
	_, _ = fmt.Fprintln(w, "Run offline ping and metadata protocol checks without opening TideMail.")
	_, _ = fmt.Fprintln(w, "Plugins with report.run also run a full report against a synthetic mailbox.")
	_, _ = fmt.Fprintln(w, "\nOptions: --metadata FILE  --report-fixture FILE  --set key=value  --secret-env key=ENV_VAR")
	_, _ = fmt.Fprintln(w, "         --timeout 8s     --json       --verbose")
	_, _ = fmt.Fprintln(w, "         --config-test requires --allow-network")
	_, _ = fmt.Fprintln(w, "\nExample: tidemail plugin test --set mode=local ./my-plugin")
}

func finishTest(out, errOut io.Writer, opts testOptions, report testReport) int {
	report.Valid = len(report.Checks) > 0
	for _, c := range report.Checks {
		if c.Status != "pass" {
			report.Valid = false
		}
	}
	if opts.jsonOutput {
		return writeJSONResult(out, report, !report.Valid)
	}
	for _, c := range report.Checks {
		if c.Status == "pass" {
			_, _ = fmt.Fprintf(out, "✓ %s", c.Name)
		} else {
			_, _ = fmt.Fprintf(out, "✗ %s: %s", c.Name, c.Error)
		}
		if c.Detail != "" {
			_, _ = fmt.Fprintf(out, " (%s)", c.Detail)
		}
		if opts.verbose && c.Elapsed != "" {
			_, _ = fmt.Fprintf(out, " (%s)", c.Elapsed)
		}
		_, _ = fmt.Fprintln(out)
	}
	if report.Valid {
		_, _ = fmt.Fprintln(out, "\nPlugin protocol test passed.")
		return 0
	}
	_, _ = fmt.Fprintln(errOut, "Plugin protocol test failed.")
	return 1
}

func runScaffold(args []string, out, errOut io.Writer) int {
	if hasHelpFlag(args) {
		printScaffoldHelp(out)
		return 0
	}
	fs := flag.NewFlagSet("tidemail plugin scaffold", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	idFlag := fs.String("id", "", "plugin ID (defaults to the name argument)")
	nameFlag := fs.String("name", "", "display name")
	if err := fs.Parse(args); err != nil {
		return usageError(fs, errOut, err, "tidemail plugin scaffold [--id ID] [--name NAME] <name>")
	}
	if fs.NArg() > 1 || (fs.NArg() == 0 && *idFlag == "") {
		printScaffoldHelp(errOut)
		return 2
	}
	id := ""
	if fs.NArg() == 1 {
		id = fs.Arg(0)
	}
	if *idFlag != "" {
		id = *idFlag
	}
	if err := plugin.ValidateID(id); err != nil {
		_, _ = fmt.Fprintln(errOut, sanitizeError(err.Error()))
		return 2
	}
	dir := id
	if _, err := os.Stat(dir); err == nil {
		_, _ = fmt.Fprintf(errOut, "error: target %q already exists\n", dir)
		return 1
	} else if !errors.Is(err, os.ErrNotExist) {
		_, _ = fmt.Fprintln(errOut, "error:", sanitizeError(err.Error()))
		return 1
	}
	name := *nameFlag
	if name == "" {
		name = titleWords(id)
	}
	files := map[string]string{
		"plugin.toml": scaffoldManifest(id, name),
		"main.go":     scaffoldMain(),
		"go.mod":      fmt.Sprintf("module example.com/%s\n\ngo 1.26.1\n", id),
		"README.md":   scaffoldReadme(id, name),
		".gitignore":  fmt.Sprintf("/%s\n", id),
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		_, _ = fmt.Fprintln(errOut, "error:", sanitizeError(err.Error()))
		return 1
	}
	for filename, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, filename), []byte(contents), 0o644); err != nil {
			_, _ = fmt.Fprintln(errOut, "error writing", filename+":", sanitizeError(err.Error()))
			return 1
		}
	}
	_, _ = fmt.Fprintf(out, "Created plugin %q in %s\n", name, dir)
	_, _ = fmt.Fprintf(out, "Next: cd %s && go build -buildvcs=false -o %s . && tidemail plugin validate . && tidemail plugin test .\n", dir, id)
	return 0
}

func printScaffoldHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: tidemail plugin scaffold [--id ID] [--name NAME] <name>")
	_, _ = fmt.Fprintln(w, "Create a standalone Go starter plugin without overwriting existing files.")
	_, _ = fmt.Fprintln(w, "\nExample: tidemail plugin scaffold invoice-tagger")
}

func loadMetadata(path string) (plugin.MessageMetadata, error) {
	if path == "" {
		return plugin.MessageMetadata{ID: 1, MessageID: "<fixture@example.com>", From: "alice@example.com", To: "me@example.com", Subject: "Invoice review requested", Date: "2026-09-27T10:00:00Z", HasAttachment: true, AccountName: "test", MailboxName: "Inbox"}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return plugin.MessageMetadata{}, fmt.Errorf("read metadata fixture: %w", err)
	}
	var meta plugin.MessageMetadata
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&meta); err != nil {
		return plugin.MessageMetadata{}, fmt.Errorf("metadata fixture: %w", err)
	}
	if meta.ID <= 0 {
		meta.ID = 1
	}
	return meta, nil
}

func parseSettingOverrides(man plugin.Manifest, values []string) (map[string]any, error) {
	result := plugin.ResolveSettings(man.Settings, nil)
	for _, raw := range values {
		key, value, ok := strings.Cut(raw, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("--set expects key=value, got %q", raw)
		}
		spec, ok := man.Setting(key)
		if !ok || spec.Type == plugin.SettingSecret {
			return nil, fmt.Errorf("setting %q is not a declared non-secret setting", key)
		}
		switch spec.Type {
		case plugin.SettingBool:
			v, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("setting %q must be true or false", key)
			}
			result[key] = v
		case plugin.SettingSelect:
			valid := false
			for _, option := range spec.Options {
				valid = valid || option == value
			}
			if !valid {
				return nil, fmt.Errorf("setting %q must be one of: %s", key, strings.Join(spec.Options, ", "))
			}
			result[key] = value
		}
	}
	return result, nil
}

func parseSecretEnvs(man plugin.Manifest, values []string) (map[string]string, error) {
	secrets := map[string]string{}
	for _, raw := range values {
		key, env, ok := strings.Cut(raw, "=")
		if !ok || key == "" || env == "" {
			return nil, fmt.Errorf("--secret-env expects setting=ENV_VAR, got %q", raw)
		}
		spec, ok := man.Setting(key)
		if !ok || spec.Type != plugin.SettingSecret {
			return nil, fmt.Errorf("setting %q is not a declared secret setting", key)
		}
		value, ok := os.LookupEnv(env)
		if !ok {
			return nil, fmt.Errorf("environment variable %s is not set", env)
		}
		secrets[key] = value
	}
	return secrets, nil
}

func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func usageError(fs *flag.FlagSet, w io.Writer, err error, usage string) int {
	_, _ = fmt.Fprintf(w, "error: %s\nusage: %s\n", sanitizeError(err.Error()), usage)
	return 2
}

func writeJSONResult(w io.Writer, value any, failed bool) int {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return 1
	}
	if failed {
		return 1
	}
	return 0
}

func sanitizeError(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func titleWords(id string) string {
	words := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(id))
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func stringList(dst *[]string) *listValue { return &listValue{dst: dst} }

type listValue struct{ dst *[]string }

func (v *listValue) String() string { return strings.Join(*v.dst, ",") }
func (v *listValue) Set(s string) error {
	*v.dst = append(*v.dst, s)
	return nil
}

func scaffoldManifest(id, name string) string {
	return fmt.Sprintf("id = %q\nname = %q\nversion = \"0.1.0\"\napi = 1\ncommand = \"./%s\"\n\n[permissions]\nmessage_metadata = true\nmessage_body = false\nnetwork = false\nannotations = true\n", id, name, id)
}

func scaffoldMain() string {
	return `package main

import (
	"encoding/json"
	"os"
	"strings"
)

type request struct { API int ` + "`json:\"api\"`" + `; Type string ` + "`json:\"type\"`" + `; RequestID string ` + "`json:\"request_id\"`" + `; Method string ` + "`json:\"method\"`" + `; Data json.RawMessage ` + "`json:\"data\"`" + ` }
type response struct { API int ` + "`json:\"api\"`" + `; Type string ` + "`json:\"type\"`" + `; RequestID string ` + "`json:\"request_id\"`" + `; OK bool ` + "`json:\"ok\"`" + `; Data any ` + "`json:\"data,omitempty\"`" + ` }

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil { os.Exit(1) }
	data := map[string]any{}
	switch req.Method {
	case "ping": data["message"] = "pong"
	case "message.metadata":
		var meta struct{ Subject string ` + "`json:\"subject\"`" + ` }
		_ = json.Unmarshal(req.Data, &meta)
		if strings.Contains(strings.ToLower(meta.Subject), "invoice") { data["annotations"] = []map[string]string{{"key":"category", "value":"billing"}} } else { data["annotations"] = []any{} }
	default: data["annotations"] = []any{}
	}
	_ = json.NewEncoder(os.Stdout).Encode(response{API:1, Type:"response", RequestID:req.RequestID, OK:true, Data:data})
}
`
}

func scaffoldReadme(id, name string) string {
	return fmt.Sprintf(`# %s

Build and test this standalone TideMail Plugin API v1 plugin:

    go build -buildvcs=false -o %s .
    tidemail plugin validate .
    tidemail plugin test .

Install it under XDG_CONFIG_HOME/tidemail/plugins/%s/, or
~/.config/tidemail/plugins/%s/ when XDG_CONFIG_HOME is unset. Other
languages are supported; the plugin only needs to implement the JSON
stdin/stdout protocol. See TideMail's Plugin API documentation.
`, name, id, id, id)
}
