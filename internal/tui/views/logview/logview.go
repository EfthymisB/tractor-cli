// Package logview renders a scrollable, searchable task log.
package logview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
	"github.com/tractor-cli/tractor-cli/internal/tui/styles"
)

type logLoadedMsg struct {
	jid, tid int
	text     string
}
type logErrMsg struct {
	jid, tid int
	err      error
}

// Model is the task log viewer.
type Model struct {
	viewport viewport.Model
	spinner  spinner.Model
	search   textinput.Model

	jid, tid int
	lines    []string
	matches  []int // indices of lines matching the active query
	matchPos int
	query    string

	searching bool
	loading   bool
	err       error
	ready     bool

	client *api.Client
	cfg    *config.Config
	width  int
	height int
}

// New creates a log viewer model.
func New(client *api.Client, cfg *config.Config) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

	ti := textinput.New()
	ti.Placeholder = "search…"
	ti.Prompt = ""
	ti.CharLimit = 128

	return Model{spinner: sp, search: ti, client: client, cfg: cfg}
}

// Load points the viewer at a task and fetches its log.
func (m Model) Load(jid, tid int, owner string) (Model, tea.Cmd) {
	m.jid = jid
	m.tid = tid
	m.lines = nil
	m.matches = nil
	m.matchPos = 0
	m.query = ""
	m.searching = false
	m.search.SetValue("")
	m.search.Blur()
	m.loading = true
	m.err = nil
	if m.ready {
		m.viewport.SetContent("")
		m.viewport.GotoTop()
	}
	return m, tea.Batch(m.fetch(owner), m.spinner.Tick)
}

func (m Model) fetch(owner string) tea.Cmd {
	jid, tid := m.jid, m.tid
	return func() tea.Msg {
		text, err := m.client.TaskLog(jid, tid, owner)
		if err != nil {
			return logErrMsg{jid: jid, tid: tid, err: err}
		}
		return logLoadedMsg{jid: jid, tid: tid, text: text}
	}
}

// CapturingInput reports whether the search input is consuming keys.
func (m Model) CapturingInput() bool { return m.searching }

// JID and TID identify the task being shown.
func (m Model) JID() int { return m.jid }
func (m Model) TID() int { return m.tid }

// SetSize updates dimensions and (re)initialises the viewport.
func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	if !m.ready {
		m.viewport = viewport.New(w, m.viewportHeight())
		m.ready = true
	} else {
		m.viewport.Width = w
		m.viewport.Height = m.viewportHeight()
	}
	m.viewport.SetContent(m.renderContent())
	return m
}

func (m Model) viewportHeight() int {
	h := m.height - 1 // header line
	if m.searching {
		h -= 3 // bordered search line
	}
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) syncViewportHeight() {
	if m.ready {
		m.viewport.Height = m.viewportHeight()
	}
}

// Update handles messages for the log viewer.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case logLoadedMsg:
		if msg.jid != m.jid || msg.tid != m.tid {
			return m, nil
		}
		m.loading = false
		m.err = nil
		text := strings.ReplaceAll(msg.text, "\r\n", "\n")
		m.lines = strings.Split(text, "\n")
		if m.ready {
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoBottom() // show the most recent output
		}
		return m, nil

	case logErrMsg:
		if msg.jid == m.jid && msg.tid == m.tid {
			m.loading = false
			m.err = msg.err
			if m.ready {
				m.viewport.SetContent(m.renderContent())
			}
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		if m.loading && m.ready {
			m.viewport.SetContent(m.renderContent())
		}

	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "enter":
				m.applySearch()
				m.searching = false
				m.search.Blur()
				m.syncViewportHeight()
				return m, nil
			case "esc":
				m.searching = false
				m.search.Blur()
				m.syncViewportHeight()
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "/":
			m.searching = true
			m.syncViewportHeight()
			return m, m.search.Focus()
		case "n":
			m.nextMatch(1)
			return m, nil
		case "N":
			m.nextMatch(-1)
			return m, nil
		case "g":
			m.viewport.GotoTop()
			return m, nil
		case "G":
			m.viewport.GotoBottom()
			return m, nil
		}
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)
	return m, tea.Batch(cmds...)
}

func (m *Model) applySearch() {
	m.query = strings.TrimSpace(m.search.Value())
	m.matches = nil
	m.matchPos = 0
	if m.query != "" {
		q := strings.ToLower(m.query)
		for i, ln := range m.lines {
			if strings.Contains(strings.ToLower(ln), q) {
				m.matches = append(m.matches, i)
			}
		}
	}
	if m.ready {
		m.viewport.SetContent(m.renderContent())
	}
	if len(m.matches) > 0 {
		m.jumpTo(m.matches[0])
	}
}

func (m *Model) nextMatch(dir int) {
	if len(m.matches) == 0 {
		return
	}
	m.matchPos = (m.matchPos + dir + len(m.matches)) % len(m.matches)
	if m.ready {
		m.viewport.SetContent(m.renderContent())
	}
	m.jumpTo(m.matches[m.matchPos])
}

func (m *Model) jumpTo(line int) {
	if !m.ready {
		return
	}
	off := line - m.viewport.Height/2
	if off < 0 {
		off = 0
	}
	m.viewport.SetYOffset(off)
}

// View renders the log viewer.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	header := m.renderHeader()
	if !m.ready {
		return header
	}
	out := header + "\n"
	if m.searching {
		out += styles.FilterBorder.Width(m.width-4).Render("Search: "+m.search.View()) + "\n"
	}
	return out + m.viewport.View()
}

func (m Model) renderHeader() string {
	title := fmt.Sprintf("Log — Job %d / Task %d", m.jid, m.tid)
	if m.loading {
		title += "  " + m.spinner.View()
	} else if m.query != "" {
		if len(m.matches) == 0 {
			title += fmt.Sprintf("   no match for %q", m.query)
		} else {
			title += fmt.Sprintf("   match %d/%d for %q", m.matchPos+1, len(m.matches), m.query)
		}
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render("   [Esc] back  [/] search  [n/N] next/prev  [g/G] top/bottom")
	return styles.Title.Render(title) + hint
}

func (m Model) renderContent() string {
	if m.loading {
		return "Loading log… " + m.spinner.View()
	}
	if m.err != nil {
		return styles.ErrorStyle.Render("Error: " + m.err.Error())
	}
	if len(m.lines) == 0 || (len(m.lines) == 1 && m.lines[0] == "") {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(no log output)")
	}

	matchSet := make(map[int]bool, len(m.matches))
	for _, i := range m.matches {
		matchSet[i] = true
	}
	cur := -1
	if len(m.matches) > 0 {
		cur = m.matches[m.matchPos]
	}

	var b strings.Builder
	for i, ln := range m.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(styleLogLine(ln, matchSet[i], i == cur))
	}
	return b.String()
}

// styleLogLine colours a log line by severity keyword and highlights search
// matches (the current match more strongly).
func styleLogLine(line string, matched, current bool) string {
	st := lipgloss.NewStyle()
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "error"), strings.Contains(low, "traceback"),
		strings.Contains(low, "fatal"), strings.Contains(low, "exception"):
		st = st.Foreground(lipgloss.Color("9"))
	case strings.Contains(low, "warning"), strings.Contains(low, "warn"):
		st = st.Foreground(lipgloss.Color("11"))
	}
	switch {
	case current:
		st = st.Background(lipgloss.Color("58")).Bold(true)
	case matched:
		st = st.Background(lipgloss.Color("236"))
	}
	return st.Render(line)
}
