package update

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// stubOwnerProbe points the package-manager probe at canned output and makes
// the given directory look unwritable, the way /usr/bin is for a normal user.
// The foreign-package query (`pacman -Qmq`) answers "not foreign", so these
// stubs describe a package from a sync repo; stubForeignOwnerProbe covers the
// AUR case.
func stubOwnerProbe(t *testing.T, available string, out string, err error) {
	t.Helper()
	stubOwnerProbeWith(t, []string{available}, out, err, "", fmt.Errorf("not foreign"))
}

// stubForeignOwnerProbe describes a pacman package that came from the AUR, with
// the named binaries on PATH alongside pacman.
func stubForeignOwnerProbe(t *testing.T, pkg string, helpers ...string) {
	t.Helper()
	stubOwnerProbeWith(t, append([]string{"pacman"}, helpers...), pkg+"\n", nil, pkg+"\n", nil)
}

// stubOwnerProbeWith answers the ownership query and the -Qmq foreign query
// separately, because a single canned answer would make every stubbed package
// look foreign.
func stubOwnerProbeWith(t *testing.T, available []string, ownerOut string, ownerErr error, foreignOut string, foreignErr error) {
	t.Helper()
	resetOwnerCacheForTest()
	origLook, origQuery, origWritable := lookPath, queryOwner, dirWritable
	lookPath = func(name string) (string, error) {
		for _, a := range available {
			if name == a {
				return "/usr/bin/" + name, nil
			}
		}
		return "", fmt.Errorf("not found")
	}
	queryOwner = func(_ string, args ...string) (string, error) {
		for _, arg := range args {
			if arg == "-Qmq" {
				return foreignOut, foreignErr
			}
		}
		return ownerOut, ownerErr
	}
	dirWritable = func(string) error { return fmt.Errorf("read-only") }
	t.Cleanup(func() {
		lookPath, queryOwner, dirWritable = origLook, origQuery, origWritable
		resetOwnerCacheForTest()
	})
}

func TestOwningPackageDetectsPacmanBinary(t *testing.T) {
	stubOwnerProbe(t, "pacman", "tidemail-bin\n", nil)

	owner, owned := owningPackage("/usr/bin/tidemail")
	if !owned {
		t.Fatal("expected /usr/bin/tidemail to be reported as package-managed")
	}
	if owner.Package != "tidemail-bin" || owner.Manager != "pacman" {
		t.Fatalf("unexpected owner: %+v", owner)
	}
	if got, want := owner.UpdateCommand(), "sudo pacman -Syu tidemail-bin"; got != want {
		t.Fatalf("update command = %q, want %q", got, want)
	}
}

func TestOwningPackageParsesDpkgOutput(t *testing.T) {
	stubOwnerProbe(t, "dpkg-query", "tidemail:amd64: /usr/bin/tidemail\n", nil)

	owner, owned := owningPackage("/usr/bin/tidemail")
	if !owned || owner.Package != "tidemail" {
		t.Fatalf("expected dpkg owner tidemail, got %+v owned=%v", owner, owned)
	}
}

func TestOwningPackageIgnoresUnownedFileError(t *testing.T) {
	stubOwnerProbe(t, "pacman", "", fmt.Errorf("No package owns /usr/bin/tidemail"))

	if _, owned := owningPackage("/usr/bin/tidemail"); owned {
		t.Fatal("unowned binary must not be reported as package-managed")
	}
}

// pacman prints an error line to stdout in some locales; a path or spaces in
// the output means we did not get a bare package name.
func TestOwningPackageRejectsNonPackageOutput(t *testing.T) {
	stubOwnerProbe(t, "pacman", "error: No package owns /usr/bin/tidemail\n", nil)

	if _, owned := owningPackage("/usr/bin/tidemail"); owned {
		t.Fatal("error text must not be parsed as a package name")
	}
}

// The probe must not run for a binary in a directory we can write, which is
// where install.sh puts it.
func TestOwningPackageSkipsProbeForWritableDirectory(t *testing.T) {
	resetOwnerCacheForTest()
	origLook, origQuery := lookPath, queryOwner
	probed := false
	lookPath = func(string) (string, error) { probed = true; return "", fmt.Errorf("not found") }
	queryOwner = func(string, ...string) (string, error) { probed = true; return "", nil }
	t.Cleanup(func() {
		lookPath, queryOwner = origLook, origQuery
		resetOwnerCacheForTest()
	})

	exe := filepath.Join(t.TempDir(), "tidemail")
	if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, owned := owningPackage(exe); owned {
		t.Fatal("a binary in a writable directory is not package-managed")
	}
	if probed {
		t.Fatal("package manager must not be queried for a writable install directory")
	}
}

// An AUR install must be told to run pacman rather than have a second copy
// written into ~/.local/bin that shadows the packaged binary.
func TestInstallRefusesToShadowPackageManagedBinary(t *testing.T) {
	stubOwnerProbe(t, "pacman", "tidemail-bin\n", nil)

	tmp := t.TempDir()
	staged := filepath.Join(tmp, "tidemail")
	if err := os.WriteFile(staged, []byte("new binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := New()
	result, err := u.Install(DownloadedAsset{BinaryPath: staged}, "/usr/bin/tidemail")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !result.RequiresManual {
		t.Fatal("expected a package-managed install to require a manual update")
	}
	if got, want := result.ManualCommand, "sudo pacman -Syu tidemail-bin"; got != want {
		t.Fatalf("manual command = %q, want %q", got, want)
	}
	if result.Restartable {
		t.Fatal("nothing was installed, so the result must not offer a restart")
	}
}

// The probe result is memoised, but a different executable path must be probed
// again instead of inheriting the previous path's answer.
func TestOwningPackageCacheIsKeyedOnPath(t *testing.T) {
	resetOwnerCacheForTest()
	origLook, origQuery, origWritable := lookPath, queryOwner, dirWritable
	lookPath = func(name string) (string, error) {
		if name == "pacman" {
			return "/usr/bin/pacman", nil
		}
		return "", fmt.Errorf("not found")
	}
	queryOwner = func(_ string, args ...string) (string, error) {
		// The queried path is the last argument.
		switch args[len(args)-1] {
		case "/usr/bin/tidemail":
			return "tidemail-bin\n", nil
		default:
			return "", fmt.Errorf("No package owns that file")
		}
	}
	dirWritable = func(string) error { return fmt.Errorf("read-only") }
	t.Cleanup(func() {
		lookPath, queryOwner, dirWritable = origLook, origQuery, origWritable
		resetOwnerCacheForTest()
	})

	if _, owned := owningPackage("/usr/bin/tidemail"); !owned {
		t.Fatal("expected /usr/bin/tidemail to be package-managed")
	}
	if _, owned := owningPackage("/opt/custom/tidemail"); owned {
		t.Fatal("a different path must be probed again, not served from the cache")
	}
	// The original path still answers from the cache.
	if _, owned := owningPackage("/usr/bin/tidemail"); !owned {
		t.Fatal("re-querying the original path must still report it as owned")
	}
}

// An AUR package cannot be updated by pacman — `pacman -Syu` skips foreign
// packages silently — so the command has to come from an AUR helper.
func TestForeignPacmanPackageUsesAURHelper(t *testing.T) {
	stubForeignOwnerProbe(t, "tidemail-bin", "yay")

	owner, owned := owningPackage("/usr/bin/tidemail")
	if !owned || !owner.Foreign {
		t.Fatalf("expected a foreign pacman package, got %+v owned=%v", owner, owned)
	}
	if got, want := owner.UpdateCommand(), "yay -S tidemail-bin"; got != want {
		t.Fatalf("update command = %q, want %q", got, want)
	}
}

// A package from a sync repo keeps the plain pacman command.
func TestRepoPacmanPackageKeepsPacmanCommand(t *testing.T) {
	stubOwnerProbe(t, "pacman", "tidemail\n", nil)

	owner, owned := owningPackage("/usr/bin/tidemail")
	if !owned {
		t.Fatal("expected the binary to be package-managed")
	}
	if owner.Foreign {
		t.Fatal("a sync-repo package must not be marked foreign")
	}
	if got, want := owner.UpdateCommand(), "sudo pacman -Syu tidemail"; got != want {
		t.Fatalf("update command = %q, want %q", got, want)
	}
}

// With no helper installed, build from the AUR. It must not fall back to the
// install script, which would drop a second binary in ~/.local/bin shadowing
// the packaged one.
func TestForeignPacmanPackageWithoutHelperBuildsFromAUR(t *testing.T) {
	stubForeignOwnerProbe(t, "tidemail-bin")

	owner, _ := owningPackage("/usr/bin/tidemail")
	got := owner.UpdateCommand()
	if got == SuggestedManualInstallScript {
		t.Fatal("the install script would shadow the packaged binary")
	}
	if want := aurFallbackCommand("tidemail-bin"); got != want {
		t.Fatalf("update command = %q, want %q", got, want)
	}
}

// Helper choice follows the documented order rather than PATH order.
func TestForeignPacmanPackagePrefersFirstListedHelper(t *testing.T) {
	stubForeignOwnerProbe(t, "tidemail-bin", "paru", "yay")

	owner, _ := owningPackage("/usr/bin/tidemail")
	if got, want := owner.UpdateCommand(), "yay -S tidemail-bin"; got != want {
		t.Fatalf("update command = %q, want %q", got, want)
	}
}
