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
		name      string
		in        []string
		from      string
		want      []string
		wantAdded bool
		wantAddr  string
	}{
		{"add", nil, "News <News@Example.com>", []string{"news@example.com"}, true, "news@example.com"},
		{"remove", []string{"a@x.io", "news@example.com"}, "NEWS@example.com", []string{"a@x.io"}, false, "news@example.com"},
		{"remove mixed-case entry", []string{"News@Example.com"}, "news@example.com", []string{}, false, "news@example.com"},
		{"domain entry kept on add", []string{"@example.com"}, "news@example.com", []string{"@example.com", "news@example.com"}, true, "news@example.com"},
		{"no address", []string{"a@x.io"}, "nobody", []string{"a@x.io"}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, added, addr := ToggleTrustedImageSender(tt.in, tt.from)
			if !reflect.DeepEqual(got, tt.want) || added != tt.wantAdded || addr != tt.wantAddr {
				t.Fatalf("got (%v, %v, %q), want (%v, %v, %q)", got, added, addr, tt.want, tt.wantAdded, tt.wantAddr)
			}
		})
	}
}

func TestTrustedImageSendersRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		list []string
	}{
		{"empty", nil},
		{"addresses and domain", []string{"news@example.com", "@shop.example"}},
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
