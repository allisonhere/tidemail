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

	Sync        key.Binding
	SyncAll     key.Binding
	MarkRead    key.Binding
	MarkAllRead key.Binding
	OpenBrowser key.Binding
	NextLink    key.Binding
	PrevLink    key.Binding
	Search      key.Binding
	UnreadOnly  key.Binding
	Compose     key.Binding
	Reply       key.Binding
	Forward     key.Binding
	Archive     key.Binding
	Command     key.Binding

	AccountManager key.Binding
	ThemePicker    key.Binding
	Settings       key.Binding
	UpdateInstall  key.Binding
	UpdateIgnore   key.Binding
	Help           key.Binding
	Quit           key.Binding

	Summary       key.Binding
	CopyText      key.Binding
	SaveMD        key.Binding
	ContentSearch key.Binding
	ToggleQuote   key.Binding
	ToggleHeaders key.Binding
	SaveAttach    key.Binding

	// Account manager specific
	Add         key.Binding
	Edit        key.Binding
	Delete      key.Binding
	SaveAccount key.Binding
	TestAccount key.Binding

	AttachFile   key.Binding
	RemoveAttach key.Binding
	CycleSender  key.Binding
	GrammarCheck key.Binding

	// Overlay / input
	Confirm   key.Binding
	Cancel    key.Binding
	Space     key.Binding
	SelectAll key.Binding
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

	Sync:        key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync mailbox")),
	SyncAll:     key.NewBinding(key.WithKeys("F", "ctrl+s"), key.WithHelp("F/ctrl+s", "sync all")),
	MarkRead:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "mark read")),
	MarkAllRead: key.NewBinding(key.WithKeys("R", "ctrl+x"), key.WithHelp("R/ctrl+x", "mark all read")),
	OpenBrowser: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
	NextLink:    key.NewBinding(key.WithKeys("ctrl+n", "alt+n"), key.WithHelp("ctrl+n", "next link")),
	PrevLink:    key.NewBinding(key.WithKeys("ctrl+p", "alt+p"), key.WithHelp("ctrl+p", "prev link")),
	Search:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	UnreadOnly:  key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "toggle unread only")),
	Compose:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "compose")),
	Reply:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reply")),
	Forward:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "forward")),
	Archive:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
	Command:     key.NewBinding(key.WithKeys("p", ":"), key.WithHelp("p", "command")),

	AccountManager: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "accounts")),
	ThemePicker:    key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "theme picker")),
	Settings:       key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "settings")),
	UpdateInstall:  key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "install update")),
	UpdateIgnore:   key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "ignore update")),
	Help:           key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:           key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),

	Summary:       key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "AI summary")),
	CopyText:      key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "copy")),
	SaveMD:        key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "save .md")),
	ContentSearch: key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "find")),
	ToggleQuote:   key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "toggle quoted text")),
	ToggleHeaders: key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "headers")),

	Add:         key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
	Edit:        key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Delete:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	SaveAccount: key.NewBinding(key.WithKeys("ctrl+s", "f2"), key.WithHelp("ctrl+s", "save")),
	TestAccount: key.NewBinding(key.WithKeys("ctrl+t", "f5"), key.WithHelp("ctrl+t", "test")),

	AttachFile: key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "attach file")),

	GrammarCheck: key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "fix grammar")),

	RemoveAttach: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "remove attachment")),

	CycleSender: key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "change sender")),

	SaveAttach: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "save attachments")),

	Confirm:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Space:     key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
	SelectAll: key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "select all")),
	Yes:       key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes")),
	No:        key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "no")),
	Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	Backspace: key.NewBinding(key.WithKeys("backspace")),
}
