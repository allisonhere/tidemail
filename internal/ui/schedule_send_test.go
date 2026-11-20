package ui

import (
	"testing"
	"time"
)

// TestLocalMidnightTomorrowUsesLocalDate pins "now" to a moment just after
// local midnight in a UTC+14 zone (Kiribati's Line Islands), where the local
// calendar date is a full day ahead of the UTC calendar date. The old
// now.AddDate(0,0,1).Truncate(24*time.Hour) rounds to the previous
// UTC-midnight boundary and lands on the wrong local day there; this checks
// the fix picks the correct one instead.
func TestLocalMidnightTomorrowUsesLocalDate(t *testing.T) {
	tz := time.FixedZone("UTC+14", 14*60*60)
	now := time.Date(2026, time.March, 10, 0, 30, 0, 0, tz)
	if utcDay := now.UTC().Day(); utcDay == now.Day() {
		t.Fatalf("test fixture doesn't actually straddle a UTC/local day boundary (utc day %d, local day %d)", utcDay, now.Day())
	}

	// Confirm this scenario reproduces the original bug: the naive
	// UTC-aligned truncation disagrees with the locally-correct "tomorrow".
	wantY, wantM, wantD := now.AddDate(0, 0, 1).Date()
	buggy := now.AddDate(0, 0, 1).Truncate(24 * time.Hour)
	if by, bm, bd := buggy.Date(); by == wantY && bm == wantM && bd == wantD {
		t.Fatalf("test fixture doesn't reproduce the truncation bug: both give %04d-%02d-%02d", wantY, wantM, wantD)
	}

	got := localMidnightTomorrow(now)
	if gotY, gotM, gotD := got.Date(); gotY != wantY || gotM != wantM || gotD != wantD {
		t.Fatalf("expected local tomorrow %04d-%02d-%02d, got %04d-%02d-%02d", wantY, wantM, wantD, gotY, gotM, gotD)
	}
	if got.Location() != time.Local {
		t.Fatalf("expected result in time.Local, got %v", got.Location())
	}
	if h, m, s := got.Clock(); h != 0 || m != 0 || s != 0 {
		t.Fatalf("expected midnight, got %02d:%02d:%02d", h, m, s)
	}
}
