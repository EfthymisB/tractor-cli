// Package action provides the job/task action menu with an optional value-input
// step and a confirmation step for destructive operations.
package action

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Target identifies what an action operates on.
type Target struct {
	Kind   string // "job" or "task"
	JID    int
	TID    int
	Label  string // human label, e.g. "Job 123 — comp"
	Paused bool   // job paused state (for pause/unpause)
}

// Item is one selectable action.
type Item struct {
	Label       string
	Op          string
	Destructive bool
	InputPrompt string // non-empty → prompt for a value first
}

// ChosenMsg is emitted when an action is confirmed.
type ChosenMsg struct {
	Target Target
	Op     string
	Value  string
}

// CancelMsg is emitted when the menu is dismissed.
type CancelMsg struct{}

type mode int

const (
	modeMenu mode = iota
	modeInput
	modeConfirm
)

// Model is the action menu.
type Model struct {
	active bool
	target Target
	items  []Item
	cursor int
	mode   mode
	input  textinput.Model
	value  string // captured input value, pending confirmation
}

// New returns an inactive action menu.
func New() Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 64
	return Model{input: ti}
}

// Active reports whether the menu is open.
func (m Model) Active() bool { return m.active }

// OpenJob opens the menu with job-level actions.
func (m Model) OpenJob(jid int, label string, paused bool) Model {
	m.target = Target{Kind: "job", JID: jid, Label: label, Paused: paused}
	m.items = jobItems(paused)
	return m.reset()
}

// OpenTask opens the menu with task-level actions.
func (m Model) OpenTask(jid, tid int, label string) Model {
	m.target = Target{Kind: "task", JID: jid, TID: tid, Label: label}
	m.items = taskItems()
	return m.reset()
}

func (m Model) reset() Model {
	m.active = true
	m.cursor = 0
	m.mode = modeMenu
	m.value = ""
	m.input.SetValue("")
	m.input.Blur()
	return m
}

func jobItems(paused bool) []Item {
	pause := Item{Label: "Pause", Op: "pause"}
	if paused {
		pause = Item{Label: "Unpause", Op: "unpause"}
	}
	return []Item{
		pause,
		{Label: "Retry errored tasks", Op: "retry-errors"},
		{Label: "Retry active tasks", Op: "retry-active"},
		{Label: "Skip errored tasks", Op: "skip-errors"},
		{Label: "Set priority…", Op: "chpri", InputPrompt: "New priority"},
		{Label: "Interrupt (kill active)", Op: "interrupt", Destructive: true},
		{Label: "Restart job", Op: "restart", Destructive: true},
		{Label: "Delete job", Op: "delete", Destructive: true},
	}
}

func taskItems() []Item {
	return []Item{
		{Label: "Retry", Op: "retry"},
		{Label: "Resume", Op: "resume"},
		{Label: "Skip", Op: "skip"},
		{Label: "Kill", Op: "kill", Destructive: true},
	}
}

// Update handles key events while the menu is open.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		if m.mode == modeInput {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	switch m.mode {
	case modeInput:
		return m.updateInput(key)
	case modeConfirm:
		return m.updateConfirm(key)
	default:
		return m.updateMenu(key)
	}
}

func (m Model) updateMenu(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "esc", "a", "q":
		return m.cancel()
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter":
		return m.choose()
	}
	return m, nil
}

func (m Model) choose() (Model, tea.Cmd) {
	it := m.items[m.cursor]
	if it.InputPrompt != "" {
		m.mode = modeInput
		m.input.Placeholder = it.InputPrompt
		m.input.SetValue("")
		return m, m.input.Focus()
	}
	if it.Destructive {
		m.mode = modeConfirm
		return m, nil
	}
	return m.emit("")
}

func (m Model) updateInput(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.mode = modeMenu
		m.input.Blur()
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.input.Value())
		if val == "" {
			return m, nil // require a value
		}
		m.input.Blur()
		if m.items[m.cursor].Destructive {
			m.value = val
			m.mode = modeConfirm
			return m, nil
		}
		return m.emit(val)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

func (m Model) updateConfirm(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "y", "Y", "enter":
		return m.emit(m.value)
	case "n", "N", "esc":
		m.mode = modeMenu
		return m, nil
	}
	return m, nil
}

func (m Model) cancel() (Model, tea.Cmd) {
	m.active = false
	return m, func() tea.Msg { return CancelMsg{} }
}

func (m Model) emit(value string) (Model, tea.Cmd) {
	op := m.items[m.cursor].Op
	target := m.target
	m.active = false
	m.mode = modeMenu
	m.input.Blur()
	return m, func() tea.Msg { return ChosenMsg{Target: target, Op: op, Value: value} }
}

// View renders the menu centred in the given area.
func (m Model) View(width, height int) string {
	if !m.active {
		return ""
	}
	var content string
	switch m.mode {
	case modeInput:
		content = m.inputView()
	case modeConfirm:
		content = m.confirmView()
	default:
		content = m.menuView()
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 3).
		Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) menuView() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).
		Render("Actions — "+m.target.Label) + "\n\n")
	for i, it := range m.items {
		cursor := "  "
		st := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
		if it.Destructive {
			st = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		}
		if i == m.cursor {
			cursor = "› "
			st = st.Bold(true).Background(lipgloss.Color("236"))
		}
		b.WriteString(cursor + st.Render(it.Label))
		if i < len(m.items)-1 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n\n" + dim("↑/↓ move   Enter select   Esc cancel"))
	return b.String()
}

func (m Model) inputView() string {
	it := m.items[m.cursor]
	return lipgloss.NewStyle().Bold(true).Render(it.Label) + "\n\n" +
		it.InputPrompt + ": " + m.input.View() + "\n\n" +
		dim("Enter confirm   Esc back")
}

func (m Model) confirmView() string {
	it := m.items[m.cursor]
	q := fmt.Sprintf("%s — %s?", it.Label, m.target.Label)
	if m.value != "" {
		q = fmt.Sprintf("%s (%s) — %s?", it.Label, m.value, m.target.Label)
	}
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render(q)
	return warn + "\n\n" + dim("y confirm   n / Esc cancel")
}

func dim(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(s)
}
