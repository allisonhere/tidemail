package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type notificationFocusMsg struct{ err error }

type notificationClient struct {
	Address        string `json:"address"`
	PID            int    `json:"pid"`
	FocusHistoryID int    `json:"focusHistoryID"`
}

// Pin the terminal window when the notifier starts, rather than focusing an
// arbitrary window with the same title when the notification is clicked.
func notificationWindow() string {
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "hyprctl", "clients", "-j").Output()
	if err != nil {
		return ""
	}
	var clients []notificationClient
	if json.Unmarshal(raw, &clients) != nil {
		return ""
	}
	var ancestors []int
	for pid := os.Getpid(); pid > 1 && len(ancestors) < 64; {
		ancestors = append(ancestors, pid)
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			break
		}
		parent := 0
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PPid:") {
				parent, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
				break
			}
		}
		if parent == pid {
			break
		}
		pid = parent
	}
	return chooseNotificationWindow(clients, ancestors)
}

func chooseNotificationWindow(clients []notificationClient, ancestors []int) string {
	for _, pid := range ancestors {
		var matches []notificationClient
		for _, client := range clients {
			if client.PID == pid && validNotificationAddress(client.Address) {
				matches = append(matches, client)
			}
		}
		if len(matches) == 1 {
			return matches[0].Address
		}
		// A terminal server may own multiple windows. At startup only accept its
		// focused window; never guess which background terminal hosts TideMail.
		for _, client := range matches {
			if client.FocusHistoryID == 0 {
				return client.Address
			}
		}
		if len(matches) > 1 {
			return ""
		}
	}
	return ""
}

func validNotificationAddress(address string) bool {
	if !strings.HasPrefix(address, "0x") {
		return false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(address, "0x"), 16, 64)
	return err == nil && n != 0
}

func raiseNotificationWindowCmd(address string) tea.Cmd {
	return func() tea.Msg {
		if !validNotificationAddress(address) {
			return notificationFocusMsg{err: errors.New("window activation requires a local Hyprland terminal")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := focusNotificationWindow(ctx, address, func(ctx context.Context, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, "hyprctl", args...).CombinedOutput()
		})
		return notificationFocusMsg{err: err}
	}
}

// Current Hyprland uses Lua dispatchers; retain support for older releases.
func focusNotificationWindow(ctx context.Context, address string, run func(context.Context, ...string) ([]byte, error)) error {
	commands := [][]string{
		{"dispatch", fmt.Sprintf("hl.dsp.focus({ window = \"address:%s\" })", address)},
		{"dispatch", "focuswindow", "address:" + address},
	}
	var lastErr error
	for _, args := range commands {
		output, err := run(ctx, args...)
		if err == nil && strings.TrimSpace(string(output)) == "ok" {
			return nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("hyprctl: %s", strings.TrimSpace(string(output)))
		}
		if ctx.Err() != nil {
			break
		}
	}
	return lastErr
}
