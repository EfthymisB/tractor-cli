// Package blades provides the blades list TUI view (Phase 5).
package blades

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

type bladesLoadedMsg struct{ blades []models.Blade }
type bladesErrMsg struct{ err error }

// upWindow is how recent a heartbeat must be for a blade to count as "up".
const upWindow = 6 * time.Minute

const bladesPanelHeight = 4

const (
	bcolName    = 0
	bcolStatus  = 1
	bcolNimby   = 2
	bcolSlots   = 3
	bcolCPU     = 4
	bcolMem     = 5
	bcolDisk    = 6
	bcolTasks   = 7
	bcolProfile = 8
	bcolOwners  = 9
	bNumCols    = 10
)

var bladeColumnNames = [bNumCols]string{
	"NAME", "STATUS", "NIMBY", "SLOTS", "CPU", "MEM", "DISK", "TASKS", "PROFILE", "OWNERS",
}

var bladeColumnCaps = [bNumCols]int{0, 0, 0, 0, 0, 0, 0, 0, 20, 24}

// Model is the blades list view.
type Model struct {
	table   table.Model
	spinner spinner.Model
	search  textinput.Model

	allBlades []models.Blade
	blades    []models.Blade

	searching bool
	sortCol   int
	sortAsc   bool

	loading     bool
	lastRefresh time.Time
	err         error
	topRow      int

	client *api.Client
	cfg    *config.Config
	width  int
	height int
}

// New creates a blades list model.
func New(client *api.Client, cfg *config.Config) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

	ti := textinput.New()
	ti.Placeholder = "name…"
	ti.Prompt = ""
	ti.CharLimit = 128

	t := table.New(table.WithFocused(true))
	s := table.DefaultStyles()
	s.Header = s.Header.BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).BorderBottom(true).Bold(true)
	s.Selected = s.Selected.Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).Bold(false)
	s.Cell = lipgloss.NewStyle().Padding(0, 1)
	t.SetStyles(s)

	return Model{
		table:   t,
		spinner: sp,
		search:  ti,
		sortCol: bcolName,
		sortAsc: true,
		client:  client,
		cfg:     cfg,
	}
}

// Refresh re-fetches the blade list.
func (m Model) Refresh() (Model, tea.Cmd) {
	m.loading = true
	return m, tea.Batch(m.fetch(), m.spinner.Tick)
}

func (m Model) fetch() tea.Cmd {
	return func() tea.Msg {
		blades, err := m.client.QueryBlades()
		if err != nil {
			return bladesErrMsg{err}
		}
		return bladesLoadedMsg{blades}
	}
}

// Update handles messages for the blades view.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case bladesLoadedMsg:
		m.loading = false
		m.err = nil
		m.lastRefresh = time.Now()
		m.allBlades = msg.blades
		m.topRow = 0
		m = m.refreshTable()
		return m, nil

	case bladesErrMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
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
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m = m.refreshTable()
			return m, cmd
		}

		switch msg.String() {
		case "/":
			m.searching = true
			m.table.SetHeight(m.viewportHeight())
			return m, m.search.Focus()
		case "R":
			return m.Refresh()
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			col := int(msg.String()[0] - '1')
			if col >= 0 && col < bNumCols {
				if m.sortCol == col {
					m.sortAsc = !m.sortAsc
				} else {
					m.sortCol = col
					m.sortAsc = true
				}
				m.topRow = 0
				m = m.refreshTable()
			}
			return m, nil
		}
	}

	var tableCmd tea.Cmd
	m.table, tableCmd = m.table.Update(msg)
	cmds = append(cmds, tableCmd)
	m = m.syncTopRow()
	return m, tea.Batch(cmds...)
}

// CapturingInput reports whether the name-search input is consuming keys.
func (m Model) CapturingInput() bool { return m.searching }

// State accessors used by the root model.
func (m Model) BladeCount() int          { return len(m.blades) }
func (m Model) IsLoading() bool          { return m.loading }
func (m Model) LastRefresh() time.Time   { return m.lastRefresh }
func (m Model) Err() error               { return m.err }

// SelectedBlade returns the highlighted blade, if any.
func (m Model) SelectedBlade() (models.Blade, bool) {
	c := m.table.Cursor()
	if c < 0 || c >= len(m.blades) {
		return models.Blade{}, false
	}
	return m.blades[c], true
}

// View renders the blades view.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	out := m.renderHeader() + "\n"
	if m.searching {
		out += styles.FilterBorder.Width(m.width-4).Render("Search name: "+m.search.View()) + "\n"
	}
	return out + m.colorize(m.table.View()) + "\n" + m.renderPanel()
}

// SetSize updates dimensions and resizes the table.
func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	m.table.SetColumns(m.makeColumns())
	m.table.SetHeight(m.viewportHeight())
	return m
}

// --- private helpers ---

func (m Model) refreshTable() Model {
	m.blades = m.filterSort()
	m.table.SetColumns(m.makeColumns())
	m.table.SetRows(m.buildRows())
	return m
}

func (m Model) filterSort() []models.Blade {
	q := strings.ToLower(strings.TrimSpace(m.search.Value()))
	out := make([]models.Blade, 0, len(m.allBlades))
	for _, b := range m.allBlades {
		if q == "" || strings.Contains(strings.ToLower(b.Name), q) {
			out = append(out, b)
		}
	}
	col, asc := m.sortCol, m.sortAsc
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		var less bool
		switch col {
		case bcolName:
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case bcolStatus:
			less = bladeStatus(a) < bladeStatus(b)
		case bcolNimby:
			less = boolToInt(isNimbied(a)) < boolToInt(isNimbied(b))
		case bcolSlots:
			less = a.NumSlots < b.NumSlots
		case bcolCPU:
			less = a.LoadAvg < b.LoadAvg
		case bcolMem:
			less = a.MemGB < b.MemGB
		case bcolDisk:
			less = a.DiskGB < b.DiskGB
		case bcolTasks:
			less = a.TaskCount < b.TaskCount
		case bcolProfile:
			less = strings.ToLower(a.Profile) < strings.ToLower(b.Profile)
		case bcolOwners:
			less = strings.Join(a.Owners, ",") < strings.Join(b.Owners, ",")
		default:
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		if asc {
			return less
		}
		return !less
	})
	return out
}

func (m Model) buildRows() []table.Row {
	rows := make([]table.Row, 0, len(m.blades))
	for _, b := range m.blades {
		rows = append(rows, table.Row{
			b.Name,
			bladeStatus(b),
			nimbyCell(b),
			fmt.Sprintf("%d/%d", b.SlotsInUse, b.NumSlots),
			fmt.Sprintf("%.2f", b.LoadAvg),
			fmt.Sprintf("%.1f", b.MemGB),
			fmt.Sprintf("%.1f", b.DiskGB),
			strconv.Itoa(b.TaskCount),
			b.Profile,
			strings.Join(b.Owners, ","),
		})
	}
	return rows
}

func (m Model) colorize(tableStr string) string {
	lines := strings.Split(tableStr, "\n")
	for i := range lines {
		if i < 2 {
			continue
		}
		idx := m.topRow + (i - 2)
		if idx < 0 || idx >= len(m.blades) {
			continue
		}
		if idx == m.table.Cursor() {
			continue
		}
		if ansi := bladeANSI(m.blades[idx]); ansi != "" {
			lines[i] = "\x1b[" + ansi + "m" + lines[i] + "\x1b[m"
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) makeColumns() []table.Column {
	widths := [bNumCols]int{}
	for i, n := range bladeColumnNames {
		widths[i] = len(n)
	}
	if m.sortCol >= 0 && m.sortCol < bNumCols {
		widths[m.sortCol] += 2
	}
	for _, b := range m.blades {
		cells := [bNumCols]string{
			b.Name,
			bladeStatus(b),
			nimbyCell(b),
			fmt.Sprintf("%d/%d", b.SlotsInUse, b.NumSlots),
			fmt.Sprintf("%.2f", b.LoadAvg),
			fmt.Sprintf("%.1f", b.MemGB),
			fmt.Sprintf("%.1f", b.DiskGB),
			strconv.Itoa(b.TaskCount),
			b.Profile,
			strings.Join(b.Owners, ","),
		}
		for col, cell := range cells {
			if w := len([]rune(cell)); w > widths[col] {
				widths[col] = w
			}
		}
	}
	for col := range widths {
		if bladeColumnCaps[col] > 0 && widths[col] > bladeColumnCaps[col] {
			widths[col] = bladeColumnCaps[col]
		}
	}

	header := func(col int) string {
		if col != m.sortCol {
			return bladeColumnNames[col]
		}
		if m.sortAsc {
			return bladeColumnNames[col] + " ▲"
		}
		return bladeColumnNames[col] + " ▼"
	}
	cols := make([]table.Column, bNumCols)
	for col := range cols {
		cols[col] = table.Column{Title: header(col), Width: widths[col] + 2}
	}
	return cols
}

func (m Model) renderHeader() string {
	title := "Blades"
	if m.loading {
		title += "  " + m.spinner.View()
	} else if len(m.blades) != len(m.allBlades) {
		title += fmt.Sprintf(" (%d/%d)", len(m.blades), len(m.allBlades))
	} else {
		title += fmt.Sprintf(" (%d)", len(m.blades))
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render("   [Esc] back  [/] search  [1-9] sort  [a] actions  [R] refresh")
	return styles.Title.Render(title) + hint
}

func (m Model) renderPanel() string {
	var lines []string
	if b, ok := m.SelectedBlade(); ok {
		lines = []string{
			fmt.Sprintf("Addr: %s:%d    OS: %s %s    GPU: %s    Ver: %s",
				b.Addr, b.Port, b.OSName, b.OSVersion, orDash(b.GPULabel), b.Version),
			fmt.Sprintf("UDI: %.0f    CPUs: %d    Heartbeat: %s    Up since: %s    Note: %s",
				b.UDI, b.NumCPU, formatEpoch(b.Heartbeat, m.cfg.UI.DateFormat),
				formatEpoch(b.StartTime, m.cfg.UI.DateFormat), orDash(b.Note)),
		}
	} else {
		lines = []string{"—"}
	}
	for i := range lines {
		lines[i] = truncRunes(lines[i], m.width-2)
	}
	for len(lines) < 2 {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(m.width - 2).
		Render(strings.Join(lines[:2], "\n"))
}

func (m Model) syncTopRow() Model {
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

func (m Model) viewportHeight() int {
	h := m.height - 2 - bladesPanelHeight - 1
	if m.searching {
		h -= 3
	}
	if h < 1 {
		h = 1
	}
	return h
}

// --- blade status helpers ---

func bladeUp(b models.Blade) bool {
	if b.Heartbeat <= 0 {
		return false
	}
	return time.Since(time.Unix(int64(b.Heartbeat), 0)) < upWindow
}

func bladeStatus(b models.Blade) string {
	if bladeUp(b) {
		return "up"
	}
	return "down"
}

func isNimbied(b models.Blade) bool {
	n := strings.TrimSpace(b.Nimby)
	return n != "" && n != "0"
}

func nimbyCell(b models.Blade) string {
	if isNimbied(b) {
		return "nimby"
	}
	return ""
}

// bladeANSI returns the row colour: down=grey, nimby=yellow, up=green.
func bladeANSI(b models.Blade) string {
	switch {
	case !bladeUp(b):
		return "38;5;240" // grey
	case isNimbied(b):
		return "38;5;11" // yellow
	default:
		return "38;5;10" // green
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if n < 0 {
		n = 0
	}
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func formatEpoch(sec float64, format string) string {
	if sec <= 0 {
		return "—"
	}
	return time.Unix(int64(sec), 0).Format(format)
}
