// Package jobs provides the jobs list TUI view.
package jobs

import (
	"fmt"
	"sort"
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

// column indices — must match the order in makeColumns.
const (
	colJID     = 0
	colTitle   = 1
	colOwner   = 2
	colStatus  = 3
	colDone    = 4
	colErr     = 5
	colPri     = 6
	colSpooled = 7
	numCols    = 8
)

// ListModel is the jobs list view model.
type ListModel struct {
	table     table.Model
	spinner   spinner.Model
	filterBar filter.Model

	allJobs []models.Job // full set returned by API
	jobs    []models.Job // after local filters + sort applied

	loading     bool
	lastRefresh time.Time
	err         error

	apiFilter api.JobFilter // sent to the API
	myJobs    bool          // 'm' toggle — pre-fill owner filter
	errOnly   bool          // 'e' toggle — local-filter to error jobs

	sortCol int  // column index; default colJID
	sortAsc bool // ascending; default false (newest JIDs first)

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
		table:     newTable(0, 0, colJID, false),
		spinner:   sp,
		filterBar: filter.New(),
		client:    client,
		cfg:       cfg,
		sortCol:   colJID,
		sortAsc:   false, // newest first
	}
}

// Init kicks off the initial jobs fetch.
func (m ListModel) Init() tea.Cmd {
	return tea.Batch(m.doFetch(), m.spinner.Tick)
}

// Update handles messages for the jobs list.
func (m ListModel) Update(msg tea.Msg) (ListModel, tea.Cmd) {
	var cmds []tea.Cmd

	// SubmitMsg and CancelMsg are dispatched as Cmds by the filter bar after it
	// closes itself, so they arrive in the NEXT update cycle when Active() is
	// already false. Handle them first, before the Active() check.
	switch msg := msg.(type) {
	case filter.SubmitMsg:
		m.apiFilter = msg.Filter
		m.loading = true
		cmds = append(cmds, m.doFetch())
		m = m.recalcTableHeight()
		return m, tea.Batch(cmds...)
	case filter.CancelMsg:
		m = m.recalcTableHeight()
		return m, nil
	}

	// While the filter bar is active, route all input to it.
	if m.filterBar.Active() {
		var cmd tea.Cmd
		m.filterBar, cmd = m.filterBar.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case jobsLoadedMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		m.err = nil
		m.allJobs = msg.jobs
		m.jobs = m.applyLocalFilters(msg.jobs)
		m.sortJobs()
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
			m.sortJobs()
			m.table.SetRows(m.buildRows())
			return m, nil
		case "R":
			m.loading = true
			cmds = append(cmds, m.doFetch())
			return m, tea.Batch(cmds...)
		case "1", "2", "3", "4", "5", "6", "7", "8":
			col := int(msg.String()[0] - '1')
			if m.sortCol == col {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortCol = col
				// Default direction per column: JID desc (newest first), others asc.
				m.sortAsc = col != colJID
			}
			m.sortJobs()
			m.table.SetColumns(makeColumns(m.width, m.sortCol, m.sortAsc))
			m.table.SetRows(m.buildRows())
			return m, nil
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
	m.table.SetColumns(makeColumns(w, m.sortCol, m.sortAsc))
	m = m.recalcTableHeight()
	return m
}

// State accessors used by the root model for the status bar.
func (m ListModel) JobCount() int          { return len(m.jobs) }
func (m ListModel) IsLoading() bool        { return m.loading }
func (m ListModel) LastRefresh() time.Time { return m.lastRefresh }
func (m ListModel) Err() error             { return m.err }

// SelectedJID returns the JID of the currently highlighted row, or 0.
func (m ListModel) SelectedJID() int {
	row := m.table.SelectedRow()
	if len(row) == 0 {
		return 0
	}
	// Strip ANSI codes before parsing — cell content is coloured.
	raw := stripANSI(row[0])
	jid, _ := strconv.Atoi(raw)
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

// sortJobs sorts m.jobs in-place according to sortCol/sortAsc.
func (m *ListModel) sortJobs() {
	col := m.sortCol
	asc := m.sortAsc
	sort.SliceStable(m.jobs, func(i, j int) bool {
		a, b := m.jobs[i], m.jobs[j]
		var less bool
		switch col {
		case colJID:
			less = a.JID < b.JID
		case colTitle:
			less = strings.ToLower(a.Title) < strings.ToLower(b.Title)
		case colOwner:
			less = strings.ToLower(a.Owner) < strings.ToLower(b.Owner)
		case colStatus:
			less = a.Status < b.Status
		case colDone:
			less = a.NumDone < b.NumDone
		case colErr:
			less = a.NumError < b.NumError
		case colPri:
			less = a.Priority < b.Priority
		case colSpooled:
			less = a.SpoolTime < b.SpoolTime
		default:
			less = a.JID < b.JID
		}
		if asc {
			return less
		}
		return !less
	})
}

func (m ListModel) buildRows() []table.Row {
	rows := make([]table.Row, 0, len(m.jobs))
	for _, j := range m.jobs {
		st := styles.StatusColor(j.Status)

		doneCell := fmt.Sprintf("%d/%d", j.NumDone, j.NumTasks)
		errCell := ""
		if j.NumError > 0 {
			// Error count always in red regardless of row status colour.
			errCell = lipgloss.NewStyle().Inline(true).Foreground(lipgloss.Color("9")).
				Render(fmt.Sprintf("%d", j.NumError))
		}

		rows = append(rows, table.Row{
			st.Render(fmt.Sprintf("%d", j.JID)),
			st.Render(j.Title),
			st.Render(j.Owner),
			st.Render(j.Status),
			st.Render(doneCell),
			errCell,
			st.Render(fmt.Sprintf("%.0f", j.Priority)),
			st.Render(formatTime(j.SpoolTime, m.cfg.UI.DateFormat)),
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
func newTable(width, height, sortCol int, sortAsc bool) table.Model {
	t := table.New(
		table.WithColumns(makeColumns(width, sortCol, sortAsc)),
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
	// Reset cell foreground so per-row ANSI colour codes in cell content are
	// not overridden by an outer lipgloss style.
	s.Cell = lipgloss.NewStyle().Padding(0, 1)
	t.SetStyles(s)
	return t
}

// makeColumns calculates column widths to fit the given terminal width.
// Fixed columns total ~74 content chars + 16 padding (2 per col × 8 cols) = 90.
// The TITLE column gets whatever remains, clamped to 10..65.
// The sorted column header gets a ▲/▼ indicator appended.
func makeColumns(width, sortCol int, sortAsc bool) []table.Column {
	titleWidth := 40
	if width > 0 {
		titleWidth = width - 92 // 92 = fixed content (74) + padding (16) + title padding (2)
		if titleWidth < 10 {
			titleWidth = 10
		}
		if titleWidth > 65 {
			titleWidth = 65
		}
	}

	indicator := func(col int, name string) string {
		if col != sortCol {
			return name
		}
		if sortAsc {
			return name + " ▲"
		}
		return name + " ▼"
	}

	return []table.Column{
		{Title: indicator(colJID, "JID"), Width: 8},
		{Title: indicator(colTitle, "TITLE"), Width: titleWidth},
		{Title: indicator(colOwner, "OWNER"), Width: 13},
		{Title: indicator(colStatus, "STATUS"), Width: 12},
		{Title: indicator(colDone, "DONE"), Width: 9},
		{Title: indicator(colErr, "ERR"), Width: 7},
		{Title: indicator(colPri, "PRI"), Width: 7},
		{Title: indicator(colSpooled, "SPOOLED"), Width: 18},
	}
}

// formatTime parses Tractor's "2006-01-02 15:04:05" spooldate and reformats it.
// Falls back to the raw string if parsing fails.
func formatTime(s, format string) string {
	if s == "" {
		return ""
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t.Format(format)
	}
	return s
}

// stripANSI removes ANSI escape sequences from s.
// Used when reading cell values that contain colour codes.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				inEsc = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
