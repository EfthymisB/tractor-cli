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
	"github.com/tractor-cli/tractor-cli/internal/users"
)

// internal async messages
type jobsLoadedMsg struct{ jobs []models.Job }
type fetchErrMsg struct{ err error }
type usernamesLoadedMsg struct{ names []string }

// column indices — must match the order returned by makeColumns.
const (
	colJID       = 0
	colProgress  = 1
	colTitle     = 2
	colOwner     = 3
	colStatus    = 4
	colAct       = 5
	colErr       = 6
	colDone      = 7
	colTotal     = 8
	colPri       = 9
	colSpooled   = 10
	colProjects  = 11
	colSpoolHost = 12
	colMetadata  = 13
	numCols      = 14

	// progressBarWidth is the fixed glyph width of the PROGRESS bar cell.
	progressBarWidth = 12
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

	// titleWidth overrides the TITLE column content width when > 0; when 0 the
	// TITLE column auto-fills the remaining terminal width. Adjusted by the
	// user with '<' / '>'.
	titleWidth int

	// topRow mirrors bubbles/table's internal viewport offset so that
	// colorizeTable() can map rendered lines back to their job index.
	topRow int

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
		table:     newTable(),
		spinner:   sp,
		filterBar: filter.New(),
		client:    client,
		cfg:       cfg,
		sortCol:   colJID,
		sortAsc:   false, // newest first
	}
}

// Init kicks off the initial jobs fetch and loads usernames for autocomplete.
func (m ListModel) Init() tea.Cmd {
	return tea.Batch(m.doFetch(), m.spinner.Tick, m.loadUsernames())
}

// loadUsernames fetches the directory username list (Active Directory over
// GSSAPI, falling back to getent passwd) in the background for owner-filter
// autocomplete.
func (m ListModel) loadUsernames() tea.Cmd {
	cfg := users.Config{
		URI:       m.cfg.LDAP.URI,
		BaseDN:    m.cfg.LDAP.BaseDN,
		Realm:     m.cfg.LDAP.Realm,
		Username:  m.cfg.Auth.Username,
		Password:  config.Password(),
		AutoKinit: m.cfg.LDAP.AutoKinit,
	}
	return func() tea.Msg {
		return usernamesLoadedMsg{names: users.List(cfg)}
	}
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
		m.topRow = 0
		m.table.SetColumns(m.makeColumns())
		m.table.SetRows(m.buildRows())
		// Seed owner autocomplete from the owners present in the result set;
		// this always works even when getent passwd cannot enumerate LDAP.
		m.filterBar = m.filterBar.AddUsernames(distinctOwners(msg.jobs))

	case fetchErrMsg:
		m.loading = false
		m.err = msg.err

	case usernamesLoadedMsg:
		m.filterBar = m.filterBar.AddUsernames(msg.names)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		switch msg.String() {
		case "/":
			var cmd tea.Cmd
			m.filterBar, cmd = m.filterBar.Open(m.apiFilter)
			cmds = append(cmds, cmd)
			m = m.recalcTableHeight()
			return m, tea.Batch(cmds...)
		case "m":
			m.myJobs = !m.myJobs
			if m.myJobs {
				m.apiFilter.Owners = []string{m.cfg.Auth.Username}
			} else {
				m.apiFilter.Owners = nil
			}
			m.loading = true
			cmds = append(cmds, m.doFetch())
			return m, tea.Batch(cmds...)
		case "e":
			m.errOnly = !m.errOnly
			m.jobs = m.applyLocalFilters(m.allJobs)
			m.sortJobs()
			m.topRow = 0
			m.table.SetColumns(m.makeColumns())
			m.table.SetRows(m.buildRows())
			return m, nil
		case "R":
			m.loading = true
			cmds = append(cmds, m.doFetch())
			return m, tea.Batch(cmds...)
		case "c":
			// Clear every active filter (server-side filter, my-jobs and
			// errors-only toggles) and reload the full job list.
			m.apiFilter = api.JobFilter{}
			m.myJobs = false
			m.errOnly = false
			m.loading = true
			cmds = append(cmds, m.doFetch())
			return m, tea.Batch(cmds...)
		case "<", ",":
			// Shrink the TITLE column.
			m.titleWidth = m.currentTitleWidth() - 4
			if m.titleWidth < 10 {
				m.titleWidth = 10
			}
			m.table.SetColumns(m.makeColumns())
			return m, nil
		case ">", ".":
			// Grow the TITLE column.
			m.titleWidth = m.currentTitleWidth() + 4
			m.table.SetColumns(m.makeColumns())
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			col := int(msg.String()[0] - '1')
			if m.sortCol == col {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortCol = col
				m.sortAsc = col != colJID
			}
			m.sortJobs()
			m.topRow = 0
			m.table.SetColumns(m.makeColumns())
			m.table.SetRows(m.buildRows())
			return m, nil
		}
	}

	// Delegate navigation to the table widget.
	var tableCmd tea.Cmd
	m.table, tableCmd = m.table.Update(msg)
	cmds = append(cmds, tableCmd)

	// Sync our topRow with the table's actual viewport position so that
	// colorizeTable() maps lines to the correct job indices.
	m = m.syncTopRow()

	return m, tea.Batch(cmds...)
}

// View renders the jobs list.
func (m ListModel) View() string {
	if m.width == 0 {
		return ""
	}
	header := m.renderHeader()
	tableView := m.colorizeTable(m.table.View())
	if m.filterBar.Active() {
		return header + "\n" + m.filterBar.View(m.width) + "\n" + tableView
	}
	return header + "\n" + tableView
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
	m.table.SetColumns(m.makeColumns())
	m = m.recalcTableHeight()
	return m
}

// State accessors used by the root model for the status bar.
func (m ListModel) JobCount() int          { return len(m.jobs) }
func (m ListModel) IsLoading() bool        { return m.loading }
func (m ListModel) LastRefresh() time.Time { return m.lastRefresh }
func (m ListModel) Err() error             { return m.err }

// CapturingInput reports whether a child component (the filter form) is
// currently consuming raw keystrokes, so the root model must not treat keys
// like "q" as global shortcuts.
func (m ListModel) CapturingInput() bool { return m.filterBar.Active() }

// SelectedJID returns the JID of the currently highlighted row, or 0.
func (m ListModel) SelectedJID() int {
	row := m.table.SelectedRow()
	if len(row) == 0 {
		return 0
	}
	jid, _ := strconv.Atoi(row[colJID])
	return jid
}

// SelectedTitle returns the title of the currently highlighted row, or "".
func (m ListModel) SelectedTitle() string {
	row := m.table.SelectedRow()
	if len(row) == 0 {
		return ""
	}
	return row[colTitle]
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

func (m *ListModel) sortJobs() {
	col := m.sortCol
	asc := m.sortAsc
	sort.SliceStable(m.jobs, func(i, j int) bool {
		a, b := m.jobs[i], m.jobs[j]
		var less bool
		switch col {
		case colJID:
			less = a.JID < b.JID
		case colProgress:
			less = progressFrac(a) < progressFrac(b)
		case colTitle:
			less = strings.ToLower(a.Title) < strings.ToLower(b.Title)
		case colOwner:
			less = strings.ToLower(a.Owner) < strings.ToLower(b.Owner)
		case colStatus:
			less = a.Status < b.Status
		case colAct:
			less = a.NumActive < b.NumActive
		case colErr:
			less = a.NumError < b.NumError
		case colDone:
			less = a.NumDone < b.NumDone
		case colTotal:
			less = a.NumTasks < b.NumTasks
		case colPri:
			less = a.Priority < b.Priority
		case colSpooled:
			less = a.SpoolTime < b.SpoolTime
		case colProjects:
			less = a.Projects < b.Projects
		case colSpoolHost:
			less = a.SpoolHost < b.SpoolHost
		case colMetadata:
			less = a.Metadata < b.Metadata
		default:
			less = a.JID < b.JID
		}
		if asc {
			return less
		}
		return !less
	})
}

// buildRows produces plain-text table rows — NO ANSI codes in cells.
// Row-level colours are applied later in colorizeTable() to avoid breaking
// the table's width calculations when it truncates/pads cells.
func (m ListModel) buildRows() []table.Row {
	rows := make([]table.Row, 0, len(m.jobs))
	for _, j := range m.jobs {
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", j.JID),
			progressBarPlain(j, progressBarWidth),
			j.Title,
			j.Owner,
			j.Status,
			countCell(j.NumActive),
			countCell(j.NumError),
			strconv.Itoa(j.NumDone),
			strconv.Itoa(j.NumTasks),
			fmt.Sprintf("%.0f", j.Priority),
			formatTime(j.SpoolTime, m.cfg.UI.DateFormat),
			j.Projects,
			j.SpoolHost,
			j.Metadata,
		})
	}
	return rows
}

// colorizeTable repaints each data line after the table is rendered: the
// PROGRESS bar cell gets its per-segment colours and the rest of the row gets
// the job's status tint. The header/separator and the selected row (which keeps
// the bubbles highlight) are left untouched.
func (m ListModel) colorizeTable(tableStr string) string {
	lines := strings.Split(tableStr, "\n")
	for i := range lines {
		if i < 2 {
			continue // header row + separator line
		}
		jobIdx := m.topRow + (i - 2)
		if jobIdx < 0 || jobIdx >= len(m.jobs) {
			continue
		}
		if jobIdx == m.table.Cursor() {
			continue // selected row keeps the purple s.Selected highlight
		}
		lines[i] = m.colorizeLine(lines[i], m.jobs[jobIdx])
	}
	return strings.Join(lines, "\n")
}

// colorizeLine tints a single plain-text data line. It locates the progress bar
// (the first run of block glyphs, which sits right after the numeric JID),
// replaces it with the coloured bar, and wraps the surrounding text in the
// job's status colour.
func (m ListModel) colorizeLine(line string, job models.Job) string {
	status := statusANSI(job.Status)
	runes := []rune(line)

	barStart := -1
	for k, r := range runes {
		if r == '▬' || r == '▭' {
			barStart = k
			break
		}
	}
	if barStart < 0 {
		// No bar found (e.g. empty table); tint the whole line.
		if status == "" {
			return line
		}
		return "\x1b[" + status + "m" + line + "\x1b[m"
	}

	barEnd := barStart + progressBarWidth
	if barEnd > len(runes) {
		barEnd = len(runes)
	}
	prefix := string(runes[:barStart])
	suffix := string(runes[barEnd:])
	bar := progressBarColored(job, progressBarWidth)

	wrap := func(s string) string {
		if s == "" || status == "" {
			return s
		}
		return "\x1b[" + status + "m" + s + "\x1b[m"
	}
	return wrap(prefix) + bar + wrap(suffix)
}

// statusANSI maps a job status to an ANSI SGR parameter string (the part that
// goes between \x1b[ and m).
func statusANSI(status string) string {
	switch status {
	case "active":
		return "38;5;10"  // bright green
	case "error":
		return "38;5;9"   // bright red
	case "blocked":
		return "38;5;11"  // bright yellow
	case "paused":
		return "38;5;14"  // bright cyan
	case "done":
		return "38;5;240" // medium grey
	default:
		return ""
	}
}

// syncTopRow replicates bubbles/table's updateViewport logic so topRow stays
// in sync with the table's actual scroll position after every cursor move.
func (m ListModel) syncTopRow() ListModel {
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

// viewportHeight returns the number of visible data rows in the table.
// Must match the value passed to table.SetHeight() in recalcTableHeight().
func (m ListModel) viewportHeight() int {
	h := m.height - 2
	if m.filterBar.Active() {
		h -= m.filterBar.Height() + 1 // form lines + the blank join line
	}
	if h < 1 {
		h = 1
	}
	return h
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
	// JID filter tags.
	if len(m.apiFilter.JIDs) > 0 {
		jidStrs := make([]string, len(m.apiFilter.JIDs))
		for i, id := range m.apiFilter.JIDs {
			jidStrs[i] = strconv.Itoa(id)
		}
		tags = append(tags, "jid:"+strings.Join(jidStrs, ","))
	}
	if m.apiFilter.JIDFrom > 0 && m.apiFilter.JIDTo > 0 {
		tags = append(tags, fmt.Sprintf("jid:%d-%d", m.apiFilter.JIDFrom, m.apiFilter.JIDTo))
	}
	// Owner filter (covers both the 'm' toggle and filter-bar owner:).
	if len(m.apiFilter.Owners) > 0 {
		tags = append(tags, "owner:"+strings.Join(m.apiFilter.Owners, ","))
	}
	if len(m.apiFilter.Statuses) > 0 {
		tags = append(tags, "status:"+strings.Join(m.apiFilter.Statuses, ","))
	}
	if m.apiFilter.Priority != 0 {
		tags = append(tags, fmt.Sprintf("prio:%.0f", m.apiFilter.Priority))
	}
	if m.apiFilter.Title != "" {
		tags = append(tags, "title:"+m.apiFilter.Title)
	}
	if len(m.apiFilter.Projects) > 0 {
		tags = append(tags, "projects:"+strings.Join(m.apiFilter.Projects, ","))
	}
	// errOnly is a local-only display filter.
	if m.errOnly {
		tags = append(tags, "errors-only")
	}
	if len(tags) > 0 {
		title += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
			Render("["+strings.Join(tags, "  ")+"]")
	}
	return styles.Title.Render(title)
}

func (m ListModel) recalcTableHeight() ListModel {
	m.table.SetHeight(m.viewportHeight())
	return m
}

// newTable builds a fresh bubbles/table with our styles. Columns are set later
// via makeColumns() once the terminal size and job data are known.
func newTable() table.Model {
	t := table.New(table.WithFocused(true))
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
	// Keep cell style to plain padding; row colours come from colorizeTable().
	s.Cell = lipgloss.NewStyle().Padding(0, 1)
	t.SetStyles(s)
	return t
}

// columnNames are the header labels in column order.
var columnNames = [numCols]string{
	"JID", "PROGRESS", "TITLE", "OWNER", "STATUS", "ACT", "ERR", "DONE",
	"TOTAL", "PRI", "SPOOLED", "PROJECTS", "SPOOLHOST", "METADATA",
}

// columnCaps are per-column hard caps on content width (0 = no cap). They keep
// outlier data (long project lists, metadata) from crowding out TITLE.
var columnCaps = [numCols]int{
	0, 0, 0, 20, 0, 0, 0, 0, 0, 0, 0, 25, 25, 35,
}

// columnContentWidths returns the content width of every column by scanning the
// current job data. Each column (except TITLE) is sized to its widest value or
// header, capped by columnCaps. TITLE is set to the remaining terminal width.
func (m ListModel) columnContentWidths() [numCols]int {
	widths := [numCols]int{}
	for i, n := range columnNames {
		widths[i] = len(n)
	}
	// The sort indicator " ▲" / " ▼" adds 2 visible chars to that column's header.
	if m.sortCol >= 0 && m.sortCol < numCols {
		widths[m.sortCol] += 2
	}

	for _, j := range m.jobs {
		cells := [numCols]string{
			strconv.Itoa(j.JID),
			progressBarPlain(j, progressBarWidth),
			"", // TITLE: excluded from auto-resize (fills remaining width)
			j.Owner,
			j.Status,
			countCell(j.NumActive),
			countCell(j.NumError),
			strconv.Itoa(j.NumDone),
			strconv.Itoa(j.NumTasks),
			fmt.Sprintf("%.0f", j.Priority),
			formatTime(j.SpoolTime, m.cfg.UI.DateFormat),
			j.Projects,
			j.SpoolHost,
			j.Metadata,
		}
		for col, cell := range cells {
			if col == colTitle {
				continue
			}
			if w := len([]rune(cell)); w > widths[col] {
				widths[col] = w
			}
		}
	}

	for col := range widths {
		if col == colTitle {
			continue
		}
		if columnCaps[col] > 0 && widths[col] > columnCaps[col] {
			widths[col] = columnCaps[col]
		}
	}

	// TITLE auto-fill = terminal width minus all other columns (content + 2
	// padding each), minus TITLE's own padding.
	fixedTotal := 0
	for col, w := range widths {
		if col != colTitle {
			fixedTotal += w + 2
		}
	}
	titleContent := m.width - fixedTotal - 2
	if titleContent < 10 {
		titleContent = 10
	}
	widths[colTitle] = titleContent
	return widths
}

// currentTitleWidth returns the effective TITLE content width: the user's
// override if set, otherwise the auto-filled remaining width.
func (m ListModel) currentTitleWidth() int {
	if m.titleWidth > 0 {
		return m.titleWidth
	}
	return m.columnContentWidths()[colTitle]
}

// makeColumns builds the bubbles/table columns. TITLE honours a user override
// (set via '<' / '>'); every other column auto-resizes to its content. The
// sorted column gets a ▲/▼ appended to its header.
func (m ListModel) makeColumns() []table.Column {
	widths := m.columnContentWidths()
	if m.titleWidth > 0 {
		widths[colTitle] = m.titleWidth
	}

	header := func(col int) string {
		if col != m.sortCol {
			return columnNames[col]
		}
		if m.sortAsc {
			return columnNames[col] + " ▲"
		}
		return columnNames[col] + " ▼"
	}

	cols := make([]table.Column, numCols)
	for col := range cols {
		cols[col] = table.Column{Title: header(col), Width: widths[col] + 2}
	}
	return cols
}

// --- progress bar ---

// progressSegments splits a bar of the given width into done / error / active /
// remaining character counts, proportional to a job's task counts. The four
// returned values always sum to width. Non-zero error/active counts are nudged
// to at least one visible cell when there is room.
func progressSegments(j models.Job, width int) (done, errs, act, rem int) {
	total := j.NumTasks
	if total <= 0 || width <= 0 {
		if width < 0 {
			width = 0
		}
		return 0, 0, 0, width
	}
	round := func(n int) int { return (n*width + total/2) / total }
	done = round(j.NumDone)
	errs = round(j.NumError)
	act = round(j.NumActive)

	if done > width {
		done = width
	}
	if done+errs > width {
		errs = width - done
	}
	if done+errs+act > width {
		act = width - done - errs
	}
	if j.NumError > 0 && errs == 0 && done+act < width {
		errs = 1
	}
	if j.NumActive > 0 && act == 0 && done+errs+act < width {
		act = 1
	}
	rem = width - done - errs - act
	if rem < 0 {
		rem = 0
	}
	return
}

// progressBarPlain renders an uncoloured, fixed-width bar (block glyphs only)
// for use as the table cell value. colorizeTable repaints it per-job after the
// table is rendered (the table truncator is not ANSI-aware, so colour must not
// live in the cell value itself).
func progressBarPlain(j models.Job, width int) string {
	d, e, a, r := progressSegments(j, width)
	return strings.Repeat("▬", d+e+a) + strings.Repeat("▭", r)
}

// progressBarColored renders the multi-colour bar: done=grey, error=red,
// active=green, remaining=blank. Each segment carries its own ANSI reset.
func progressBarColored(j models.Job, width int) string {
	d, e, a, r := progressSegments(j, width)
	var b strings.Builder
	seg := func(n int, color string) {
		if n <= 0 {
			return
		}
		b.WriteString("\x1b[38;5;" + color + "m")
		b.WriteString(strings.Repeat("▬", n))
		b.WriteString("\x1b[0m")
	}
	seg(d, "240") // done: grey
	seg(e, "9")   // error: red
	seg(a, "10")  // active: green
	if r > 0 {
		b.WriteString(strings.Repeat(" ", r)) // remaining: nothing
	}
	return b.String()
}

// progressFrac is the completed-task fraction used to sort the PROGRESS column.
func progressFrac(j models.Job) float64 {
	if j.NumTasks <= 0 {
		return 0
	}
	return float64(j.NumDone) / float64(j.NumTasks)
}

// distinctOwners returns the unique, non-empty owner names across the jobs, in
// first-seen order. Used to seed owner-filter autocomplete from real data.
func distinctOwners(jobs []models.Job) []string {
	seen := make(map[string]struct{}, len(jobs))
	var out []string
	for _, j := range jobs {
		if j.Owner == "" {
			continue
		}
		if _, ok := seen[j.Owner]; ok {
			continue
		}
		seen[j.Owner] = struct{}{}
		out = append(out, j.Owner)
	}
	return out
}

// countCell renders a task-count cell, leaving it blank for zero so the ACT
// and ERR columns stay visually quiet for jobs with none.
func countCell(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// formatTime parses Tractor's "2006-01-02 15:04:05" spooldate and reformats it.
func formatTime(s, format string) string {
	if s == "" {
		return ""
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t.Format(format)
	}
	return s
}
