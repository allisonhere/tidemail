package config

import (
	"reflect"
	"testing"
)

func TestIsTrustedImageSender(t *testing.T) {
	trusted := []string{"news@example.com", "@Shop.Example"}
	tests := []struct {
		name string
		from string
		want bool
	}{
		{"exact", "news@example.com", true},
		{"case insensitive", `"News" <NEWS@Example.COM>`, true},
		{"domain entry", "deals@shop.example", true},
		{"domain entry is not a suffix match", "x@badshop.example", false},
		{"untrusted", "other@example.com", false},
		{"empty", "", false},
		{"no address", "Just A Name", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTrustedImageSender(trusted, tt.from); got != tt.want {
				t.Fatalf("IsTrustedImageSender(%q) = %v, want %v", tt.from, got, tt.want)
			}
		})
	}
}

func TestToggleTrustedImageSender(t *testing.T) {
	tests := []struct {
		name       string
		in         []string
		from       string
		want       []string
		wantChange TrustChange
		wantAddr   string
	}{
		{"add", nil, "News <News@Example.com>", []string{"news@example.com"}, TrustAdded, "news@example.com"},
		{"remove", []string{"a@x.io", "news@example.com"}, "NEWS@example.com", []string{"a@x.io"}, TrustRemoved, "news@example.com"},
		{"remove mixed-case entry", []string{"News@Example.com"}, "news@example.com", []string{}, TrustRemoved, "news@example.com"},
		{"no address", []string{"a@x.io"}, "nobody", []string{"a@x.io"}, TrustUnchanged, ""},

		// Regression: removing a sender of a trusted domain used to drop only
		// an exact entry, so the sender stayed trusted through "@domain".
		{"exclude from trusted domain", []string{"@example.com"}, "news@example.com",
			[]string{"@example.com", "!news@example.com"}, TrustExcluded, "news@example.com"},
		{"remove exact entry still covered by domain excludes it", []string{"@example.com", "news@example.com"}, "news@example.com",
			[]string{"@example.com", "!news@example.com"}, TrustExcluded, "news@example.com"},
		{"drop exclusion re-trusts via domain", []string{"@example.com", "!News@example.com"}, "news@example.com",
			[]string{"@example.com"}, TrustExclusionDrop, "news@example.com"},
		{"stale exclusion without domain adds exact", []string{"!news@example.com"}, "news@example.com",
			[]string{"news@example.com"}, TrustAdded, "news@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := append([]string(nil), tt.in...)
			got, change, addr := ToggleTrustedImageSender(in, tt.from)
			if !reflect.DeepEqual(got, tt.want) || change != tt.wantChange || addr != tt.wantAddr {
				t.Fatalf("got (%v, %v, %q), want (%v, %v, %q)", got, change, addr, tt.want, tt.wantChange, tt.wantAddr)
			}
			if !reflect.DeepEqual(in, tt.in) {
				t.Fatalf("input list was modified: %v", in)
			}
			trusted := IsTrustedImageSender(got, tt.from)
			wantTrusted := tt.wantChange == TrustAdded || tt.wantChange == TrustExclusionDrop
			if tt.wantChange != TrustUnchanged && trusted != wantTrusted {
				t.Fatalf("after %v, trusted = %v", change, trusted)
			}
		})
	}
}

func TestExclusionBeatsDomainTrust(t *testing.T) {
	trusted := []string{"@example.com", "!news@example.com"}
	if IsTrustedImageSender(trusted, "News <NEWS@example.com>") {
		t.Fatal("excluded sender is still trusted through its domain")
	}
	if !IsTrustedImageSender(trusted, "deals@example.com") {
		t.Fatal("exclusion of one sender untrusted the whole domain")
	}
	// An exclusion alone never trusts anyone.
	if IsTrustedImageSender([]string{"!news@example.com"}, "news@example.com") {
		t.Fatal("exclusion entry trusted its own address")
	}
}

func TestTrustedImageSendersRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		list []string
	}{
		{"empty", nil},
		{"addresses and domain", []string{"news@example.com", "@shop.example"}},
		{"with exclusion", []string{"@shop.example", "!spam@shop.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			cfg := DefaultConfig()
			cfg.Display.TrustedImageSenders = tt.list
			if err := Save(cfg); err != nil {
				t.Fatalf("Save: %v", err)
			}
			loaded, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(tt.list) == 0 && len(loaded.Display.TrustedImageSenders) == 0 {
				return
			}
			if !reflect.DeepEqual(loaded.Display.TrustedImageSenders, tt.list) {
				t.Fatalf("round trip = %v, want %v", loaded.Display.TrustedImageSenders, tt.list)
			}
			if loaded.Display.Images != cfg.Display.Images {
				t.Fatalf("images setting changed: %q", loaded.Display.Images)
			}
		})
	}
}
