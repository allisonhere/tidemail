package plugin

import (
	"context"
	"errors"
	"testing"
)

func TestBookContactsPermissionsBeforeLaunch(t *testing.T) {
	m := cryptoManager(t, "tidemail-plugin-contactsgood", metadataPermission)
	if _, err := m.ListBookContacts(context.Background(), "p", ""); !errors.Is(err, ErrPermissionDenied) || ranMarker(t, m) {
		t.Fatal("read permission not enforced", err)
	}
	m = cryptoManager(t, "tidemail-plugin-contactsgood", contactsPermission)
	if _, err := m.SaveBookContact(context.Background(), "p", BookContact{}); !errors.Is(err, ErrPermissionDenied) || ranMarker(t, m) {
		t.Fatal("write permission not enforced", err)
	}
}
