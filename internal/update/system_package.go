package update

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A binary installed by a distro package manager (the AUR's tidemail-bin,
// a .deb, an .rpm) must be updated by that package manager. Installing over
// it needs root, and the ~/.local/bin fallback in installTarget would leave a
// second copy shadowing the packaged one — so `pacman -Syu` would report
// tidemail up to date while a stale binary kept running. Detect that case and
// hand the user their package manager's command instead.

// packageOwner is the distro package that owns the running executable.
type packageOwner struct {
	Manager string // pacman, dpkg, rpm
	Package string // tidemail-bin
}

// UpdateCommand is the shell command that upgrades this package in place.
func (o packageOwner) UpdateCommand() string {
	switch o.Manager {
	case "pacman":
		return "sudo pacman -Syu " + o.Package
	case "dpkg":
		return "sudo apt update && sudo apt install --only-upgrade " + o.Package
	case "rpm":
		return "sudo dnf upgrade " + o.Package
	}
	return ""
}

// probeTimeout keeps a wedged package manager from stalling a render pass.
const probeTimeout = 2 * time.Second

var (
	ownerOnce   sync.Once
	cachedOwner packageOwner
	cachedOwned bool

	// Swapped in tests.
	lookPath   = exec.LookPath
	queryOwner = runQueryOwner
)

// resetOwnerCacheForTest clears the memoised probe result.
func resetOwnerCacheForTest() {
	ownerOnce = sync.Once{}
	cachedOwner = packageOwner{}
	cachedOwned = false
}

// owningPackage reports the distro package that owns path. The result is
// memoised: the executable path cannot change while we run, and the probe
// runs subprocesses on a path the UI touches every frame.
func owningPackage(path string) (packageOwner, bool) {
	ownerOnce.Do(func() {
		cachedOwner, cachedOwned = probeOwningPackage(path)
	})
	return cachedOwner, cachedOwned
}

func probeOwningPackage(path string) (packageOwner, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return packageOwner{}, false
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	// Only ask when the executable sits somewhere we cannot write anyway;
	// a binary in $HOME is never package-managed and this skips the probe
	// entirely for the common install.sh case.
	if err := dirWritable(filepath.Dir(path)); err == nil {
		return packageOwner{}, false
	}

	for _, probe := range []struct {
		manager string
		bin     string
		args    []string
		parse   func(string) string
	}{
		{"pacman", "pacman", []string{"-Qoq"}, parsePacmanOwner},
		{"dpkg", "dpkg-query", []string{"-S"}, parseDpkgOwner},
		{"rpm", "rpm", []string{"-qf", "--queryformat", "%{NAME}"}, parseRPMOwner},
	} {
		if _, err := lookPath(probe.bin); err != nil {
			continue
		}
		out, err := queryOwner(probe.bin, append(probe.args, path)...)
		if err != nil {
			continue
		}
		if pkg := probe.parse(out); pkg != "" {
			return packageOwner{Manager: probe.manager, Package: pkg}, true
		}
	}
	return packageOwner{}, false
}

func runQueryOwner(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

// pacman -Qoq prints just the package name.
func parsePacmanOwner(out string) string {
	return firstLineField(out)
}

// dpkg-query -S prints "package: /usr/bin/tidemail"; the package may carry an
// ":arch" suffix on multi-arch systems.
func parseDpkgOwner(out string) string {
	line := firstLine(out)
	name, _, ok := strings.Cut(line, ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(name)
}

// rpm -qf with %{NAME} prints the bare package name.
func parseRPMOwner(out string) string {
	return firstLineField(out)
}

func firstLine(out string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(line)
}

func firstLineField(out string) string {
	line := firstLine(out)
	if strings.ContainsAny(line, " \t/") {
		return ""
	}
	return line
}
