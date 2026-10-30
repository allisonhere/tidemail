package ui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

var readAnimationFrames = spinner.MiniDot.Frames

const readAnimationInterval = 250 * time.Millisecond

type readAnimationTickMsg struct{ Generation uint64 }

func readAnimationTick(generation uint64) tea.Cmd {
	return tea.Tick(readAnimationInterval, func(time.Time) tea.Msg { return readAnimationTickMsg{Generation: generation} })
}
