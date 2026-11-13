package plugin

import (
	"context"
	"errors"
	"testing"
)

const contactsPermission = "[permissions]\ncontacts_read = true\n"

func init() {
	helpers["tidemail-plugin-contactsgood"] = helperCrypto(`{"contacts":[{"addr":"ann@example.com","name":"Ann Lee"},{"addr":"bob@example.com"}]}`)
	helpers["tidemail-plugin-contactsbad"] = helperCrypto(`{"contacts":[
		{"addr":"not an address"},
		{"addr":"Eve <eve@example.com>"},
		{"addr":"ok@example.com","name":"Ann\u001b[31m"},
		{"addr":"fine@example.com","name":"Fine"}]}`)
}

func TestSearchContactsNeedsItsPermission(t *testing.T) {
	m := cryptoManager(t, "tidemail-plugin-contactsgood", metadataPermission)
	_, err := m.SearchContacts(context.Background(), "p", "an")
	if !errors.Is(err, ErrPermissionDenied) || ranMarker(t, m) {
		t.Fatalf("without contacts_read: err=%v ran=%v", err, ranMarker(t, m))
	}
}

func TestSearchContactsReturnsMatches(t *testing.T) {
	m := cryptoManager(t, "tidemail-plugin-contactsgood", contactsPermission)
	got, err := m.SearchContacts(context.Background(), "p", "an")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Addr != "ann@example.com" || got[0].Name != "Ann Lee" || got[1].Addr != "bob@example.com" {
		t.Fatalf("matches = %+v", got)
	}
}

func TestSearchContactsDropsUnsafeMatches(t *testing.T) {
	m := cryptoManager(t, "tidemail-plugin-contactsbad", contactsPermission)
	got, err := m.SearchContacts(context.Background(), "p", "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Addr != "fine@example.com" {
		t.Fatalf("only the clean match should survive, got %+v", got)
	}
}

func TestSearchContactsSkipsEmptyQueryWithoutStartingPlugin(t *testing.T) {
	m := cryptoManager(t, "tidemail-plugin-contactsgood", contactsPermission)
	got, err := m.SearchContacts(context.Background(), "p", "   ")
	if err != nil || got != nil || ranMarker(t, m) {
		t.Fatalf("empty query: got=%v err=%v ran=%v", got, err, ranMarker(t, m))
	}
}

func TestGenericCallRefusesContactsSearch(t *testing.T) {
	m := cryptoManager(t, "tidemail-plugin-contactsgood", contactsPermission)
	if _, err := m.Call(context.Background(), "p", MethodContactsSearch, nil); err == nil || ranMarker(t, m) {
		t.Fatalf("Call(contacts.search) should be refused before starting the plugin (err=%v)", err)
	}
}
