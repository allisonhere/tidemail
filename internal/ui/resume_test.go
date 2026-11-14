package ui

import (
	"testing"
	"time"
)

func TestResumedAfterGap(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		prev time.Time
		gap  time.Duration
		want bool
	}{
		{"first tick", time.Time{}, time.Hour, false},
		{"normal tick", base, resumeTickInterval, false},
		{"slightly late", base, resumeTickInterval + 20*time.Second, false},
		{"suspended", base, 8 * time.Hour, true},
	}
	for _, tc := range cases {
		if got := resumedAfterGap(tc.prev, base.Add(tc.gap)); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResumeTickRearms(t *testing.T) {
	m := Model{}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	got, cmd := m.handleResumeTick(resumeTickMsg{at: now})
	if cmd == nil {
		t.Fatal("first tick must re-arm the ticker")
	}
	if !got.(Model).lastResumeTick.Equal(now) {
		t.Fatal("tick time not recorded")
	}
}
