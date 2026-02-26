package ui

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var (
	AppBorder = lipgloss.NewStyle()

	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("252"))

	Muted = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	Success = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	Warn = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Bold(true)

	Danger = lipgloss.NewStyle().
		Foreground(lipgloss.Color("203")).
		Bold(true)

	Help = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
)

func FormTheme() *huh.Theme {
	t := huh.ThemeBase()
	t.Focused.Base = t.Focused.Base.BorderForeground(lipgloss.Color("238"))
	t.Focused.Title = t.Focused.Title.Foreground(lipgloss.Color("110")).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(lipgloss.Color("110")).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(lipgloss.Color("245"))
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(lipgloss.Color("110"))
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(lipgloss.Color("110"))
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(lipgloss.Color("110"))
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("110")).SetString("✓ ")
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).SetString("• ")
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Bold(true)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(lipgloss.Color("250")).Background(lipgloss.Color("238"))
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(lipgloss.Color("110"))
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(lipgloss.Color("110"))
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(lipgloss.Color("240"))
	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()
	return t
}
