package plugin

import (
	"os"
	"strings"
	"testing"
)

func TestPluginEnvKeepsIsolatedProfile(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(key, root)
	}
	t.Setenv("TIDEMAIL_PRIVATE_TEST_SECRET", "never-forward")
	env := strings.Join(pluginEnv(), "\n")
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		if !strings.Contains(env, key+"="+os.Getenv(key)) {
			t.Fatal("profile lost", key)
		}
	}
	if strings.Contains(env, "never-forward") {
		t.Fatal("secret environment leaked")
	}
}
