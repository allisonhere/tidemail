package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// composeDoubleClick is how close in time two presses on the same cell must be
// to count as a double-click.
const composeDoubleClick = 400 * time.Millisecond

// composeMouseState is the gesture state for the compose body editor. Ripple
// supplies the editing operations; recognizing clicks and drags is the host's
// job, so it lives here.
type composeMouseState struct {
	dragging bool
	clickAt  time.Time
	clickX   int
	clickY   int
}

// composeBodyRect returns the on-screen position and size of the message body
// editor in terminal cells. It renders the compose view to find where the body
// starts, so it follows the real layout (recipient suggestions, the From row,
// and so on) rather than a hand-kept copy of it. ok is false when the body is
// not on screen (attachment picker, template browser).
func (m Model) composeBodyRect() (x, y, w, h int, ok bool) {
	c := m.compose
	if c.picker.active || c.tmplOpen {
		return 0, 0, 0, 0, false
	}
	winW, winH := c.overlayDimensions(m.width, m.height)
	_, bodyW, bodyH, _ := c.composeLayout(winW, winH, m.styles)
	_, bodyTop := c.viewWithBodyTop(winW, winH, m.styles)
	if bodyTop < 0 {
		return 0, 0, 0, 0, false
	}
	// renderSoftPanelBox adds a title row on top and a border on each side. A
	// compact box is centered; a maximized one fills the screen from the origin.
	boxX, boxY := 0, 0
	if !c.maximized {
		boxX = max(0, (m.width-(winW+2))/2)
		boxY = max(0, (m.height-(winH+2))/2)
	}
	// The body is inset 2 cells from the panel edge (bodyRow's padding).
	return boxX + 1 + 2, boxY + 1 + bodyTop, bodyW, bodyH, true
}

// focusBodyField moves compose focus to the message body, as Tab would.
func (c *ComposeModel) focusBodyField() {
	if c.focusedField == composeFieldBody {
		return
	}
	c.focusedField = composeFieldBody
	c.toInput.Blur()
	c.ccInput.Blur()
	c.bccInput.Blur()
	c.subjectInput.Blur()
	c.bodyInput.Focus()
	c.bodyInput.EnterInsert()
}

// handleComposeMouse maps mouse events on the open compose overlay to the body
// editor: click places the caret, drag selects, double-click selects a word,
// and the wheel scrolls. Events outside the body are ignored.
func (m Model) handleComposeMouse(msg tea.MouseMsg, now time.Time) (tea.Model, tea.Cmd) {
	c := &m.compose
	if c.senderPickerOpen {
		return m, nil
	}
	x0, y0, w, h, ok := m.composeBodyRect()
	if !ok {
		return m, nil
	}
	row, col := msg.Y-y0, msg.X-x0
	inside := row >= 0 && row < h && col >= 0 && col < w
	ed := &c.bodyInput.ed

	switch {
	case tea.MouseEvent(msg).IsWheel():
		if !inside {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			ed.ScrollRows(-wheelStepRows)
		case tea.MouseButtonWheelDown:
			ed.ScrollRows(wheelStepRows)
		}

	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		c.bodyMouse.dragging = false
		if !inside {
			return m, nil
		}
		c.focusBodyField()
		double := !c.bodyMouse.clickAt.IsZero() && now.Sub(c.bodyMouse.clickAt) <= composeDoubleClick &&
			c.bodyMouse.clickX == msg.X && c.bodyMouse.clickY == msg.Y
		if double {
			ed.SelectWordAt(row, col)
			c.bodyMouse.clickAt = time.Time{}
			return m, nil
		}
		ed.PlaceCursor(row, col)
		c.bodyMouse = composeMouseState{dragging: true, clickAt: now, clickX: msg.X, clickY: msg.Y}

	case msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft && c.bodyMouse.dragging:
		// Dragging past the top or bottom edge scrolls one row per motion event
		// and keeps extending the selection; sideways overflow clamps to the edge.
		if row < 0 {
			ed.ScrollRows(-1)
		} else if row >= h {
			ed.ScrollRows(1)
		}
		ed.ExtendSelectionTo(clamp(row, 0, h-1), clamp(col, 0, w-1))

	case msg.Action == tea.MouseActionRelease:
		c.bodyMouse.dragging = false
	}
	return m, nil
}
