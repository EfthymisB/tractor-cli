package blades

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

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

type cmdsLoadedMsg struct {
	blade string
	cmds  []models.BladeCommand
}
type cmdsErrMsg struct{ err error }

const (
	dcolJID     = 0
	dcolTID     = 1
	dcolOwner   = 2
	dcolSlots   = 3
	dcolCommand = 4
	dNumCols    = 5
)

var detailColumnNames = [dNumCols]string{"JID", "TID", "OWNER", "SLOTS", "COMMAND"}

// DetailModel lists the commands currently running on a single blade.
type DetailModel struct {
	table   table.Model
	spinner spinner.Model
	search  textinput.Model

	blade   string // display name
	bladeID string // uuid used for queries
	allCmds []models.BladeCommand
	cmds    []models.BladeCommand

	searching bool
	sortCol   int
	sortAsc   bool

	loading bool
	err     error

	client *api.Client
	cfg    *config.Config
	width  int
	height int
}

// NewDetail creates a blade detail (command list) model.
func NewDetail(client *api.Client, cfg *config.Config) DetailModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

	ti := textinput.New()
	ti.Placeholder = "command/owner…"
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

	return DetailModel{
		table:   t,
		spinner: sp,
		search:  ti,
		sortCol: dcolJID,
		sortAsc: true,
		client:  client,
		cfg:     cfg,
	}
}

// Load points the view at a blade and fetches its running commands.
func (m DetailModel) Load(blade, bladeID string) (DetailModel, tea.Cmd) {
	m.blade = blade
	m.bladeID = bladeID
	m.allCmds = nil
	m.cmds = nil
	m.err = nil
	m.loading = true
	m.searching = false
	m.search.SetValue("")
	m.search.Blur()
	m.sortCol = dcolJID
	m.sortAsc = true
	m.table.SetRows(nil)
	return m, tea.Batch(m.fetch(), m.spinner.Tick)
}

// Refresh re-fetches the current blade's commands.
func (m DetailModel) Refresh() (DetailModel, tea.Cmd) {
	if m.bladeID == "" {
		return m, nil
	}
	m.loading = true
	return m, m.fetch()
}

func (m DetailModel) fetch() tea.Cmd {
	blade, bladeID := m.blade, m.bladeID
	return func() tea.Msg {
		cmds, err := m.client.BladeDetails(bladeID)
		if err != nil {
			return cmdsErrMsg{err}
		}
		return cmdsLoadedMsg{blade: blade, cmds: cmds}
	}
}

// Update handles messages for the blade detail view.
func (m DetailModel) Update(msg tea.Msg) (DetailModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case cmdsLoadedMsg:
		if msg.blade != m.blade {
			return m, nil
		}
		m.loading = false
		m.err = nil
		m.allCmds = msg.cmds
		m = m.refreshTable()
		return m, nil

	case cmdsErrMsg:
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
		case "1", "2", "3", "4", "5":
			col := int(msg.String()[0] - '1')
			if col >= 0 && col < dNumCols {
				if m.sortCol == col {
					m.sortAsc = !m.sortAsc
				} else {
					m.sortCol = col
					m.sortAsc = true
				}
				m = m.refreshTable()
			}
			return m, nil
		}
	}

	var tableCmd tea.Cmd
	m.table, tableCmd = m.table.Update(msg)
	cmds = append(cmds, tableCmd)
	return m, tea.Batch(cmds...)
}

// CapturingInput reports whether the search input is consuming keys.
func (m DetailModel) CapturingInput() bool { return m.searching }

// State accessors used by the root model.
func (m DetailModel) BladeName() string { return m.blade }
func (m DetailModel) CmdCount() int     { return len(m.cmds) }
func (m DetailModel) IsLoading() bool   { return m.loading }
func (m DetailModel) Err() error        { return m.err }

// View renders the blade detail view.
func (m DetailModel) View() string {
	if m.width == 0 {
		return ""
	}
	out := m.renderHeader() + "\n"
	if m.searching {
		out += styles.FilterBorder.Width(m.width-4).Render("Search: "+m.search.View()) + "\n"
	}
	return out + m.table.View()
}

// SetSize updates dimensions and resizes the table.
func (m DetailModel) SetSize(w, h int) DetailModel {
	m.width = w
	m.height = h
	m.table.SetColumns(m.makeColumns())
	m.table.SetHeight(m.viewportHeight())
	return m
}

// --- private helpers ---

func (m DetailModel) refreshTable() DetailModel {
	m.cmds = m.filterSort()
	m.table.SetColumns(m.makeColumns())
	m.table.SetRows(m.buildRows())
	return m
}

func (m DetailModel) filterSort() []models.BladeCommand {
	q := strings.ToLower(strings.TrimSpace(m.search.Value()))
	out := make([]models.BladeCommand, 0, len(m.allCmds))
	for _, c := range m.allCmds {
		if q == "" || strings.Contains(strings.ToLower(c.Command), q) ||
			strings.Contains(strings.ToLower(c.Owner), q) {
			out = append(out, c)
		}
	}
	col, asc := m.sortCol, m.sortAsc
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		var less bool
		switch col {
		case dcolJID:
			less = a.JID < b.JID
		case dcolTID:
			less = a.TID < b.TID
		case dcolOwner:
			less = strings.ToLower(a.Owner) < strings.ToLower(b.Owner)
		case dcolSlots:
			less = a.NumSlots < b.NumSlots
		case dcolCommand:
			less = strings.ToLower(a.Command) < strings.ToLower(b.Command)
		default:
			less = a.JID < b.JID
		}
		if asc {
			return less
		}
		return !less
	})
	return out
}

func (m DetailModel) buildRows() []table.Row {
	rows := make([]table.Row, 0, len(m.cmds))
	for _, c := range m.cmds {
		rows = append(rows, table.Row{
			strconv.Itoa(c.JID),
			strconv.Itoa(c.TID),
			c.Owner,
			strconv.Itoa(c.NumSlots),
			c.Command,
		})
	}
	return rows
}

func (m DetailModel) makeColumns() []table.Column {
	widths := [dNumCols]int{}
	for i, n := range detailColumnNames {
		widths[i] = len(n)
	}
	if m.sortCol >= 0 && m.sortCol < dNumCols {
		widths[m.sortCol] += 2
	}
	for _, c := range m.cmds {
		cells := [dNumCols]string{
			strconv.Itoa(c.JID),
			strconv.Itoa(c.TID),
			c.Owner,
			strconv.Itoa(c.NumSlots),
			"", // COMMAND fills remaining width
		}
		for col, cell := range cells {
			if col == dcolCommand {
				continue
			}
			if w := len([]rune(cell)); w > widths[col] {
				widths[col] = w
			}
		}
	}

	fixedTotal := 0
	for col, w := range widths {
		if col != dcolCommand {
			fixedTotal += w + 2
		}
	}
	cmdContent := m.width - fixedTotal - 2
	if cmdContent < 10 {
		cmdContent = 10
	}
	widths[dcolCommand] = cmdContent

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

func (m DetailModel) renderHeader() string {
	title := "Blade " + m.blade
	if m.loading {
		title += "  " + m.spinner.View()
	} else if len(m.cmds) != len(m.allCmds) {
		title += fmt.Sprintf("  (%d/%d cmds)", len(m.cmds), len(m.allCmds))
	} else {
		title += fmt.Sprintf("  (%d cmds)", len(m.cmds))
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render("   [Esc] back  [/] search  [1-5] sort  [R] refresh")
	return styles.Title.Render(title) + hint
}

func (m DetailModel) viewportHeight() int {
	h := m.height - 2
	if m.searching {
		h -= 3
	}
	if h < 1 {
		h = 1
	}
	return h
}
