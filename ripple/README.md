# ripple

A keyboard-first, soft-wrapping multi-line text editor component for
[Bubble Tea](https://github.com/charmbracelet/bubbletea).

`ripple` is a self-contained editing surface — selection, undo/redo,
system-clipboard copy/cut/paste, word movement, and grapheme-width-aware soft
wrap — with no opinion about how the host frames or styles it. It was extracted
from [TideMail](https://github.com/allisonhere/tidemail)'s compose editor.

## Install

```bash
go get github.com/allisonhere/ripple
```

## Usage

Embed `ripple.Model` in your own model, size it, route messages through
`Update`, and render with `View`:

```go
type app struct{ ed ripple.Model }

func (a app) Init() tea.Cmd { return nil }

func (a app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.ed.SetSize(msg.Width, msg.Height)
		return a, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc {
			return a, tea.Quit
		}
	}
	var cmd tea.Cmd
	a.ed, cmd = a.ed.Update(msg)
	return a, cmd
}

func (a app) View() string {
	return a.ed.View(ripple.Options{Cursor: "█"})
}
```

`Update` returns the updated model and a command, like any Bubble Tea component.

## Clipboard

Copy/cut/paste emit their clipboard side effect as a `tea.Cmd`, so it travels
back with the model. Provide a backend with `SetClipboard`; without one those
keys are inert:

```go
type osClipboard struct{}

func (osClipboard) Read() (string, error)  { return clipboard.ReadAll() }
func (osClipboard) Write(s string) error   { return clipboard.WriteAll(s) }

ed.SetClipboard(osClipboard{})
```

`ctrl+v` reads the clipboard and feeds the text back through `Update` as a
`ripple.PasteMsg`; forward all messages to the focused editor and it is handled
for you.

## Wrapping

The editor owns wrapping: it fits every visual line to the width passed to
`SetSize`. **Do not re-wrap the string returned by `View`** — measuring it with
a different width model can disagree on wide runes and overflow. Clip or pad
each line to the configured width instead.

## Keys

| Action | Default |
|--------|---------|
| Move | arrows; `ctrl`+arrows by word; home/end; ctrl+home/end |
| Select | `shift`+movement; `ctrl+shift`+arrows by word |
| Select all | `ctrl+a` |
| Undo / redo | `ctrl+z` / `ctrl+y` |
| Copy / cut / paste | `ctrl+c` / `ctrl+x` / `ctrl+v` |
| Page | pgup / pgdn |

Editing commands are configurable via `KeyMap` and `SetKeyMap`. Text input and
cursor movement are fixed.

## License

MIT (see repository).
