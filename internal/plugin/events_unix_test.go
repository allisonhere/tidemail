//go:build unix

package plugin

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestEventsCloseKillsRunningProcess(t *testing.T) {
	e, _, dir := realEventManager(t, "tidemail-plugin-hangpid", eventManifest, time.Minute)
	e.SetEnabled("p", true)
	e.Enqueue(MessageMetadata{ID: 1})
	var pid int
	waitFor(t, "plugin to start", func() bool {
		data, err := os.ReadFile(filepath.Join(dir, "PID"))
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(data))
		return err == nil
	})
	start := time.Now()
	if !e.Close(5 * time.Second) {
		t.Fatal("workers did not exit")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("close took %v", elapsed)
	}
	waitFor(t, "plugin process to exit", func() bool {
		return syscall.Kill(pid, 0) != nil
	})
}
