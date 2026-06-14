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

	heatmap  bool // 'g' — show the blade heatmap grid
	selIndex int  // selected cell in heatmap mode

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

		// Heatmap-mode grid navigation.
		if m.heatmap {
			switch msg.String() {
			case "left", "h":
				m.selIndex--
				return m.clampSel(), nil
			case "right", "l":
				m.selIndex++
				return m.clampSel(), nil
			case "up", "k":
				m.selIndex -= m.heatCols()
				return m.clampSel(), nil
			case "down", "j":
				m.selIndex += m.heatCols()
				return m.clampSel(), nil
			}
		}

		switch msg.String() {
		case "g":
			// Toggle the heatmap grid; keep the selection in sync.
			m.heatmap = !m.heatmap
			if m.heatmap {
				m.selIndex = m.table.Cursor()
			} else {
				m.table.SetCursor(m.selIndex)
			}
			return m.clampSel(), nil
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

// Heatmap reports whether the heatmap grid is showing.
func (m Model) Heatmap() bool { return m.heatmap }

// ExitHeatmap switches back to the table view, syncing the cursor.
func (m Model) ExitHeatmap() Model {
	if m.heatmap {
		m.heatmap = false
		m.table.SetCursor(m.selIndex)
	}
	return m
}

// State accessors used by the root model.
func (m Model) BladeCount() int          { return len(m.blades) }
func (m Model) IsLoading() bool          { return m.loading }
func (m Model) LastRefresh() time.Time   { return m.lastRefresh }
func (m Model) Err() error               { return m.err }

// SelectedBlade returns the highlighted blade, if any.
func (m Model) SelectedBlade() (models.Blade, bool) {
	c := m.table.Cursor()
	if m.heatmap {
		c = m.selIndex
	}
	if c < 0 || c >= len(m.blades) {
		return models.Blade{}, false
	}
	return m.blades[c], true
}

// View renders the blades view (table or heatmap).
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.heatmap {
		return m.renderHeatmap()
	}
	out := m.renderHeader() + "\n"
	if m.searching {
		out += styles.FilterBorder.Width(m.width-4).Render("Search name: "+m.search.View()) + "\n"
	}
	return out + m.colorize(m.table.View()) + "\n" + m.renderPanel()
}

func (m Model) clampSel() Model {
	if m.selIndex < 0 {
		m.selIndex = 0
	}
	if m.selIndex >= len(m.blades) {
		m.selIndex = len(m.blades) - 1
	}
	if m.selIndex < 0 {
		m.selIndex = 0
	}
	return m
}

func (m Model) heatCols() int {
	c := m.width / 3 // each cell is "██" + a space
	if c < 1 {
		c = 1
	}
	return c
}

// renderHeatmap draws a grid of blades, one coloured cell each.
func (m Model) renderHeatmap() string {
	title := fmt.Sprintf("Blades — heatmap (%d)", len(m.blades))
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render("   [g] grid/table  ←↑↓→ move  [Enter] detail  [a] actions  [t] trace  [Esc] back")
	head := styles.Title.Render(title) + hint
	if len(m.blades) == 0 {
		return head + "\n\n(no blades)"
	}

	cols := m.heatCols()
	var b strings.Builder
	for i, bl := range m.blades {
		b.WriteString(heatCell(bl, i == m.selIndex))
		if (i+1)%cols == 0 {
			b.WriteByte('\n')
		} else {
			b.WriteByte(' ')
		}
	}

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	var sel string
	if m.selIndex >= 0 && m.selIndex < len(m.blades) {
		bl := m.blades[m.selIndex]
		pct := 0
		if bl.NumSlots > 0 {
			pct = bl.SlotsInUse * 100 / bl.NumSlots
		}
		sel = fmt.Sprintf("%s   %s   slots %d/%d (%d%%)   load %.2f   mem %.1f   %s",
			bl.Name, bladeStatus(bl), bl.SlotsInUse, bl.NumSlots, pct, bl.LoadAvg, bl.MemGB, nimbyCell(bl))
	}
	cell := func(c string) string { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("██") }
	legend := dim.Render("idle ") + cell("10") + dim.Render("  busy ") + cell("9") +
		dim.Render("  nimby ") + cell("11") + dim.Render("  down ") + cell("240")

	return head + "\n\n" + b.String() + "\n\n" + sel + "\n" + legend
}

// heatCell renders one blade as a coloured block (selected = reversed).
func heatCell(b models.Blade, selected bool) string {
	st := lipgloss.NewStyle().Foreground(lipgloss.Color(bladeHeatColor(b)))
	if selected {
		st = st.Reverse(true)
	}
	return st.Render("██")
}

// bladeHeatColor maps a blade to a heat colour: grey down, yellow nimby,
// otherwise green→orange→red by slot utilisation.
func bladeHeatColor(b models.Blade) string {
	if !bladeUp(b) {
		return "240"
	}
	if isNimbied(b) {
		return "11"
	}
	pct := 0
	if b.NumSlots > 0 {
		pct = b.SlotsInUse * 100 / b.NumSlots
	}
	switch {
	case pct < 34:
		return "10" // green (idle)
	case pct < 67:
		return "11" // yellow
	case pct < 90:
		return "208" // orange
	default:
		return "9" // red (full)
	}
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
