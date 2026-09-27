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
// choose to send to the plugin. Every flag defaults to false, so a plugin
// gets nothing it did not ask for, and future capabilities stay off until a
// plugin requests them.
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

	// The read-only query permissions below let TideMail disclose selected
	// structured data to a plugin during a report (see query.go). They never
	// grant database access, bodies, or any way to change mail, and none of
	// them implies another.

	// MessagesQuery allows query.messages: allowlisted header fields of
	// messages in a scope, a bounded page at a time.
	MessagesQuery bool `toml:"messages_query"`
	// ThreadsQuery allows query.threads: conversation summaries.
	ThreadsQuery bool `toml:"threads_query"`
	// AnnotationsQuery allows query.annotations: stored plugin annotations.
	// With AnalyticsRead it also allows query.classification.
	AnnotationsQuery bool `toml:"annotations_query"`
	// AnalyticsRead allows the analytics.* queries: computed counts and
	// statistics, not message records.
	AnalyticsRead bool `toml:"analytics_read"`
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
		{p.MessagesQuery, "messages query"},
		{p.ThreadsQuery, "threads query"},
		{p.AnnotationsQuery, "annotations query"},
		{p.AnalyticsRead, "analytics"},
	} {
		if perm.on {
			names = append(names, perm.name)
		}
	}
	return names
}
