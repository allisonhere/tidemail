package cli

import "os"

// agentEnv, set to exactly "1", forces agent mode for mail commands.
const agentEnv = "TIDEMAIL_AGENT"

// agentForced reports whether the session environment forces agent mode.
func agentForced() bool { return os.Getenv(agentEnv) == "1" }

// agentMode returns the effective agent setting. Call it after flag parsing,
// so an explicit --agent=false cannot turn the session environment off.
func agentMode(requested bool) bool { return requested || agentForced() }
