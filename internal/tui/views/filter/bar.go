// Package filter provides the search/filter bar component.
package filter

import (
	"strconv"
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
	ti.Placeholder = "owner:a,b  jid:123,100-200  prio:1000  projects:cc  status:error  title:t"
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

// parseFilter parses a filter expression string into a JobFilter.
//
// Syntax:
//
//	owner:alice,bob         — match jobs owned by alice OR bob
//	jid:2207510             — exact JID match
//	jid:2207510,2207511     — multiple JIDs (OR)
//	jid:2207510-2207520     — inclusive JID range
//	jid:100,200-300         — mixed specific JIDs and a range
//	status:error            — exact status
//	title:comp              — case-insensitive substring on title
//	prio:1000               — exact priority match
//	projects:cc,thornfield  — job must contain cc OR thornfield
func parseFilter(s string) api.JobFilter {
	f := api.JobFilter{}
	for _, part := range strings.Fields(s) {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(kv[0])
		val := kv[1]
		switch key {
		case "owner":
			f.Owners = splitCSV(val)
		case "jid":
			parseJIDFilter(val, &f)
		case "status":
			f.Status = val
		case "title":
			f.Title = val
		case "prio", "priority":
			if p, err := strconv.ParseFloat(val, 64); err == nil {
				f.Priority = p
			}
		case "projects", "project":
			f.Projects = splitCSV(val)
		}
	}
	return f
}

// splitCSV splits a comma-separated string, trimming whitespace and dropping
// empty elements.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseJIDFilter parses the value side of a jid: token.  Each comma-separated
// item may be a single integer ("2207510") or an inclusive integer range
// ("2207510-2207520").  Mixed forms are supported: "100,200-300,400".
// Only one range is tracked; a second range overwrites the first.
func parseJIDFilter(val string, f *api.JobFilter) {
	for _, item := range strings.Split(val, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		// Try range: "N-M" where the dash is not the very first character
		// (avoids treating negative numbers as ranges).
		if dashIdx := strings.Index(item, "-"); dashIdx > 0 {
			fromStr := item[:dashIdx]
			toStr := item[dashIdx+1:]
			from, err1 := strconv.Atoi(fromStr)
			to, err2 := strconv.Atoi(toStr)
			if err1 == nil && err2 == nil && from > 0 && to >= from {
				f.JIDFrom = from
				f.JIDTo = to
				continue
			}
		}
		// Fall back to a single JID.
		if jid, err := strconv.Atoi(item); err == nil && jid > 0 {
			f.JIDs = append(f.JIDs, jid)
		}
	}
}
