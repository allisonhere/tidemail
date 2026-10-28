package config

import "testing"

func TestSignatureConfigurationRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.Accounts = []AccountConfig{
		{ID: "signature-test-personal", Name: "Personal", User: "personal@example.com", AuthMethod: AuthPassword, Signature: "Personal\nSecond line"},
		{ID: "signature-test-work", Name: "Work", User: "work@example.com", AuthMethod: AuthPassword, Signature: "Work\nSecond line"},
	}
	cfg.DefaultAccountID = cfg.Accounts[1].ID
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range cfg.Accounts {
		account, ok := loaded.AccountByID(expected.ID)
		if !ok || account.Signature != expected.Signature {
			t.Fatal("TOML save/load lost signature or stable account association")
		}
	}
	account, ok := loaded.DefaultAccount()
	if !ok || account.ID != cfg.Accounts[1].ID || account.Signature != cfg.Accounts[1].Signature {
		t.Fatal("default sender resolved wrong signature")
	}
}
