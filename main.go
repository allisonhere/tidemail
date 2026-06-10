package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/ui"
)

var version = "dev"

type startupOptions struct {
	previewManualUpdate bool
	prototypeForms      bool
}

func parseStartupOptions(args []string) startupOptions {
	var opts startupOptions
	for _, a := range args {
		switch strings.TrimSpace(a) {
		case "--preview-manual-update":
			opts.previewManualUpdate = true
		case "--prototype-forms":
			opts.prototypeForms = true
		}
	}
	return opts
}

func main() {
	opts := parseStartupOptions(os.Args[1:])
	for _, a := range os.Args[1:] {
		switch strings.TrimSpace(a) {
		case "--version", "-version", "-v":
			fmt.Printf("tidemail %s\n", resolvedVersion())
			return
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not load config:", config.RedactSecrets(err.Error(), cfg))
		cfg = config.DefaultConfig()
	}
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

	var model tea.Model
	if opts.prototypeForms {
		model = ui.NewPrototypeFormsModel(cfg)
	} else {
		database, err := db.Open()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error opening database:", config.RedactSecrets(err.Error(), cfg))
			os.Exit(1)
		}
		defer database.Close()

		// --preview-manual-update: open Settings on Updates with a demo manual-install command (dev UI).
		model = ui.NewModel(database, cfg, resolvedVersion(), opts.previewManualUpdate)
	}

	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	defer func() {
		if r := recover(); r != nil {
			p.Kill()
			fmt.Fprintln(os.Stderr, "panic:", config.RedactSecrets(fmt.Sprint(r), cfg))
			os.Exit(1)
		}
	}()
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", config.RedactSecrets(err.Error(), cfg))
		os.Exit(1)
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
