// Package filter provides the search/filter bar component.
package filter

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/tui/styles"
)

// SubmitMsg is sent when the user presses Enter to apply the filter.
type SubmitMsg struct{ Filter api.JobFilter }

// CancelMsg is sent when the user presses Esc to dismiss the filter bar.
type CancelMsg struct{}

// Model is the filter bar component.
type Model struct {
	input  textinput.Model
	active bool
}

// New returns an initialised filter bar (inactive by default).
func New() Model {
	ti := textinput.New()
	ti.Placeholder = "owner:name  status:error  title:comp"
	ti.CharLimit = 256
	return Model{input: ti}
}

// Active reports whether the filter bar is currently visible.
func (m Model) Active() bool { return m.active }

// Open makes the filter bar visible and focuses the input.
func (m Model) Open() (Model, tea.Cmd) {
	m.active = true
	m.input.SetValue("")
	return m, m.input.Focus()
}

// Update handles key events while the filter bar is active.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			f := parseFilter(m.input.Value())
			m.active = false
			m.input.Blur()
			return m, func() tea.Msg { return SubmitMsg{Filter: f} }
		case "esc":
			m.active = false
			m.input.Blur()
			return m, func() tea.Msg { return CancelMsg{} }
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View renders the filter bar at the given width. Returns "" when inactive.
func (m Model) View(width int) string {
	if !m.active {
		return ""
	}
	innerWidth := width - 6 // border (2) + padding (2) + "Filter: " label offset
	if innerWidth < 10 {
		innerWidth = 10
	}
	m.input.Width = innerWidth
	return styles.FilterBorder.Width(width - 4).Render("Filter: " + m.input.View())
}

// parseFilter turns "owner:x status:y title:z" into a JobFilter.
func parseFilter(s string) api.JobFilter {
	f := api.JobFilter{}
	for _, part := range strings.Fields(s) {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		switch strings.ToLower(kv[0]) {
		case "owner":
			f.Owner = kv[1]
		case "status":
			f.Status = kv[1]
		case "title":
			f.Title = kv[1]
		}
	}
	return f
}
