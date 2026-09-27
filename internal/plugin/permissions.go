package plugin

import "errors"

// ErrPermissionDenied is returned, before the plugin is started, when a call
// needs a permission the plugin's manifest does not declare.
var ErrPermissionDenied = errors.New("permission denied")

// Permissions lists the capabilities a plugin asks for in its manifest.
//
// This is capability metadata, not a sandbox. A plugin runs as an ordinary
// process with the user's privileges, so nothing here stops it from opening
// sockets or reading files. The flags only decide what TideMail itself will
// choose to send to the plugin once later milestones add message events and
// annotations. Every flag defaults to false, so a plugin gets nothing it did
// not ask for, and future capabilities stay off until a plugin requests them.
type Permissions struct {
	// MessageMetadata allows TideMail to send headers such as sender, subject,
	// and date.
	MessageMetadata bool `toml:"message_metadata"`
	// MessageBody allows TideMail to send message bodies.
	MessageBody bool `toml:"message_body"`
	// Network declares that the plugin makes network requests. TideMail cannot
	// enforce this; it exists so the user can see what the plugin claims.
	Network bool `toml:"network"`
	// Annotations allows the plugin to return annotations for TideMail to
	// store and display.
	Annotations bool `toml:"annotations"`
}

// Names lists the granted permissions with short display names, in a fixed
// order.
func (p Permissions) Names() []string {
	var names []string
	for _, perm := range []struct {
		on   bool
		name string
	}{
		{p.MessageMetadata, "metadata"},
		{p.MessageBody, "body"},
		{p.Network, "network"},
		{p.Annotations, "annotations"},
	} {
		if perm.on {
			names = append(names, perm.name)
		}
	}
	return names
}
