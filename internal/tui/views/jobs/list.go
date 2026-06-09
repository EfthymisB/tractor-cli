// Package jobs provides the jobs list TUI view.
package jobs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
	"github.com/tractor-cli/tractor-cli/internal/models"
	"github.com/tractor-cli/tractor-cli/internal/tui/styles"
	"github.com/tractor-cli/tractor-cli/internal/tui/views/filter"
)

// internal async messages
type jobsLoadedMsg struct{ jobs []models.Job }
type fetchErrMsg struct{ err error }

// ListModel is the jobs list view model.
type ListModel struct {
	table     table.Model
	spinner   spinner.Model
	filterBar filter.Model

	allJobs []models.Job // full set returned by API
	jobs    []models.Job // after local filters applied

	loading     bool
	lastRefresh time.Time
	err         error

	apiFilter api.JobFilter // sent to the API
	myJobs    bool          // 'm' toggle — pre-fill owner filter
	errOnly   bool          // 'e' toggle — local-filter to error jobs

	client *api.Client
	cfg    *config.Config
	width  int
	height int
}

// New creates a new jobs list model.
func New(client *api.Client, cfg *config.Config) ListModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

	return ListModel{
		table:     newTable(0, 0),
		spinner:   sp,
		filterBar: filter.New(),
		client:    client,
		cfg:       cfg,
	}
}

// Init kicks off the initial jobs fetch.
func (m ListModel) Init() tea.Cmd {
	return tea.Batch(m.doFetch(), m.spinner.Tick)
}

// Update handles messages for the jobs list.
func (m ListModel) Update(msg tea.Msg) (ListModel, tea.Cmd) {
	var cmds []tea.Cmd

	// While the filter bar is active, route all input to it.
	if m.filterBar.Active() {
		var cmd tea.Cmd
		m.filterBar, cmd = m.filterBar.Update(msg)
		cmds = append(cmds, cmd)
		switch msg := msg.(type) {
		case filter.SubmitMsg:
			m.apiFilter = msg.Filter
			m.loading = true
			cmds = append(cmds, m.doFetch())
			m = m.recalcTableHeight()
		case filter.CancelMsg:
			m = m.recalcTableHeight()
		}
		return m, tea.Batch(cmds...)
	}

	switch msg := msg.(type) {
	case jobsLoadedMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		m.err = nil
		m.allJobs = msg.jobs
		m.jobs = m.applyLocalFilters(msg.jobs)
		m.table.SetRows(m.buildRows())

	case fetchErrMsg:
		m.loading = false
		m.err = msg.err

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		switch msg.String() {
		case "/":
			var cmd tea.Cmd
			m.filterBar, cmd = m.filterBar.Open()
			cmds = append(cmds, cmd)
			m = m.recalcTableHeight()
			return m, tea.Batch(cmds...)
		case "m":
			m.myJobs = !m.myJobs
			if m.myJobs {
				m.apiFilter.Owner = m.cfg.Auth.Username
			} else {
				m.apiFilter.Owner = ""
			}
			m.loading = true
			cmds = append(cmds, m.doFetch())
			return m, tea.Batch(cmds...)
		case "e":
			m.errOnly = !m.errOnly
			m.jobs = m.applyLocalFilters(m.allJobs)
			m.table.SetRows(m.buildRows())
			return m, nil
		case "R":
			m.loading = true
			cmds = append(cmds, m.doFetch())
			return m, tea.Batch(cmds...)
		}
	}

	// Delegate navigation to the table widget.
	var tableCmd tea.Cmd
	m.table, tableCmd = m.table.Update(msg)
	cmds = append(cmds, tableCmd)

	return m, tea.Batch(cmds...)
}

// View renders the jobs list.
func (m ListModel) View() string {
	if m.width == 0 {
		return ""
	}
	header := m.renderHeader()
	if m.filterBar.Active() {
		return header + "\n" + m.filterBar.View(m.width) + "\n" + m.table.View()
	}
	return header + "\n" + m.table.View()
}

// Refresh triggers a background re-fetch and marks the model as loading.
func (m ListModel) Refresh() (ListModel, tea.Cmd) {
	m.loading = true
	return m, m.doFetch()
}

// SetSize updates the terminal dimensions and resizes the table.
func (m ListModel) SetSize(w, h int) ListModel {
	m.width = w
	m.height = h
	m.table.SetColumns(makeColumns(w))
	m = m.recalcTableHeight()
	return m
}

// State accessors used by the root model for the status bar.
func (m ListModel) JobCount() int         { return len(m.jobs) }
func (m ListModel) IsLoading() bool       { return m.loading }
func (m ListModel) LastRefresh() time.Time { return m.lastRefresh }
func (m ListModel) Err() error            { return m.err }

// SelectedJID returns the JID of the currently highlighted row, or 0.
func (m ListModel) SelectedJID() int {
	row := m.table.SelectedRow()
	if len(row) == 0 {
		return 0
	}
	jid, _ := strconv.Atoi(row[0])
	return jid
}

// --- private helpers ---

func (m ListModel) doFetch() tea.Cmd {
	f := m.apiFilter
	return func() tea.Msg {
		jobs, err := m.client.QueryJobs(f)
		if err != nil {
			return fetchErrMsg{err}
		}
		return jobsLoadedMsg{jobs}
	}
}

func (m ListModel) applyLocalFilters(src []models.Job) []models.Job {
	if !m.errOnly {
		return src
	}
	out := make([]models.Job, 0, len(src))
	for _, j := range src {
		if j.Status == "error" || j.NumError > 0 {
			out = append(out, j)
		}
	}
	return out
}

func (m ListModel) buildRows() []table.Row {
	rows := make([]table.Row, 0, len(m.jobs))
	for _, j := range m.jobs {
		statusCell := styles.StatusColor(j.Status).Render(j.Status)
		doneCell := fmt.Sprintf("%d/%d", j.NumDone, j.NumTasks)
		errCell := ""
		if j.NumError > 0 {
			errCell = styles.ErrorStyle.Render(fmt.Sprintf("%d", j.NumError))
		}
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", j.JID),
			j.Title,
			j.Owner,
			statusCell,
			doneCell,
			errCell,
			fmt.Sprintf("%.0f", j.Priority),
			formatTime(j.SpoolTime, m.cfg.UI.DateFormat),
		})
	}
	return rows
}

func (m ListModel) renderHeader() string {
	title := "Jobs"
	if !m.loading && len(m.jobs) > 0 {
		title = fmt.Sprintf("Jobs (%d)", len(m.jobs))
	}
	if m.loading {
		title += "  " + m.spinner.View()
	}

	var tags []string
	if m.myJobs {
		tags = append(tags, "owner:"+m.cfg.Auth.Username)
	}
	if m.errOnly {
		tags = append(tags, "status:error")
	}
	if m.apiFilter.Title != "" {
		tags = append(tags, "title:"+m.apiFilter.Title)
	}
	if len(tags) > 0 {
		title += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
			Render("["+strings.Join(tags, "  ")+"]")
	}
	return styles.Title.Render(title)
}

func (m ListModel) recalcTableHeight() ListModel {
	h := m.height - 2 // 1 header line + 1 for table's own border row
	if m.filterBar.Active() {
		h -= 3 // rounded-border filter bar is 3 lines tall
	}
	if h < 1 {
		h = 1
	}
	m.table.SetHeight(h)
	return m
}

// newTable builds a fresh bubbles/table with our default styles.
func newTable(width, height int) table.Model {
	t := table.New(
		table.WithColumns(makeColumns(width)),
		table.WithFocused(true),
		table.WithHeight(height),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)
	return t
}

// makeColumns calculates column widths to fit the given terminal width.
// Fixed columns total 64 content chars + 16 padding (2 per col × 8 cols).
func makeColumns(width int) []table.Column {
	titleWidth := 40
	if width > 0 {
		titleWidth = width - 80 // 80 = fixed cols + padding
		if titleWidth < 10 {
			titleWidth = 10
		}
		if titleWidth > 55 {
			titleWidth = 55
		}
	}
	return []table.Column{
		{Title: "JID", Width: 6},
		{Title: "TITLE", Width: titleWidth},
		{Title: "OWNER", Width: 10},
		{Title: "STATUS", Width: 9},
		{Title: "DONE", Width: 7},
		{Title: "ERR", Width: 5},
		{Title: "PRI", Width: 5},
		{Title: "SPOOLED", Width: 14},
	}
}

// formatTime tries to parse s as a Unix timestamp float and format it.
// Falls back to the raw string if parsing fails.
func formatTime(s, format string) string {
	if s == "" {
		return ""
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Unix(int64(f), 0).Local().Format(format)
	}
	return s
}
