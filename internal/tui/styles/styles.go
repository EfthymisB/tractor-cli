// Package styles defines all lipgloss colours and styles for tractor-cli.
package styles

import "github.com/charmbracelet/lipgloss"

var (
	// StatusBar is the fixed bottom bar.
	StatusBar = lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(lipgloss.Color("250"))

	// ErrorStyle is used for error text anywhere in the UI.
	ErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	// Bold title / heading text.
	Title = lipgloss.NewStyle().Bold(true)

	// FilterBorder wraps the active filter textinput.
	FilterBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("86")).
			Padding(0, 1)

	// TableHeaderStyle is applied to the bubbles/table header row.
	TableHeaderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("240")).
				BorderBottom(true).
				Bold(true)

	// TableSelectedStyle is applied to the highlighted table row.
	TableSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("229")).
				Background(lipgloss.Color("57"))
)

// StatusColor returns a pre-built style for the given Tractor status string.
func StatusColor(status string) lipgloss.Style {
	switch status {
	case "active":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	case "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	case "done":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	case "blocked":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	case "paused":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	}
}
