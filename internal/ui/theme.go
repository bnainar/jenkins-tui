package ui

import "github.com/charmbracelet/lipgloss"

var (
	AppBorder = lipgloss.NewStyle()

	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230"))

	Muted = lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

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
		Foreground(lipgloss.Color("246"))
)
