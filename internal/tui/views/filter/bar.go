// Package filter provides the multi-field search/filter form component.
package filter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/tui/styles"
)

// SubmitMsg is sent when the user presses Enter to apply the filter.
type SubmitMsg struct{ Filter api.JobFilter }

// CancelMsg is sent when the user presses Esc to dismiss the filter form.
type CancelMsg struct{}

// field indices (also the Tab order)
const (
	fieldOwner = iota
	fieldStatus
	fieldJID
	fieldPrio
	fieldProjects
	fieldTitle
	numFields
)

// labelWidth is the fixed column width for field labels.
const labelWidth = 11

// maxOwnerMatches caps the autocomplete dropdown height.
const maxOwnerMatches = 6

// allStatuses lists every selectable job status, in display order.
var allStatuses = []string{"active", "error", "blocked", "paused", "done"}

// statusColor maps a status to its lipgloss colour code.
var statusColor = map[string]string{
	"active":  "10",
	"error":   "9",
	"blocked": "11",
	"paused":  "14",
	"done":    "240",
}

// Model is the multi-field filter form.
type Model struct {
	active bool
	focus  int

	owner    textinput.Model
	jid      textinput.Model
	prio     textinput.Model
	projects textinput.Model
	title    textinput.Model

	statusSel map[string]bool // selected statuses
	statusCur int             // sub-cursor among status chips

	usernames []string // owner autocomplete source (getent passwd)
	acIndex   int      // highlighted autocomplete match

	width int
}

// New returns an initialised, inactive filter form.
func New() Model {
	mk := func(placeholder string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.Prompt = ""
		ti.CharLimit = 256
		return ti
	}
	return Model{
		owner:     mk("alice,bob"),
		jid:       mk("2207510,2207600-2207620"),
		prio:      mk("1000"),
		projects:  mk("cc,thornfield"),
		title:     mk("comp"),
		statusSel: make(map[string]bool),
	}
}

// Active reports whether the form is currently visible.
func (m Model) Active() bool { return m.active }

// SetUsernames sets the autocomplete source for the owner field. The form
// renders its own dropdown (see renderDropdown), so we don't enable bubbles'
// built-in inline suggestions here.
func (m Model) SetUsernames(u []string) Model {
	m.usernames = u
	return m
}

// Open makes the form visible, pre-populating every field from cur so existing
// filters can be amended rather than re-entered.
func (m Model) Open(cur api.JobFilter) (Model, tea.Cmd) {
	m.active = true
	m.focus = fieldOwner
	m.acIndex = 0
	m.statusCur = 0

	m.owner.SetValue(strings.Join(cur.Owners, ","))
	m.jid.SetValue(jidFilterToString(cur))
	if cur.Priority != 0 {
		m.prio.SetValue(strconv.FormatFloat(cur.Priority, 'g', -1, 64))
	} else {
		m.prio.SetValue("")
	}
	m.projects.SetValue(strings.Join(cur.Projects, ","))
	m.title.SetValue(cur.Title)

	m.statusSel = make(map[string]bool)
	for _, s := range cur.Statuses {
		m.statusSel[s] = true
	}

	return m.focusField(fieldOwner)
}

// Update handles key events while the form is active.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m.updateFocusedInput(msg)
	}

	dropdown := m.ownerDropdownOpen()

	switch key.String() {
	case "esc":
		m.active = false
		m.blurAll()
		return m, func() tea.Msg { return CancelMsg{} }

	case "enter":
		m.active = false
		m.blurAll()
		f := m.buildFilter()
		return m, func() tea.Msg { return SubmitMsg{Filter: f} }

	case "tab":
		if dropdown {
			m = m.acceptOwnerMatch()
			return m, nil
		}
		return m.focusField(m.nextField())

	case "shift+tab":
		return m.focusField(m.prevField())

	case "down":
		if dropdown {
			m.acIndex++
			m = m.clampAC()
			return m, nil
		}
		return m.focusField(m.nextField())

	case "up":
		if dropdown {
			m.acIndex--
			m = m.clampAC()
			return m, nil
		}
		return m.focusField(m.prevField())

	case "right":
		if dropdown {
			m = m.acceptOwnerMatch()
			return m, nil
		}
		// no dropdown: exit switch so a text field gets the cursor move,
		// or status navigation is handled in the post-switch block
	case "left":
		if m.focus == fieldStatus {
			if m.statusCur > 0 {
				m.statusCur--
			}
			return m, nil
		}
	case " ", "space":
		if m.focus == fieldStatus {
			s := allStatuses[m.statusCur]
			m.statusSel[s] = !m.statusSel[s]
			return m, nil
		}
	}

	// status field horizontal navigation (right handled here when no dropdown)
	if m.focus == fieldStatus {
		if key.String() == "right" && m.statusCur < len(allStatuses)-1 {
			m.statusCur++
		}
		return m, nil
	}

	return m.updateFocusedInput(msg)
}

// updateFocusedInput routes a message to the currently focused text input.
func (m Model) updateFocusedInput(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focus {
	case fieldOwner:
		m.owner, cmd = m.owner.Update(msg)
	case fieldJID:
		m.jid, cmd = m.jid.Update(msg)
	case fieldPrio:
		m.prio, cmd = m.prio.Update(msg)
	case fieldProjects:
		m.projects, cmd = m.projects.Update(msg)
	case fieldTitle:
		m.title, cmd = m.title.Update(msg)
	}
	return m, cmd
}

// View renders the form at the given width. Returns "" when inactive.
func (m Model) View(width int) string {
	if !m.active {
		return ""
	}
	m.width = width
	bodyWidth := width - 4 - labelWidth
	if bodyWidth < 10 {
		bodyWidth = 10
	}
	m.owner.Width = bodyWidth
	m.jid.Width = bodyWidth
	m.prio.Width = bodyWidth
	m.projects.Width = bodyWidth
	m.title.Width = bodyWidth

	var b strings.Builder
	b.WriteString(m.row("Owner", fieldOwner, m.owner.View()))
	if m.ownerDropdownOpen() {
		b.WriteString("\n" + m.renderDropdown())
	}
	b.WriteString("\n" + m.row("Status", fieldStatus, m.renderStatus()))
	b.WriteString("\n" + m.row("JID", fieldJID, m.jid.View()))
	b.WriteString("\n" + m.row("Priority", fieldPrio, m.prio.View()))
	b.WriteString("\n" + m.row("Projects", fieldProjects, m.projects.View()))
	b.WriteString("\n" + m.row("Title", fieldTitle, m.title.View()))
	b.WriteString("\n" + m.helpLine())

	return styles.FilterBorder.Width(width - 4).Render(b.String())
}

// Height returns the number of terminal lines View occupies, for layout maths.
func (m Model) Height() int {
	if !m.active {
		return 0
	}
	lines := numFields + 1 // field rows + help row
	if m.ownerDropdownOpen() {
		lines += len(m.ownerMatches())
	}
	return lines + 2 // rounded border top + bottom
}

// --- rendering helpers ---

func (m Model) row(label string, field int, body string) string {
	lbl := fmt.Sprintf("%-*s", labelWidth, label+":")
	st := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	if m.focus == field {
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
		lbl = "› " + fmt.Sprintf("%-*s", labelWidth-2, label+":")
	}
	return st.Render(lbl) + body
}

func (m Model) renderStatus() string {
	parts := make([]string, len(allStatuses))
	for i, s := range allStatuses {
		mark := "○"
		if m.statusSel[s] {
			mark = "●"
		}
		st := lipgloss.NewStyle()
		if m.statusSel[s] {
			st = st.Foreground(lipgloss.Color(statusColor[s])).Bold(true)
		} else {
			st = st.Foreground(lipgloss.Color("244"))
		}
		if m.focus == fieldStatus && i == m.statusCur {
			st = st.Reverse(true)
		}
		parts[i] = st.Render(mark + " " + s)
	}
	return strings.Join(parts, "  ")
}

func (m Model) renderDropdown() string {
	matches := m.ownerMatches()
	pad := strings.Repeat(" ", labelWidth+2)
	lines := make([]string, len(matches))
	for i, mm := range matches {
		st := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
		if i == m.acIndex {
			st = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).
				Background(lipgloss.Color("57"))
		}
		lines[i] = pad + st.Render(" "+mm+" ")
	}
	return strings.Join(lines, "\n")
}

func (m Model) helpLine() string {
	help := "[Tab] next  [⇧Tab] prev  [Space] toggle  [→] accept  [Enter] apply  [Esc] cancel"
	return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(help)
}

// --- focus management ---

func (m Model) focusField(i int) (Model, tea.Cmd) {
	m.blurAll()
	m.focus = i
	m.acIndex = 0
	var cmd tea.Cmd
	switch i {
	case fieldOwner:
		cmd = m.owner.Focus()
	case fieldJID:
		cmd = m.jid.Focus()
	case fieldPrio:
		cmd = m.prio.Focus()
	case fieldProjects:
		cmd = m.projects.Focus()
	case fieldTitle:
		cmd = m.title.Focus()
	}
	return m, cmd
}

func (m *Model) blurAll() {
	m.owner.Blur()
	m.jid.Blur()
	m.prio.Blur()
	m.projects.Blur()
	m.title.Blur()
}

func (m Model) nextField() int { return (m.focus + 1) % numFields }
func (m Model) prevField() int { return (m.focus - 1 + numFields) % numFields }

// --- owner autocomplete ---

func (m Model) ownerDropdownOpen() bool {
	return m.active && m.focus == fieldOwner && len(m.ownerMatches()) > 0
}

// ownerMatches returns up to maxOwnerMatches usernames that prefix-match the
// segment after the last comma in the owner input.
func (m Model) ownerMatches() []string {
	seg := lastSegment(m.owner.Value())
	if seg == "" {
		return nil
	}
	segL := strings.ToLower(seg)
	var out []string
	for _, u := range m.usernames {
		if strings.HasPrefix(strings.ToLower(u), segL) {
			out = append(out, u)
			if len(out) >= maxOwnerMatches {
				break
			}
		}
	}
	// Nothing useful to suggest if the only match is already fully typed.
	if len(out) == 1 && strings.EqualFold(out[0], seg) {
		return nil
	}
	return out
}

func (m Model) acceptOwnerMatch() Model {
	matches := m.ownerMatches()
	if len(matches) == 0 {
		return m
	}
	idx := m.acIndex
	if idx < 0 || idx >= len(matches) {
		idx = 0
	}
	val := m.owner.Value()
	prefix := ""
	if c := strings.LastIndex(val, ","); c >= 0 {
		prefix = val[:c+1]
	}
	m.owner.SetValue(prefix + matches[idx])
	m.owner.CursorEnd()
	m.acIndex = 0
	return m
}

func (m Model) clampAC() Model {
	n := len(m.ownerMatches())
	if n == 0 {
		m.acIndex = 0
		return m
	}
	if m.acIndex < 0 {
		m.acIndex = 0
	}
	if m.acIndex >= n {
		m.acIndex = n - 1
	}
	return m
}

// --- filter construction ---

func (m Model) buildFilter() api.JobFilter {
	f := api.JobFilter{}
	f.Owners = splitCSV(m.owner.Value())
	f.Projects = splitCSV(m.projects.Value())
	if t := strings.TrimSpace(m.title.Value()); t != "" {
		f.Title = t
	}
	if p, err := strconv.ParseFloat(strings.TrimSpace(m.prio.Value()), 64); err == nil {
		f.Priority = p
	}
	parseJIDFilter(m.jid.Value(), &f)
	for _, s := range allStatuses {
		if m.statusSel[s] {
			f.Statuses = append(f.Statuses, s)
		}
	}
	return f
}

// --- shared parsing helpers ---

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

// lastSegment returns the text after the last comma, trimmed.
func lastSegment(s string) string {
	if c := strings.LastIndex(s, ","); c >= 0 {
		s = s[c+1:]
	}
	return strings.TrimSpace(s)
}

// parseJIDFilter parses a jid token value into f. Each comma-separated item may
// be a single integer ("2207510") or an inclusive range ("2207510-2207520").
// Mixed forms are supported; only one range is tracked.
func parseJIDFilter(val string, f *api.JobFilter) {
	for _, item := range strings.Split(val, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if dashIdx := strings.Index(item, "-"); dashIdx > 0 {
			from, err1 := strconv.Atoi(item[:dashIdx])
			to, err2 := strconv.Atoi(item[dashIdx+1:])
			if err1 == nil && err2 == nil && from > 0 && to >= from {
				f.JIDFrom = from
				f.JIDTo = to
				continue
			}
		}
		if jid, err := strconv.Atoi(item); err == nil && jid > 0 {
			f.JIDs = append(f.JIDs, jid)
		}
	}
}

// jidFilterToString reconstructs a jid field string from a JobFilter.
func jidFilterToString(f api.JobFilter) string {
	parts := make([]string, 0, len(f.JIDs)+1)
	for _, id := range f.JIDs {
		parts = append(parts, strconv.Itoa(id))
	}
	if f.JIDFrom > 0 && f.JIDTo > 0 {
		parts = append(parts, fmt.Sprintf("%d-%d", f.JIDFrom, f.JIDTo))
	}
	return strings.Join(parts, ",")
}
