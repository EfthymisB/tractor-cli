package jobs

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
	"github.com/tractor-cli/tractor-cli/internal/models"
	"github.com/tractor-cli/tractor-cli/internal/tui/styles"
)

// tasksPanelHeight is the height (incl. border) of the bottom task-info panel.
const tasksPanelHeight = 6

// async messages for the detail view
type tasksLoadedMsg struct {
	jid   int
	tasks []models.Task
}
type taskFetchErrMsg struct{ err error }

type taskDetailLoadedMsg struct {
	jid    int
	tid    int
	detail models.TaskDetail
}
type taskDetailErrMsg struct {
	tid int
	err error
}

// detail column indices — must match the order in detailColumnNames/buildRows.
const (
	dcolTID      = 0
	dcolTitle    = 1
	dcolState    = 2
	dcolProgress = 3
	dcolBlade    = 4
	dcolService  = 5
	dcolActive   = 6
	dNumCols     = 7
)

var detailColumnNames = [dNumCols]string{
	"TID", "TITLE", "STATE", "PROG", "BLADE", "SERVICE", "ACTIVE",
}

var detailColumnCaps = [dNumCols]int{0, 0, 0, 0, 20, 24, 0}

// DetailModel is the per-job task list view (Phase 3).
type DetailModel struct {
	table   table.Model
	spinner spinner.Model
	search  textinput.Model

	jid      int
	jobTitle string
	jobOwner string
	allTasks []models.Task // unfiltered
	tasks    []models.Task // after title filter + sort

	searching bool
	sortCol   int
	sortAsc   bool

	selected map[int]bool // multi-selected TIDs for bulk actions

	// bottom info panel for the highlighted task
	detailTID     int
	taskDetail    models.TaskDetail
	detailLoading bool

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

	ti := textinput.New()
	ti.Placeholder = "title…"
	ti.Prompt = ""
	ti.CharLimit = 128

	return DetailModel{
		table:    newTable(),
		spinner:  sp,
		search:   ti,
		selected: make(map[int]bool),
		sortCol:  dcolTID,
		sortAsc:  true,
		client:   client,
		cfg:      cfg,
	}
}

// Load points the detail view at a job and kicks off a task fetch.
func (m DetailModel) Load(jid int, title, owner string) (DetailModel, tea.Cmd) {
	m.jid = jid
	m.jobTitle = title
	m.jobOwner = owner
	m.allTasks = nil
	m.tasks = nil
	m.err = nil
	m.loading = true
	m.topRow = 0
	m.searching = false
	m.search.SetValue("")
	m.search.Blur()
	m.sortCol = dcolTID
	m.sortAsc = true
	m.detailTID = 0
	m.taskDetail = models.TaskDetail{}
	m.detailLoading = false
	m.selected = make(map[int]bool)
	m.table.SetRows(nil)
	return m, tea.Batch(m.fetchTasks(), m.spinner.Tick)
}

// Refresh re-fetches the current job's tasks (and, after they load, the
// highlighted task's details, so live stats like RSS/CPU update too).
func (m DetailModel) Refresh() (DetailModel, tea.Cmd) {
	if m.jid == 0 {
		return m, nil
	}
	m.loading = true
	m.detailTID = 0 // force the info panel to re-fetch for the current selection
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
		m.allTasks = msg.tasks
		m.topRow = 0
		m = m.refreshTable()
		var dcmd tea.Cmd
		m, dcmd = m.maybeFetchDetail()
		return m, dcmd

	case taskFetchErrMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case taskDetailLoadedMsg:
		if msg.jid == m.jid && msg.tid == m.detailTID {
			m.taskDetail = msg.detail
			m.detailLoading = false
		}
		return m, nil

	case taskDetailErrMsg:
		if msg.tid == m.detailTID {
			m.detailLoading = false
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		// While the title search is open it owns the keyboard.
		if m.searching {
			switch msg.String() {
			case "enter":
				m.searching = false
				m.search.Blur()
				m.table.SetHeight(m.viewportHeight())
				return m, nil
			case "esc":
				m.searching = false
				m.search.SetValue("")
				m.search.Blur()
				m = m.refreshTable()
				m.table.SetHeight(m.viewportHeight())
				var dcmd tea.Cmd
				m, dcmd = m.maybeFetchDetail()
				return m, dcmd
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m = m.refreshTable()
			var dcmd tea.Cmd
			m, dcmd = m.maybeFetchDetail()
			return m, tea.Batch(cmd, dcmd)
		}

		switch msg.String() {
		case "/":
			m.searching = true
			m.table.SetHeight(m.viewportHeight())
			return m, m.search.Focus()
		case "R":
			var cmd tea.Cmd
			m, cmd = m.Refresh()
			return m, cmd
		case "1", "2", "3", "4", "5", "6", "7":
			col := int(msg.String()[0] - '1')
			if col >= 0 && col < dNumCols {
				if m.sortCol == col {
					m.sortAsc = !m.sortAsc
				} else {
					m.sortCol = col
					m.sortAsc = true
				}
				m.topRow = 0
				m = m.refreshTable()
				var dcmd tea.Cmd
				m, dcmd = m.maybeFetchDetail()
				return m, dcmd
			}
			return m, nil
		case " ", "space":
			// Toggle multi-selection on the highlighted task.
			if tid := m.SelectedTID(); tid > 0 {
				if m.selected[tid] {
					delete(m.selected, tid)
				} else {
					m.selected[tid] = true
				}
			}
			return m, nil
		case "A":
			for _, t := range m.tasks {
				m.selected[t.TID] = true
			}
			return m, nil
		}
	}

	var tableCmd tea.Cmd
	m.table, tableCmd = m.table.Update(msg)
	cmds = append(cmds, tableCmd)
	m = m.syncTopRow()
	var dcmd tea.Cmd
	m, dcmd = m.maybeFetchDetail()
	cmds = append(cmds, dcmd)

	return m, tea.Batch(cmds...)
}

// maybeFetchDetail issues a task-details fetch when the highlighted task has
// changed since the panel was last populated.
func (m DetailModel) maybeFetchDetail() (DetailModel, tea.Cmd) {
	cur := m.SelectedTID()
	if cur == 0 || cur == m.detailTID {
		return m, nil
	}
	m.detailTID = cur
	m.taskDetail = models.TaskDetail{}
	m.detailLoading = true
	return m, m.fetchDetail(cur)
}

func (m DetailModel) fetchDetail(tid int) tea.Cmd {
	jid := m.jid
	return func() tea.Msg {
		d, err := m.client.TaskDetails(jid, tid)
		if err != nil {
			return taskDetailErrMsg{tid: tid, err: err}
		}
		return taskDetailLoadedMsg{jid: jid, tid: tid, detail: d}
	}
}

// CapturingInput reports whether the title-search input is consuming keys.
func (m DetailModel) CapturingInput() bool { return m.searching }

// refreshTable recomputes the filtered/sorted task slice and repopulates the
// bubbles table.
func (m DetailModel) refreshTable() DetailModel {
	m.tasks = m.filterSortTasks()
	m.table.SetColumns(m.makeColumns())
	m.table.SetRows(m.buildRows())
	return m
}

// filterSortTasks applies the title filter then the active column sort.
func (m DetailModel) filterSortTasks() []models.Task {
	q := strings.ToLower(strings.TrimSpace(m.search.Value()))
	out := make([]models.Task, 0, len(m.allTasks))
	for _, t := range m.allTasks {
		if q == "" || strings.Contains(strings.ToLower(t.Title), q) {
			out = append(out, t)
		}
	}
	col, asc := m.sortCol, m.sortAsc
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		var less bool
		switch col {
		case dcolTID:
			less = a.TID < b.TID
		case dcolTitle:
			less = strings.ToLower(a.Title) < strings.ToLower(b.Title)
		case dcolState:
			less = a.State < b.State
		case dcolProgress:
			less = a.Progress < b.Progress
		case dcolBlade:
			less = a.Blade < b.Blade
		case dcolService:
			less = a.Service < b.Service
		case dcolActive:
			less = a.ActiveTime < b.ActiveTime
		default:
			less = a.TID < b.TID
		}
		if asc {
			return less
		}
		return !less
	})
	return out
}

// View renders the detail view.
func (m DetailModel) View() string {
	if m.width == 0 {
		return ""
	}
	out := m.renderHeader() + "\n"
	if m.searching {
		out += m.renderSearch() + "\n"
	}
	return out + m.colorizeTasks(m.table.View()) + "\n" + m.renderPanel()
}

func (m DetailModel) renderSearch() string {
	return styles.FilterBorder.Width(m.width - 4).Render("Search title: " + m.search.View())
}

// renderPanel draws the bottom info panel for the highlighted task. Its height
// is fixed at tasksPanelHeight (4 content lines + border).
func (m DetailModel) renderPanel() string {
	var lines []string
	switch {
	case m.detailLoading:
		lines = []string{"Loading task details… " + m.spinner.View()}
	case m.detailTID == 0:
		lines = []string{"—"}
	default:
		d := m.taskDetail
		lines = []string{
			"Command: " + d.Command,
			fmt.Sprintf("Blades: %s    Service: %s",
				orDash(strings.Join(d.Blades, ",")), orDash(d.Service)),
			fmt.Sprintf("RSS: %.0f MB    VSZ: %.0f MB    CPU: %.0f%%    Slots: %d",
				d.RSSMB, d.VSZMB, d.CPU, d.Slots),
			fmt.Sprintf("Env: %s    Min/Max Slots: %d/%d    Min/Max Run: %ds/%ds",
				orDash(d.EnvKey), d.MinSlots, d.MaxSlots, d.MinRunSecs, d.MaxRunSecs),
		}
	}
	for i := range lines {
		lines[i] = truncRunes(lines[i], m.width-2)
	}
	for len(lines) < 4 {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(m.width - 2).
		Render(strings.Join(lines[:4], "\n"))
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
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
func (m DetailModel) JID() int           { return m.jid }
func (m DetailModel) Owner() string      { return m.jobOwner }
func (m DetailModel) TaskCount() int     { return len(m.tasks) }
func (m DetailModel) IsLoading() bool    { return m.loading }
func (m DetailModel) Err() error         { return m.err }

// JobTitle returns the title of the job whose tasks are shown.
func (m DetailModel) JobTitle() string { return m.jobTitle }

// TitleForTID returns the title of the task with the given id, or "".
func (m DetailModel) TitleForTID(tid int) string {
	for _, t := range m.tasks {
		if t.TID == tid {
			return t.Title
		}
	}
	for _, t := range m.allTasks {
		if t.TID == tid {
			return t.Title
		}
	}
	return ""
}

// AdjacentTID returns the task id dir steps away from tid in the current
// (filtered/sorted) order, or 0 if out of range.
func (m DetailModel) AdjacentTID(tid, dir int) int {
	for i, t := range m.tasks {
		if t.TID == tid {
			j := i + dir
			if j >= 0 && j < len(m.tasks) {
				return m.tasks[j].TID
			}
			return 0
		}
	}
	return 0
}

// SelectTID moves the cursor to the task with the given id so returning from
// the log view lands on the right task. The info panel is marked stale (it is
// refreshed via RefreshPanel when the detail view becomes active again).
func (m DetailModel) SelectTID(tid int) DetailModel {
	for i, t := range m.tasks {
		if t.TID == tid {
			m.table.SetCursor(i)
			m = m.syncTopRow()
			m.detailTID = 0
			m.detailLoading = false
			m.taskDetail = models.TaskDetail{}
			break
		}
	}
	return m
}

// RefreshPanel re-fetches the info panel for the current selection (used when
// returning to the detail view from the log view).
func (m DetailModel) RefreshPanel() (DetailModel, tea.Cmd) {
	return m.maybeFetchDetail()
}

// SelectionCount returns the number of multi-selected tasks.
func (m DetailModel) SelectionCount() int { return len(m.selected) }

// SelectedTIDs returns the multi-selected task ids, ascending.
func (m DetailModel) SelectedTIDs() []int {
	out := make([]int, 0, len(m.selected))
	for tid := range m.selected {
		out = append(out, tid)
	}
	sort.Ints(out)
	return out
}

// ClearSelection drops the multi-selection.
func (m DetailModel) ClearSelection() DetailModel {
	m.selected = make(map[int]bool)
	return m
}

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
	if m.loading {
		title += "  " + m.spinner.View()
	} else if len(m.tasks) != len(m.allTasks) {
		title += fmt.Sprintf("  (%d/%d tasks)", len(m.tasks), len(m.allTasks))
	} else {
		title += fmt.Sprintf("  (%d tasks)", len(m.tasks))
	}
	if n := len(m.selected); n > 0 {
		title += lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true).
			Render(fmt.Sprintf("  ✓ %d selected", n))
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render("   [Esc] back  [/] search  [1-7] sort  [Spc] select  [R] refresh")
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
			t.Blade,
			t.Service,
			formatEpoch(t.ActiveTime, m.cfg.UI.DateFormat),
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
		sgr := statusANSI(m.tasks[idx].State)
		if m.selected[m.tasks[idx].TID] {
			if sgr != "" {
				sgr += ";"
			}
			sgr += "48;5;23" // dark teal background for selected rows
		}
		if sgr != "" {
			lines[i] = "\x1b[" + sgr + "m" + lines[i] + "\x1b[m"
		}
	}
	return strings.Join(lines, "\n")
}

func (m DetailModel) makeColumns() []table.Column {
	widths := [dNumCols]int{}
	for i, n := range detailColumnNames {
		widths[i] = len(n)
	}
	if m.sortCol >= 0 && m.sortCol < dNumCols {
		widths[m.sortCol] += 2 // room for the ▲/▼ sort indicator
	}
	for _, t := range m.tasks {
		cells := [dNumCols]string{
			strconv.Itoa(t.TID),
			"", // TITLE fills remaining width
			t.State,
			progressPercent(t.Progress),
			t.Blade,
			t.Service,
			formatEpoch(t.ActiveTime, m.cfg.UI.DateFormat),
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

	header := func(col int) string {
		if col != m.sortCol {
			return detailColumnNames[col]
		}
		if m.sortAsc {
			return detailColumnNames[col] + " ▲"
		}
		return detailColumnNames[col] + " ▼"
	}
	cols := make([]table.Column, dNumCols)
	for col := range cols {
		cols[col] = table.Column{Title: header(col), Width: widths[col] + 2}
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
	h := m.height - 2 - tasksPanelHeight - 1 // panel + its join line
	if m.searching {
		h -= 3 // bordered search line
	}
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

// formatEpoch renders a Unix timestamp (seconds) using the UI date format,
// blank when there is no timestamp.
func formatEpoch(sec float64, format string) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(int64(sec), 0).Format(format)
}
