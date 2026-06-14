// Package logview renders a scrollable, searchable task log.
package logview

import (
	"fmt"
	"regexp"
	"strconv"
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

	jid, tid  int
	owner     string
	jobTitle  string
	taskTitle string
	lines     []string
	matches   []int // original line indices matching the active query
	matchPos  int
	query     string

	wrap       bool  // wrap long lines vs. horizontal scroll
	xOffset    int   // horizontal scroll offset (when not wrapping)
	maxLineLen int   // longest line length, for clamping xOffset
	lineStart  []int // display-row index of each original line's first row

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

	return Model{spinner: sp, search: ti, client: client, cfg: cfg, wrap: true}
}

// Load points the viewer at a task and fetches its log.
func (m Model) Load(jid, tid int, owner, jobTitle, taskTitle string) (Model, tea.Cmd) {
	m.jid = jid
	m.tid = tid
	m.owner = owner
	m.jobTitle = jobTitle
	m.taskTitle = taskTitle
	m.lines = nil
	m.matches = nil
	m.matchPos = 0
	m.query = ""
	m.xOffset = 0
	m.maxLineLen = 0
	m.lineStart = nil
	m.searching = false
	m.search.SetValue("")
	m.search.Blur()
	m.loading = true
	m.err = nil
	if m.ready {
		m.viewport.SetContent("")
		m.viewport.GotoTop()
	}
	return m, tea.Batch(m.fetch(), m.spinner.Tick)
}

func (m Model) fetch() tea.Cmd {
	jid, tid, owner := m.jid, m.tid, m.owner
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

// RawText returns the full, unstyled log text.
func (m Model) RawText() string { return strings.Join(m.lines, "\n") }

// Accessors used by the root model's status bar.
func (m Model) JID() int          { return m.jid }
func (m Model) TID() int          { return m.tid }
func (m Model) JobTitle() string  { return m.jobTitle }
func (m Model) TaskTitle() string { return m.taskTitle }

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
	m.setContent()
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
		m.maxLineLen = 0
		for _, ln := range m.lines {
			if n := len([]rune(ln)); n > m.maxLineLen {
				m.maxLineLen = n
			}
		}
		if m.ready {
			m.setContent()
			m.viewport.GotoBottom() // show the most recent output
		}
		return m, nil

	case logErrMsg:
		if msg.jid == m.jid && msg.tid == m.tid {
			m.loading = false
			m.err = msg.err
			if m.ready {
				m.setContent()
			}
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		if m.loading && m.ready {
			m.setContent()
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
		case "c":
			// Clear the active search.
			m.query = ""
			m.matches = nil
			m.matchPos = 0
			if m.ready {
				m.setContent()
			}
			return m, nil
		case "R":
			// Re-fetch the current task's log.
			m.loading = true
			if m.ready {
				m.setContent()
			}
			return m, tea.Batch(m.fetch(), m.spinner.Tick)
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
		case "w":
			// Toggle line wrapping vs. horizontal scrolling.
			m.wrap = !m.wrap
			m.xOffset = 0
			if m.ready {
				m.setContent()
			}
			return m, nil
		case "left", "h":
			if !m.wrap {
				m.scrollH(-hScrollStep)
				return m, nil
			}
		case "right", "l":
			if !m.wrap {
				m.scrollH(hScrollStep)
				return m, nil
			}
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
		m.setContent()
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
		m.setContent()
	}
	m.jumpTo(m.matches[m.matchPos])
}

func (m *Model) jumpTo(line int) {
	if !m.ready {
		return
	}
	disp := line
	if line >= 0 && line < len(m.lineStart) {
		disp = m.lineStart[line] // map original line → display row
	}
	off := disp - m.viewport.Height/2
	if off < 0 {
		off = 0
	}
	m.viewport.SetYOffset(off)
}

// scrollH adjusts the horizontal offset (no-wrap mode), clamped so you can't
// scroll past the longest line.
func (m *Model) scrollH(delta int) {
	m.xOffset += delta
	if m.xOffset < 0 {
		m.xOffset = 0
	}
	if max := m.maxLineLen - 1; m.xOffset > max && max > 0 {
		m.xOffset = max
	}
	if m.ready {
		m.setContent()
	}
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
	title := fmt.Sprintf("Log — Job %d", m.jid)
	if m.jobTitle != "" {
		title += " (" + m.jobTitle + ")"
	}
	title += fmt.Sprintf("  Task %d", m.tid)
	if m.taskTitle != "" {
		title += " (" + m.taskTitle + ")"
	}
	if m.loading {
		title += "  " + m.spinner.View()
	} else if m.query != "" {
		if len(m.matches) == 0 {
			title += fmt.Sprintf("   no match for %q", m.query)
		} else {
			title += fmt.Sprintf("   match %d/%d for %q", m.matchPos+1, len(m.matches), m.query)
		}
	}
	wrapHint := "[w] wrap:on"
	if !m.wrap {
		wrapHint = "[w] wrap:off  [←/→] scroll"
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render("   [Esc] back  [/] search  [c] clear  [n/N] match  [[ ]] task  [R] refresh  " + wrapHint)
	return styles.Title.Render(title) + hint
}

// hScrollStep is how many columns a left/right press scrolls in no-wrap mode.
const hScrollStep = 12

// setContent rebuilds the viewport content and the original→display line map.
func (m *Model) setContent() {
	if m.loading {
		m.lineStart = nil
		m.viewport.SetContent("Loading log… " + m.spinner.View())
		return
	}
	if m.err != nil {
		m.lineStart = nil
		m.viewport.SetContent(styles.ErrorStyle.Render("Error: " + m.err.Error()))
		return
	}
	if len(m.lines) == 0 || (len(m.lines) == 1 && m.lines[0] == "") {
		m.lineStart = nil
		m.viewport.SetContent(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(no log output)"))
		return
	}

	matchSet := make(map[int]bool, len(m.matches))
	for _, i := range m.matches {
		matchSet[i] = true
	}
	cur := -1
	if len(m.matches) > 0 {
		cur = m.matches[m.matchPos]
	}

	// Line-number gutter: width of the largest number + a trailing space.
	numW := len(strconv.Itoa(len(m.lines)))
	gutterW := numW + 1
	contentW := m.viewport.Width - gutterW
	if contentW < 1 { // terminal too narrow for a gutter
		contentW = m.viewport.Width
		gutterW = 0
	}
	gutStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	lineStart := make([]int, len(m.lines))
	var display []string
	for i, raw := range m.lines {
		lineStart[i] = len(display)
		matched, isCur := matchSet[i], i == cur

		var segs []string
		if m.wrap {
			segs = hardWrap(raw, contentW)
		} else {
			segs = []string{sliceH(raw, m.xOffset, contentW)}
		}

		for j, seg := range segs {
			gutter := ""
			if gutterW > 0 {
				if j == 0 {
					gutter = gutStyle.Render(fmt.Sprintf("%*d ", numW, i+1))
				} else {
					gutter = strings.Repeat(" ", gutterW) // blank for wrapped rows
				}
			}
			display = append(display, gutter+styleLogLine(seg, matched, isCur))
		}
	}
	m.lineStart = lineStart
	m.viewport.SetContent(strings.Join(display, "\n"))
}

// hardWrap splits s into rune chunks of at most width (character wrapping, so
// no characters are lost on unbreakable tokens).
func hardWrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	r := []rune(s)
	if len(r) <= width {
		return []string{s}
	}
	var out []string
	for len(r) > width {
		out = append(out, string(r[:width]))
		r = r[width:]
	}
	return append(out, string(r))
}

// sliceH returns the width-wide horizontal window of s starting at off.
func sliceH(s string, off, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if off >= len(r) {
		return ""
	}
	end := off + width
	if end > len(r) {
		end = len(r)
	}
	return string(r[off:end])
}

// logToken matches the syntactic pieces we colour. Order of alternation sets
// precedence (timestamps before bare times/numbers, etc.).
var logToken = regexp.MustCompile(
	`(?P<ts>\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?)` + // 2024-01-02 15:04:05
		`|(?P<time>\b\d{1,2}:\d{2}:\d{2}\b)` + // 15:04:05
		`|(?P<level>(?i)\b(?:CRITICAL|CRIT|FATAL|ERROR|ERR|WARNING|WARN|NOTICE|INFO|DEBUG|TRACE)\b)` +
		`|(?P<url>https?://[^\s]+)` +
		`|(?P<str>"[^"]*"|'[^']*')` +
		`|(?P<path>(?:/[\w.+\-]+){2,}/?)` + // /abs/paths/like/this
		`|(?P<num>\b\d+(?:\.\d+)?\b)`,
)

var logTokenNames = logToken.SubexpNames()

// styleLogLine colours search matches (whole line) or, when not a match,
// applies per-token syntax highlighting.
func styleLogLine(line string, matched, current bool) string {
	switch {
	case current:
		return lipgloss.NewStyle().Background(lipgloss.Color("58")).Bold(true).Render(line)
	case matched:
		return lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(line)
	}
	return highlightLog(line)
}

// highlightLog applies VS Code-style token colouring to a single log line.
func highlightLog(line string) string {
	matches := logToken.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return line
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		if start < last {
			continue
		}
		b.WriteString(line[last:start])
		class := ""
		for g := 1; g < len(logTokenNames); g++ {
			if logTokenNames[g] != "" && m[2*g] >= 0 {
				class = logTokenNames[g]
				break
			}
		}
		b.WriteString(styleToken(class, line[start:end]))
		last = end
	}
	b.WriteString(line[last:])
	return b.String()
}

func styleToken(class, tok string) string {
	st := lipgloss.NewStyle()
	switch class {
	case "ts", "time":
		st = st.Foreground(lipgloss.Color("45")) // cyan
	case "level":
		switch strings.ToUpper(tok) {
		case "CRITICAL", "CRIT", "FATAL", "ERROR", "ERR":
			st = st.Foreground(lipgloss.Color("9")).Bold(true)
		case "WARNING", "WARN":
			st = st.Foreground(lipgloss.Color("11"))
		case "INFO", "NOTICE":
			st = st.Foreground(lipgloss.Color("10"))
		default: // DEBUG, TRACE
			st = st.Foreground(lipgloss.Color("245"))
		}
	case "url":
		st = st.Foreground(lipgloss.Color("39")).Underline(true)
	case "str":
		st = st.Foreground(lipgloss.Color("114")) // green
	case "path":
		st = st.Foreground(lipgloss.Color("39")) // blue
	case "num":
		st = st.Foreground(lipgloss.Color("213")) // magenta
	default:
		return tok
	}
	return st.Render(tok)
}
