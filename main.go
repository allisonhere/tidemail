package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/ui"
)

var version = "dev"

type startupOptions struct {
	previewManualUpdate   bool
	previewUpdateProgress bool
	prototypeForms        bool
	disableGoogleOAuth    bool
	// openMessageID is the message a panel asked us to show, by the id in the
	// cache both programs read (messages.id). Zero means nobody asked.
	openMessageID int64
}

// parseStartupOptions reads the flags. A flag that takes a value reads it from
// the argument after it, and an unknown argument is ignored: a terminal or a
// desktop environment hands a program arguments it did not ask for.
func parseStartupOptions(args []string) (startupOptions, error) {
	var opts startupOptions
	for i := 0; i < len(args); i++ {
		switch strings.TrimSpace(args[i]) {
		case "--preview-manual-update":
			opts.previewManualUpdate = true
		case "--preview-update-progress":
			opts.previewUpdateProgress = true
		case "--prototype-forms":
			opts.prototypeForms = true
		case "--disable-google-oauth":
			opts.disableGoogleOAuth = true
		case "--open":
			if i+1 >= len(args) {
				return startupOptions{}, fmt.Errorf("--open needs a message id")
			}
			id, err := strconv.ParseInt(strings.TrimSpace(args[i+1]), 10, 64)
			if err != nil || id <= 0 {
				return startupOptions{}, fmt.Errorf("--open needs a message id, not %q", args[i+1])
			}
			opts.openMessageID = id
			i++ // the value is not an argument of its own
		}
	}
	return opts, nil
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
	opts, err := parseStartupOptions(os.Args[1:])
	if err != nil {
		// Refusing here is the point: a mistyped id that started the client
		// anyway would look exactly like the feature not working.
		fmt.Fprintln(os.Stderr, "tidemail:", err)
		return 1, ""
	}
	for _, a := range os.Args[1:] {
		switch strings.TrimSpace(a) {
		case "--version", "-version", "-v":
			fmt.Printf("tidemail %s\n", resolvedVersion())
			return 0, ""
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
		m := ui.NewModel(database, cfg, resolvedVersion(), opts.previewManualUpdate || opts.previewUpdateProgress)
		if opts.previewUpdateProgress {
			m.ApplyUpdateProgressPreview()
		}
		model = m
	}
	if uiModel, ok := model.(ui.Model); ok {
		defer uiModel.CloseSessions()
	}

	p := tea.NewProgram(model, programOptions()...)

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

func applyStartupOverrides(cfg *config.Config, opts startupOptions) {
	if cfg == nil || !opts.disableGoogleOAuth {
		return
	}
	// Development/test override: exercise the same account-manager path a user
	// sees when they have not configured a Google OAuth client. This is
	// runtime-only; never rewrite the user's saved credentials.
	cfg.OAuth.GoogleClientID = ""
	cfg.OAuth.GoogleClientSecret = ""
	cfg.OAuth.GoogleDisabled = true
}

func programOptions() []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithAltScreen(),
	}
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
