package ui

import "github.com/charmbracelet/lipgloss"

type folderColorOption struct {
	Name  string
	Color lipgloss.Color
}

var folderColorOptions = []folderColorOption{
	{Name: "Blue", Color: "#7aa2f7"},
	{Name: "Green", Color: "#9ece6a"},
	{Name: "Gold", Color: "#e0af68"},
	{Name: "Rose", Color: "#f7768e"},
	{Name: "Mauve", Color: "#bb9af7"},
	{Name: "Teal", Color: "#73daca"},
	{Name: "Peach", Color: "#ff9e64"},
	{Name: "Red", Color: "#f44747"},
}

func folderColorByValue(color string) (folderColorOption, int, bool) {
	for i, option := range folderColorOptions {
		if string(option.Color) == color {
			return option, i, true
		}
	}
	return folderColorOption{}, 0, false
}
