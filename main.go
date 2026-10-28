package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/cli"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/ui"
)

var version = "dev"

// changelog is shown in Settings → Changelog, so it always matches this build.
//
//go:embed CHANGELOG.md
var changelog string

// manPage is the manual page, printed by "tidemail man" and installed by
// install.sh and the AUR package, so it always matches this build.
//
//go:embed man/tidemail.1
var manPage string

type startupOptions struct {
	previewManualUpdate   bool
	previewUpdateProgress bool
	prototypeForms        bool
	disableGoogleOAuth    bool
}

func parseStartupOptions(args []string) startupOptions {
	var opts startupOptions
	for _, a := range args {
		switch strings.TrimSpace(a) {
		case "--preview-manual-update":
			opts.previewManualUpdate = true
		case "--preview-update-progress":
			opts.previewUpdateProgress = true
		case "--prototype-forms":
			opts.prototypeForms = true
		case "--disable-google-oauth":
			opts.disableGoogleOAuth = true
		}
	}
	return opts
}

func main() {
	// Run the program through a helper that returns an exit code so os.Exit is
	// called exactly once, here, after every deferred cleanup has run. Calling
	// os.Exit deeper in the program would skip those defers — including the
	// terminal default-color reset below — and leave the shell prompt rendered
	// in the theme's colors (an invisible / "broken" prompt after quitting).
	code, restartExec := run()

	// An in-app update restart asks us to re-exec the freshly installed binary.
	// We only reach here after run() has returned, so all of its defers have run:
	// BubbleTea restored the terminal, the DB and IMAP sessions are closed (so the
	// new process can take the DB lock), and the theme colors were reset. Replacing
	// the process now — rather than spawning a second one from inside the live TUI —
	// hands the clean terminal to the new version without two processes racing over it.
	if restartExec != "" {
		argv := append([]string{restartExec}, os.Args[1:]...)
		if err := syscall.Exec(restartExec, argv, os.Environ()); err != nil {
			fmt.Fprintln(os.Stderr, "restart failed:", err)
			os.Exit(1)
		}
	}

	os.Exit(code)
}

func run() (code int, restartExec string) {
	// Developer plugin commands are intentionally headless. Short-circuit before
	// config, database, IMAP, or Bubble Tea initialization so they work on a
	// clean contributor machine.
	if len(os.Args) > 1 && os.Args[1] == "plugin" {
		return cli.Run(os.Args[2:], os.Stdout, os.Stderr), ""
	}
	startupArgs := os.Args[1:]
	cli.ManPage = manPage
	if len(startupArgs) > 0 && cli.IsMailCommand(startupArgs[0]) {
		code := cli.RunHeadless(startupArgs, os.Stdin, os.Stdout, os.Stderr)
		if code != cli.OpenInbox {
			return code, ""
		}
		startupArgs = nil
	}
	opts := parseStartupOptions(startupArgs)
	for _, a := range startupArgs {
		switch strings.TrimSpace(a) {
		case "--version", "-version", "-v":
			fmt.Printf("tidemail %s\n", resolvedVersion())
			return 0, ""
		}
	}
	for _, a := range startupArgs {
		switch a {
		case "--preview-manual-update", "--preview-update-progress", "--prototype-forms", "--disable-google-oauth":
		default:
			fmt.Fprintln(os.Stderr, "unknown command or option; run tidemail --help")
			return 2, ""
		}
	}

	cfg, err := config.Load()
	if err != nil {
		path, pathErr := config.Path()
		if pathErr != nil {
			path = "config.toml"
		}
		fmt.Fprintln(os.Stderr, formatConfigLoadError(path, err, cfg))
		return 1, ""
	}
	applyStartupOverrides(&cfg, opts)
	if warnings, err := config.SecurityWarnings(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not check config permissions:", config.RedactSecrets(err.Error(), cfg))
	} else {
		for _, warning := range warnings {
			fmt.Fprintln(os.Stderr, "warning:", config.RedactSecrets(warning, cfg))
		}
	}

	if setColors, resetColors := ui.TerminalColorSequences(cfg.Theme); setColors != "" {
		fmt.Print(setColors)
		defer fmt.Print(resetColors)
	}
	var quitIndicator *shutdownIndicator
	defer func() {
		if quitIndicator != nil {
			quitIndicator.Stop()
		}
	}()

	var model tea.Model
	if opts.prototypeForms {
		model = ui.NewPrototypeFormsModel(cfg)
	} else {
		database, err := db.Open()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error opening database:", config.RedactSecrets(err.Error(), cfg))
			return 1, ""
		}
		defer database.Close()

		// --preview-manual-update / --preview-update-progress are dev UI entry points.
		ui.SetChangelog(changelog)
		ui.PruneEmailExports()
		m := ui.NewModel(database, cfg, resolvedVersion(), opts.previewManualUpdate || opts.previewUpdateProgress)
		// Experimental plugins; a missing or broken plugin directory is never fatal.
		m.LoadPlugins()
		if opts.previewUpdateProgress {
			m.ApplyUpdateProgressPreview()
		}
		model = m
	}
	if uiModel, ok := model.(ui.Model); ok {
		defer uiModel.CloseSessions()
	}

	// Frames go through SerializedStdout so image uploads (written to /dev/tty)
	// cannot land in the middle of one.
	p := tea.NewProgram(model, append(programOptions(cfg), tea.WithOutput(ui.SerializedStdout()))...)

	defer func() {
		if r := recover(); r != nil {
			p.Kill()
			fmt.Fprintln(os.Stderr, "panic:", config.RedactSecrets(fmt.Sprint(r), cfg))
			code = 1
		}
	}()
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", config.RedactSecrets(err.Error(), cfg))
		return 1, ""
	}
	// A finished session may ask to re-exec the freshly installed binary (in-app
	// update restart). main does the exec after this function's defers run.
	if um, ok := finalModel.(ui.Model); ok {
		um.ReleaseTerminalImages()
		restartExec = um.RestartExecPath()
		if restartExec == "" && (um.QuitActivated() || um.HasPendingDestructiveActions() || um.HasPendingSends()) {
			quitIndicator = startShutdownIndicator()
		}
		if err := um.FlushPendingDestructiveActions(); err != nil {
			if quitIndicator != nil {
				quitIndicator.Stop()
				quitIndicator = nil
			}
			fmt.Fprintln(os.Stderr, "pending message action failed:", config.RedactSecrets(err.Error(), cfg))
		}
		if err := um.FlushPendingSends(); err != nil {
			if quitIndicator != nil {
				quitIndicator.Stop()
				quitIndicator = nil
			}
			fmt.Fprintln(os.Stderr, "pending send failed:", config.RedactSecrets(err.Error(), cfg))
		}
	}
	return 0, restartExec
}

func formatConfigLoadError(path string, err error, cfg config.Config) string {
	detail := config.RedactSecrets(err.Error(), cfg)
	return fmt.Sprintf("error: could not load or migrate config %s: %s\nFix the file or permissions shown above, then restart TideMail; it will start once the config parses and any required migration can be saved.", path, detail)
}

// googleClientFromEnv reports whether the developer environment names a whole
// Google client: both the ID and the secret.
func googleClientFromEnv() bool {
	return strings.TrimSpace(os.Getenv("TIDEMAIL_GOOGLE_CLIENT_ID")) != "" &&
		strings.TrimSpace(os.Getenv("TIDEMAIL_GOOGLE_CLIENT_SECRET")) != ""
}

func applyStartupOverrides(cfg *config.Config, opts startupOptions) {
	if cfg == nil {
		return
	}
	// Google must approve TideMail's restricted Gmail scope before new OAuth
	// authorizations can be offered. A developer who supplies their own Google
	// client (see docs/google-oauth.md) can still sign in, for example with a
	// test user on a project in Testing. Keep this runtime-only so existing
	// OAuth accounts and their saved credentials are never rewritten.
	cfg.OAuth.GoogleDisabled = !googleClientFromEnv()
	if opts.disableGoogleOAuth {
		cfg.OAuth.GoogleDisabled = true
		// Retain this development/test flag's former behavior: it also simulates
		// a build without a configured Google client.
		cfg.OAuth.GoogleClientID = ""
		cfg.OAuth.GoogleClientSecret = ""
	}
}

func programOptions(cfg config.Config) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.Display.MouseSupport {
		// Cell-motion mode reports clicks/releases without the extra hover
		// noise of all-motion mode, matching TideMail's click-only scope.
		// Mouse support is opt-in: capturing mouse input takes over the
		// terminal's own native click-drag text selection in most emulators.
		opts = append(opts, tea.WithMouseCellMotion())
	}
	return opts
}

func resolvedVersion() string {
	if version != "" && version != "dev" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if ok {
		if resolved := resolvedVersionFromBuildInfo(info); resolved != "" {
			return resolved
		}
	}
	if desc := gitDescribeVersion(); desc != "" {
		return desc
	}

	return version
}

func resolvedVersionFromBuildInfo(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimSpace(info.Main.Version)
	}

	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if modified {
		revision += "-dirty"
	}
	return revision
}

func gitDescribeVersion() string {
	out, err := exec.Command("git", "describe", "--tags", "--long", "--dirty", "--always").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
