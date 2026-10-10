package ui

import (
	"encoding/json"
	"errors"

	"github.com/allisonhere/tidemail/internal/config"
)

var errNoStateStore = errors.New("no database to keep update state in")

// updateStateKey is the settings-table row holding the update checker's state.
const updateStateKey = "update_state"

// persistedUpdateState is what survives a restart. The "available update"
// fields are deliberately absent: the banner is check-first, so they were
// always cleared at startup and never read back.
type persistedUpdateState struct {
	LastCheckedUnix  int64  `json:"last_checked_unix"`
	DismissedVersion string `json:"dismissed_version"`
}

// loadUpdateState restores the update checker's state from the database. With
// nothing stored yet, it keeps whatever an older config.toml carried and stores
// that, so an upgrade doesn't forget a dismissed update or re-check at once.
func (m *Model) loadUpdateState() {
	if m.db == nil {
		return
	}
	if raw, err := m.db.GetSetting(updateStateKey); err == nil && raw != "" {
		var st persistedUpdateState
		if json.Unmarshal([]byte(raw), &st) == nil {
			m.cfg.Updates.LastCheckedUnix = st.LastCheckedUnix
			m.cfg.Updates.DismissedVersion = st.DismissedVersion
			return
		}
	}
	_ = m.saveUpdateState()
}

// saveUpdateState writes the live update-check state to the database.
func (m *Model) saveUpdateState() error {
	if m.db == nil {
		return errNoStateStore
	}
	raw, err := json.Marshal(persistedUpdateState{
		LastCheckedUnix:  m.cfg.Updates.LastCheckedUnix,
		DismissedVersion: m.cfg.Updates.DismissedVersion,
	})
	if err != nil {
		return err
	}
	return m.db.SetSetting(updateStateKey, string(raw))
}

// withoutUpdateState returns cfg minus the update checker's runtime state, which
// belongs in the database rather than in config.toml.
func withoutUpdateState(cfg config.Config) config.Config {
	cfg.Updates.LastCheckedUnix = 0
	cfg.Updates.DismissedVersion = ""
	cfg.Updates.AvailableVersion = ""
	cfg.Updates.AvailableSummary = ""
	cfg.Updates.AvailablePublished = 0
	return cfg
}
