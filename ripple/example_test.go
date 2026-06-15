package ripple_test

import (
	"fmt"

	"github.com/allisonhere/ripple"
	tea "github.com/charmbracelet/bubbletea"
)

// Example shows the standard Bubble Tea component wiring: route messages through
// Update and render with View.
func Example() {
	ed := ripple.New()
	ed.SetSize(20, 3)

	// Type "hi" by feeding key messages, as Bubble Tea would.
	ed, _ = ed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})

	fmt.Printf("%q", ed.Value())
	// Output: "hi"
}
