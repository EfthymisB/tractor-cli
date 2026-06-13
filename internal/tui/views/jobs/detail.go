package jobs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
	"github.com/tractor-cli/tractor-cli/internal/models"
	"github.com/tractor-cli/tractor-cli/internal/tui/styles"
)

// async messages for the detail view
type tasksLoadedMsg struct {
	jid   int
	tasks []models.Task
}
type taskFetchErrMsg struct{ err error }

// detail column indices — must match the order in detailColumnNames/buildRows.
const (
	dcolTID      = 0
	dcolTitle    = 1
	dcolState    = 2
	dcolProgress = 3
	dcolRetry    = 4
	dcolService  = 5
	dcolActive   = 6
	dNumCols     = 7
)

var detailColumnNames = [dNumCols]string{
	"TID", "TITLE", "STATE", "PROG", "RETRY", "SERVICE", "ACTIVE",
}

var detailColumnCaps = [dNumCols]int{0, 0, 0, 0, 0, 24, 0}

// DetailModel is the per-job task list view (Phase 3).
type DetailModel struct {
	table   table.Model
	spinner spinner.Model

	jid      int
	jobTitle string
	tasks    []models.Task

	loading bool
	err     error
	topRow  int

	client *api.Client
	cfg    *config.Config
	width  int
	height int
}

// NewDetail creates a job-detail (task list) model.
func NewDetail(client *api.Client, cfg *config.Config) DetailModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	return DetailModel{
		table:   newTable(),
		spinner: sp,
		client:  client,
		cfg:     cfg,
	}
}

// Load points the detail view at a job and kicks off a task fetch.
func (m DetailModel) Load(jid int, title string) (DetailModel, tea.Cmd) {
	m.jid = jid
	m.jobTitle = title
	m.tasks = nil
	m.err = nil
	m.loading = true
	m.topRow = 0
	m.table.SetRows(nil)
	return m, tea.Batch(m.fetchTasks(), m.spinner.Tick)
}

// Refresh re-fetches the current job's tasks.
func (m DetailModel) Refresh() (DetailModel, tea.Cmd) {
	if m.jid == 0 {
		return m, nil
	}
	m.loading = true
	return m, m.fetchTasks()
}

func (m DetailModel) fetchTasks() tea.Cmd {
	jid := m.jid
	return func() tea.Msg {
		tasks, err := m.client.QueryTasks(jid)
		if err != nil {
			return taskFetchErrMsg{err}
		}
		return tasksLoadedMsg{jid: jid, tasks: tasks}
	}
}

// Update handles messages for the detail view.
func (m DetailModel) Update(msg tea.Msg) (DetailModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tasksLoadedMsg:
		if msg.jid != m.jid {
			return m, nil // stale load for a job we've navigated away from
		}
		m.loading = false
		m.err = nil
		m.tasks = msg.tasks
		m.topRow = 0
		m.table.SetColumns(m.makeColumns())
		m.table.SetRows(m.buildRows())

	case taskFetchErrMsg:
		m.loading = false
		m.err = msg.err

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		if msg.String() == "R" {
			var cmd tea.Cmd
			m, cmd = m.Refresh()
			return m, cmd
		}
	}

	var tableCmd tea.Cmd
	m.table, tableCmd = m.table.Update(msg)
	cmds = append(cmds, tableCmd)
	m = m.syncTopRow()

	return m, tea.Batch(cmds...)
}

// View renders the detail view.
func (m DetailModel) View() string {
	if m.width == 0 {
		return ""
	}
	return m.renderHeader() + "\n" + m.colorizeTasks(m.table.View())
}

// SetSize updates dimensions and resizes the table.
func (m DetailModel) SetSize(w, h int) DetailModel {
	m.width = w
	m.height = h
	m.table.SetColumns(m.makeColumns())
	m.table.SetHeight(m.viewportHeight())
	return m
}

// State accessors used by the root model.
func (m DetailModel) JID() int          { return m.jid }
func (m DetailModel) TaskCount() int     { return len(m.tasks) }
func (m DetailModel) IsLoading() bool    { return m.loading }
func (m DetailModel) Err() error         { return m.err }

// SelectedTID returns the task id of the highlighted row, or 0.
func (m DetailModel) SelectedTID() int {
	row := m.table.SelectedRow()
	if len(row) == 0 {
		return 0
	}
	tid, _ := strconv.Atoi(row[dcolTID])
	return tid
}

// --- private helpers ---

func (m DetailModel) renderHeader() string {
	title := fmt.Sprintf("Job %d", m.jid)
	if m.jobTitle != "" {
		title += "  " + m.jobTitle
	}
	if !m.loading {
		title += fmt.Sprintf("  (%d tasks)", len(m.tasks))
	} else {
		title += "  " + m.spinner.View()
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render("   [Esc] back  [R] refresh")
	return styles.Title.Render(title) + hint
}

func (m DetailModel) buildRows() []table.Row {
	rows := make([]table.Row, 0, len(m.tasks))
	for _, t := range m.tasks {
		rows = append(rows, table.Row{
			strconv.Itoa(t.TID),
			t.Title,
			t.State,
			progressPercent(t.Progress),
			countCell(t.RetryCount),
			t.Service,
			formatTime(t.ActiveTime, m.cfg.UI.DateFormat),
		})
	}
	return rows
}

// colorizeTasks tints each non-selected data line by its task state.
func (m DetailModel) colorizeTasks(tableStr string) string {
	lines := strings.Split(tableStr, "\n")
	for i := range lines {
		if i < 2 {
			continue // header + separator
		}
		idx := m.topRow + (i - 2)
		if idx < 0 || idx >= len(m.tasks) {
			continue
		}
		if idx == m.table.Cursor() {
			continue // selected row keeps the bubbles highlight
		}
		if ansi := statusANSI(m.tasks[idx].State); ansi != "" {
			lines[i] = "\x1b[" + ansi + "m" + lines[i] + "\x1b[m"
		}
	}
	return strings.Join(lines, "\n")
}

func (m DetailModel) makeColumns() []table.Column {
	widths := [dNumCols]int{}
	for i, n := range detailColumnNames {
		widths[i] = len(n)
	}
	for _, t := range m.tasks {
		cells := [dNumCols]string{
			strconv.Itoa(t.TID),
			"", // TITLE fills remaining width
			t.State,
			progressPercent(t.Progress),
			countCell(t.RetryCount),
			t.Service,
			formatTime(t.ActiveTime, m.cfg.UI.DateFormat),
		}
		for col, cell := range cells {
			if col == dcolTitle {
				continue
			}
			if w := len([]rune(cell)); w > widths[col] {
				widths[col] = w
			}
		}
	}
	for col := range widths {
		if col == dcolTitle {
			continue
		}
		if detailColumnCaps[col] > 0 && widths[col] > detailColumnCaps[col] {
			widths[col] = detailColumnCaps[col]
		}
	}

	fixedTotal := 0
	for col, w := range widths {
		if col != dcolTitle {
			fixedTotal += w + 2
		}
	}
	titleContent := m.width - fixedTotal - 2
	if titleContent < 10 {
		titleContent = 10
	}
	widths[dcolTitle] = titleContent

	cols := make([]table.Column, dNumCols)
	for col := range cols {
		cols[col] = table.Column{Title: detailColumnNames[col], Width: widths[col] + 2}
	}
	return cols
}

func (m DetailModel) syncTopRow() DetailModel {
	cursor := m.table.Cursor()
	height := m.viewportHeight()
	if cursor < m.topRow {
		m.topRow = cursor
	}
	if cursor >= m.topRow+height {
		m.topRow = cursor - height + 1
	}
	if m.topRow < 0 {
		m.topRow = 0
	}
	return m
}

func (m DetailModel) viewportHeight() int {
	h := m.height - 2
	if h < 1 {
		h = 1
	}
	return h
}

// progressPercent renders a task progress value (0–1 or 0–100) as a percentage,
// blank when there is no progress to show.
func progressPercent(p float64) string {
	if p <= 0 {
		return ""
	}
	if p <= 1 {
		p *= 100
	}
	return fmt.Sprintf("%.0f%%", p)
}
