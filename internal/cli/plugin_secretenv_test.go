package cli

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/plugin"
)

func TestParseSecretEnvsRejectsAnEmptyValue(t *testing.T) {
	man := plugin.Manifest{Settings: []plugin.SettingSpec{{Key: "api_key", Type: plugin.SettingSecret}}}
	t.Setenv("EMPTY_SECRET", "")
	_, err := parseSecretEnvs(man, []string{"api_key=EMPTY_SECRET"})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("an empty secret env should be refused clearly, got %v", err)
	}
}
