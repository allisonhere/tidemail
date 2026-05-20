package ui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	NextPane key.Binding
	PrevPane key.Binding
	Enter    key.Binding
	Back     key.Binding

	Refresh     key.Binding
	RefreshAll  key.Binding
	MarkRead    key.Binding
	MarkAllRead key.Binding
	OpenBrowser key.Binding
	NextLink    key.Binding
	PrevLink    key.Binding
	Search      key.Binding
	UnreadOnly  key.Binding

	FeedManager   key.Binding
	ThemePicker   key.Binding
	Settings      key.Binding
	UpdateInstall key.Binding
	UpdateIgnore  key.Binding
	Help          key.Binding
	Quit          key.Binding

	Summary       key.Binding // fetch/show AI summary (articles or content pane)
	CopyText      key.Binding // copy summary to clipboard
	SaveMD        key.Binding // save summary as .md file
	ContentSearch key.Binding // find text in the current article

	// Feed manager specific
	Add       key.Binding
	AddFolder key.Binding
	Edit      key.Binding
	Delete    key.Binding
	Import    key.Binding
	Export    key.Binding
	Move      key.Binding

	// Overlay / input
	Confirm   key.Binding
	Cancel    key.Binding
	Space     key.Binding
	Yes       key.Binding
	No        key.Binding
	Tab       key.Binding
	Backspace key.Binding
}

var DefaultKeys = KeyMap{
	Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
	Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
	Left:     key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "move left")),
	Right:    key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "move right")),
	NextPane: key.NewBinding(key.WithKeys("tab", "]"), key.WithHelp("tab/]", "next pane")),
	PrevPane: key.NewBinding(key.WithKeys("shift+tab", "["), key.WithHelp("shift+tab/[", "prev pane")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),

	Refresh:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "refresh feed")),
	RefreshAll:  key.NewBinding(key.WithKeys("F", "I"), key.WithHelp("F/I", "refresh all")),
	MarkRead:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "mark read")),
	MarkAllRead: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "mark all read")),
	OpenBrowser: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
	NextLink:    key.NewBinding(key.WithKeys("ctrl+n", "alt+n"), key.WithHelp("ctrl+n", "next link")),
	PrevLink:    key.NewBinding(key.WithKeys("ctrl+p", "alt+p"), key.WithHelp("ctrl+p", "prev link")),
	Search:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	UnreadOnly:  key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "toggle unread only")),

	FeedManager:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "feed manager")),
	ThemePicker:   key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "theme picker")),
	Settings:      key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "settings")),
	UpdateInstall: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "install update")),
	UpdateIgnore:  key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "ignore update")),
	Help:          key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:          key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),

	Summary:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "AI summary")),
	CopyText:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
	SaveMD:        key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "save .md")),
	ContentSearch: key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "find")),

	Add:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
	AddFolder: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "add folder")),
	Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Import:    key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "import OPML")),
	Export:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "export OPML")),
	Move:      key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "move to folder")),

	Confirm:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Space:     key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
	Yes:       key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes")),
	No:        key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),
	Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	Backspace: key.NewBinding(key.WithKeys("backspace")),
}
