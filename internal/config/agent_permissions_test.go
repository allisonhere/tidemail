package config

import (
	"bytes"
	"github.com/BurntSushi/toml"
	"testing"
)

func TestAgentPermissionsEncodingPreservesDefaultsAndDenial(t *testing.T) {
	for _, permissions := range [][]string{nil, {}, {"messages.read"}} {
		var out bytes.Buffer
		original := Config{AgentPermissions: permissions}
		if err := toml.NewEncoder(&out).Encode(original); err != nil {
			t.Fatal(err)
		}
		var decoded Config
		if _, err := toml.Decode(out.String(), &decoded); err != nil {
			t.Fatal(err)
		}
		if (decoded.AgentPermissions == nil) != (permissions == nil) {
			t.Fatalf("permissions %#v decoded as %#v", permissions, decoded.AgentPermissions)
		}
	}
}
