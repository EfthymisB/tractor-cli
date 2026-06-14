// Package tui is the root bubbletea model for tractor-cli.
package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
	"github.com/tractor-cli/tractor-cli/internal/state"
	"github.com/tractor-cli/tractor-cli/internal/tui/styles"
	"github.com/tractor-cli/tractor-cli/internal/tui/views/action"
	"github.com/tractor-cli/tractor-cli/internal/tui/views/blades"
	"github.com/tractor-cli/tractor-cli/internal/tui/views/jobs"
	"github.com/tractor-cli/tractor-cli/internal/tui/views/logview"
)

type tickMsg time.Time

// View identifies which screen is active.
type View int

const (
	ViewJobsList View = iota
	ViewJobDetail
	ViewTaskLog
	ViewBladesList
	ViewBladeDetail
)

// Model is the root bubbletea model.
type Model struct {
	activeView View

	jobsList   jobs.ListModel
	detail     jobs.DetailModel
	logView     logview.Model
	blades      blades.Model
	bladeDetail blades.DetailModel
	actionMenu  action.Model

	client *api.Client
	cfg    *config.Config

	spinner spinner.Model

	showHelp  bool
	actionMsg string // last action result, shown in the status bar
	actionErr bool

	// notes overlay
	showNotes    bool
	notesJID     int
	notesTitle   string
	notesText    string
	notesLoading bool
	notesErr     error

	// blade trace overlay
	showTrace    bool
	traceTitle   string
	traceVP      viewport.Model
	traceReady   bool
	traceLoading bool
	traceErr     error

	// persistence + saved filters
	stateData      state.State
	showSaveFilter bool
	saveInput      textinput.Model
	showLoadFilter bool
	loadNames      []string
	loadCursor     int

	width, height int
}

// New creates and returns the root TUI model.
func New(client *api.Client, cfg *config.Config) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

	st := state.Load()
	jl := jobs.New(client, cfg).
		ApplyLayout(st.TitleWidth, st.SortCol, st.SortAsc, st.RelTime)

	saveIn := textinput.New()
	saveIn.Prompt = ""
	saveIn.CharLimit = 40
	saveIn.Placeholder = "filter name"

	return Model{
		activeView:  ViewJobsList,
		jobsList:    jl,
		detail:      jobs.NewDetail(client, cfg),
		logView:     logview.New(client, cfg),
		blades:      blades.New(client, cfg),
		bladeDetail: blades.NewDetail(client, cfg),
		actionMenu:  action.New(),
		stateData:   st,
		saveInput:   saveIn,
		client:      client,
		cfg:         cfg,
		spinner:     sp,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.jobsList.Init(),
		m.spinner.Tick,
		m.fetchFarmStats(),
		scheduleTick(time.Duration(m.cfg.UI.RefreshInterval)*time.Second),
	)
}

// farmStatsMsg carries farm-wide slot/blade totals for the jobs summary line.
type farmStatsMsg struct {
	slotsInUse, slotsTotal, bladesUp, bladesTotal int
}

// fetchFarmStats queries all blades to compute slot utilisation. Errors are
// ignored (the summary just omits the slot figures).
func (m Model) fetchFarmStats() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		bl, err := client.QueryBlades()
		if err != nil {
			return farmStatsMsg{}
		}
		var inUse, total, up int
		now := time.Now()
		for _, b := range bl {
			total += b.NumSlots
			inUse += b.SlotsInUse
			if b.Heartbeat > 0 && now.Sub(time.Unix(int64(b.Heartbeat), 0)) < 6*time.Minute {
				up++
			}
		}
		return farmStatsMsg{slotsInUse: inUse, slotsTotal: total, bladesUp: up, bladesTotal: len(bl)}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Give each child everything except the bottom status bar line.
		m.jobsList = m.jobsList.SetSize(m.width, m.height-1)
		m.detail = m.detail.SetSize(m.width, m.height-1)
		m.logView = m.logView.SetSize(m.width, m.height-1)
		m.blades = m.blades.SetSize(m.width, m.height-1)
		m.bladeDetail = m.bladeDetail.SetSize(m.width, m.height-1)
		if !m.traceReady {
			m.traceVP = viewport.New(m.width, m.traceViewportHeight())
			m.traceReady = true
		} else {
			m.traceVP.Width = m.width
			m.traceVP.Height = m.traceViewportHeight()
		}
		return m, nil

	case action.ChosenMsg:
		return m, m.runAction(msg)

	case action.CancelMsg:
		return m, nil

	case tea.MouseMsg:
		// Wheel scrolling: the trace/log viewports handle the mouse natively;
		// for tables we translate a wheel tick into an up/down key.
		up := msg.Button == tea.MouseButtonWheelUp
		down := msg.Button == tea.MouseButtonWheelDown
		if !up && !down {
			return m, nil
		}
		if m.showTrace {
			var cmd tea.Cmd
			m.traceVP, cmd = m.traceVP.Update(msg)
			return m, cmd
		}
		if m.activeView == ViewTaskLog {
			var cmd tea.Cmd
			m.logView, cmd = m.logView.Update(msg)
			return m, cmd
		}
		return m.scrollActive(up)

	case farmStatsMsg:
		m.jobsList = m.jobsList.SetFarmStats(
			msg.slotsInUse, msg.slotsTotal, msg.bladesUp, msg.bladesTotal)
		return m, nil

	case jobs.WatchMsg:
		m.actionErr = msg.Status == "error"
		verb := "✔ finished"
		if msg.Status == "error" {
			verb = "✗ errored"
		}
		m.actionMsg = fmt.Sprintf("watched job %d %s — %s", msg.JID, verb, msg.Title)
		return m, bellCmd()

	case traceLoadedMsg:
		m.traceLoading = false
		m.traceErr = nil
		if m.traceReady {
			m.traceVP.SetContent(msg.text)
			m.traceVP.GotoTop()
		}
		return m, nil

	case traceErrMsg:
		m.traceLoading = false
		m.traceErr = msg.err
		if m.traceReady {
			m.traceVP.SetContent(styles.ErrorStyle.Render("Error: " + msg.err.Error()))
		}
		return m, nil

	case notesLoadedMsg:
		if msg.jid == m.notesJID {
			m.notesText = msg.text
			m.notesLoading = false
		}
		return m, nil

	case notesErrMsg:
		if msg.jid == m.notesJID {
			m.notesErr = msg.err
			m.notesLoading = false
		}
		return m, nil

	case actionDoneMsg:
		m.actionErr = msg.err != nil
		if msg.err != nil {
			m.actionMsg = "✗ " + msg.op + ": " + msg.err.Error()
		} else {
			m.actionMsg = "✓ " + msg.op + " — " + msg.label
		}
		// Reflect the change by refreshing the active view, and clear any
		// multi-selection (the bulk action consumed it).
		var refresh tea.Cmd
		switch m.activeView {
		case ViewJobDetail:
			m.detail = m.detail.ClearSelection()
			m.detail, refresh = m.detail.Refresh()
		case ViewBladesList:
			m.blades, refresh = m.blades.Refresh()
		default:
			m.jobsList = m.jobsList.ClearSelection()
			m.jobsList, refresh = m.jobsList.Refresh()
		}
		return m, refresh

	case tea.KeyMsg:
		// While the help overlay is up it owns the keyboard: any key dismisses
		// it (ctrl+c still quits) and nothing reaches the list underneath.
		if m.showHelp {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.showHelp = false
			return m, nil
		}

		// The notes overlay is dismissed by any key.
		if m.showNotes {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.showNotes = false
			return m, nil
		}

		// The trace overlay is scrollable; Esc/q close it.
		if m.showTrace {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "q":
				m.showTrace = false
				return m, nil
			}
			var cmd tea.Cmd
			m.traceVP, cmd = m.traceVP.Update(msg)
			return m, cmd
		}

		// Save-filter overlay (name input).
		if m.showSaveFilter {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.showSaveFilter = false
				m.saveInput.Blur()
				return m, nil
			case "enter":
				if name := strings.TrimSpace(m.saveInput.Value()); name != "" {
					if m.stateData.Filters == nil {
						m.stateData.Filters = map[string]api.JobFilter{}
					}
					m.stateData.Filters[name] = m.jobsList.CurrentFilter()
					m.saveState()
					m.actionErr = false
					m.actionMsg = "saved filter: " + name
				}
				m.showSaveFilter = false
				m.saveInput.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.saveInput, cmd = m.saveInput.Update(msg)
			return m, cmd
		}

		// Load-filter overlay (pick a saved filter).
		if m.showLoadFilter {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.showLoadFilter = false
				return m, nil
			case "up", "k":
				if m.loadCursor > 0 {
					m.loadCursor--
				}
				return m, nil
			case "down", "j":
				if m.loadCursor < len(m.loadNames)-1 {
					m.loadCursor++
				}
				return m, nil
			case "d":
				if m.loadCursor < len(m.loadNames) {
					delete(m.stateData.Filters, m.loadNames[m.loadCursor])
					m.saveState()
					m.loadNames = m.stateData.FilterNames()
					if m.loadCursor >= len(m.loadNames) {
						m.loadCursor = len(m.loadNames) - 1
					}
					if len(m.loadNames) == 0 {
						m.showLoadFilter = false
					}
				}
				return m, nil
			case "enter":
				if m.loadCursor < len(m.loadNames) {
					name := m.loadNames[m.loadCursor]
					m.showLoadFilter = false
					var cmd tea.Cmd
					m.jobsList, cmd = m.jobsList.ApplyFilter(m.stateData.Filters[name])
					m.actionErr = false
					m.actionMsg = "applied filter: " + name
					return m, cmd
				}
				m.showLoadFilter = false
				return m, nil
			}
			return m, nil
		}

		// The action menu is modal: route all keys to it while open.
		if m.actionMenu.Active() {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			var cmd tea.Cmd
			m.actionMenu, cmd = m.actionMenu.Update(msg)
			return m, cmd
		}

		// A child view is "capturing" when it has a focused text input (the
		// filter form, or the task-search box) that should receive raw keys.
		capturing := m.capturingInput()

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			// Don't quit while an input is capturing keystrokes — "q" may be
			// part of a username or title being typed.
			if capturing {
				break
			}
			m.saveState()
			return m, tea.Quit
		case "?":
			// Open help, unless an input wants the literal "?".
			if !capturing {
				m.showHelp = true
				return m, nil
			}
		case "enter":
			if capturing {
				break
			}
			switch m.activeView {
			case ViewJobsList:
				// Open the selected job's task detail view.
				if jid := m.jobsList.SelectedJID(); jid > 0 {
					m.activeView = ViewJobDetail
					var cmd tea.Cmd
					m.detail, cmd = m.detail.Load(jid, m.jobsList.SelectedTitle(), m.jobsList.SelectedOwner())
					return m, cmd
				}
			case ViewJobDetail:
				// Open the highlighted task's log.
				if tid := m.detail.SelectedTID(); tid > 0 {
					m.activeView = ViewTaskLog
					var cmd tea.Cmd
					m.logView, cmd = m.logView.Load(
						m.detail.JID(), tid, m.detail.Owner(),
						m.detail.JobTitle(), m.detail.TitleForTID(tid))
					return m, cmd
				}
			case ViewBladesList:
				// Open the highlighted blade's running commands.
				if bl, ok := m.blades.SelectedBlade(); ok {
					m.activeView = ViewBladeDetail
					var cmd tea.Cmd
					m.bladeDetail, cmd = m.bladeDetail.Load(bl.Name, bl.BladeID)
					return m, cmd
				}
			}
		case "]", "[":
			// Jump to the next/previous sibling: task log in the log view,
			// job in the task view.
			if capturing {
				break
			}
			dir := 1
			if msg.String() == "[" {
				dir = -1
			}
			switch m.activeView {
			case ViewTaskLog:
				next := m.detail.AdjacentTID(m.logView.TID(), dir)
				if next > 0 {
					m.detail = m.detail.SelectTID(next)
					var cmd tea.Cmd
					m.logView, cmd = m.logView.Load(
						m.detail.JID(), next, m.detail.Owner(),
						m.detail.JobTitle(), m.detail.TitleForTID(next))
					return m, cmd
				}
				return m, nil
			case ViewJobDetail:
				next := m.jobsList.AdjacentJID(m.detail.JID(), dir)
				if next > 0 {
					m.jobsList = m.jobsList.SelectJID(next)
					job, _ := m.jobsList.JobByJID(next)
					var cmd tea.Cmd
					m.detail, cmd = m.detail.Load(next, job.Title, job.Owner)
					return m, cmd
				}
				return m, nil
			}
		case "s":
			// Save the current filter under a name.
			if !capturing && m.activeView == ViewJobsList {
				m.showSaveFilter = true
				m.saveInput.SetValue("")
				return m, m.saveInput.Focus()
			}
		case "f":
			// Recall a saved filter.
			if !capturing && m.activeView == ViewJobsList {
				m.loadNames = m.stateData.FilterNames()
				if len(m.loadNames) == 0 {
					m.actionErr = false
					m.actionMsg = "no saved filters"
					return m, nil
				}
				m.showLoadFilter = true
				m.loadCursor = 0
				return m, nil
			}
		case "y":
			// Yank a context value to the clipboard (OSC 52 — works over SSH).
			if !capturing {
				text := ""
				switch m.activeView {
				case ViewJobsList:
					if jid := m.jobsList.SelectedJID(); jid > 0 {
						text = strconv.Itoa(jid)
					}
				case ViewJobDetail:
					if cmd := m.detail.PanelCommand(); cmd != "" {
						text = cmd
					} else if tid := m.detail.SelectedTID(); tid > 0 {
						text = fmt.Sprintf("%d/%d", m.detail.JID(), tid)
					}
				case ViewBladesList:
					if bl, ok := m.blades.SelectedBlade(); ok {
						text = bl.Name
					}
				case ViewBladeDetail:
					text = m.bladeDetail.BladeName()
				}
				if text != "" {
					m.actionErr = false
					m.actionMsg = "copied: " + truncatePlain(text, 60)
					if !hasClipboardTool() {
						m.actionErr = true
						m.actionMsg = "copied (OSC52) — install xclip if paste doesn't work: " + truncatePlain(text, 40)
					}
					return m, yankCmd(text)
				}
			}
		case "o":
			// Open the current log in $PAGER.
			if !capturing && m.activeView == ViewTaskLog {
				return m, openInPager(m.logView.RawText())
			}
		case "n":
			// Show the selected job's notes overlay.
			if !capturing {
				jid, title := 0, ""
				switch m.activeView {
				case ViewJobsList:
					if job, ok := m.jobsList.SelectedJob(); ok {
						jid, title = job.JID, fmt.Sprintf("Job %d — %s", job.JID, job.Title)
					}
				case ViewJobDetail:
					jid = m.detail.JID()
					title = fmt.Sprintf("Job %d %s", jid, parenTitle(m.detail.JobTitle()))
				}
				if jid > 0 {
					m.showNotes = true
					m.notesJID = jid
					m.notesTitle = title
					m.notesText = ""
					m.notesErr = nil
					m.notesLoading = true
					return m, m.fetchNotes(jid)
				}
			}
		case "t":
			// Show the engine's trace for the highlighted blade.
			if !capturing && m.activeView == ViewBladesList {
				if bl, ok := m.blades.SelectedBlade(); ok {
					m.showTrace = true
					m.traceTitle = bl.Name
					m.traceLoading = true
					m.traceErr = nil
					if m.traceReady {
						m.traceVP.SetContent("Loading…")
						m.traceVP.GotoTop()
					}
					return m, m.fetchTrace(bl.Name, bl.Addr)
				}
			}
		case "b":
			// Toggle into the blades view from the jobs list.
			if !capturing && m.activeView == ViewJobsList {
				m.activeView = ViewBladesList
				var cmd tea.Cmd
				m.blades, cmd = m.blades.Refresh()
				return m, cmd
			}
		case "a":
			// Open the action menu for the current job/task/blade.
			if !capturing {
				switch m.activeView {
				case ViewJobsList:
					if n := m.jobsList.SelectionCount(); n > 0 {
						m.actionMenu = m.actionMenu.OpenJobsBulk(
							m.jobsList.SelectedJIDs(), fmt.Sprintf("%d jobs", n))
						return m, nil
					}
					if job, ok := m.jobsList.SelectedJob(); ok {
						label := fmt.Sprintf("Job %d — %s", job.JID, job.Title)
						m.actionMenu = m.actionMenu.OpenJob(job.JID, label, job.Status == "paused", job.Locked)
						return m, nil
					}
				case ViewJobDetail:
					if n := m.detail.SelectionCount(); n > 0 {
						m.actionMenu = m.actionMenu.OpenTasksBulk(
							m.detail.JID(), m.detail.SelectedTIDs(), fmt.Sprintf("%d tasks", n))
						return m, nil
					}
					if tid := m.detail.SelectedTID(); tid > 0 {
						label := fmt.Sprintf("Task %d (Job %d)", tid, m.detail.JID())
						m.actionMenu = m.actionMenu.OpenTask(m.detail.JID(), tid, label)
						return m, nil
					}
				case ViewBladesList:
					if bl, ok := m.blades.SelectedBlade(); ok {
						m.actionMenu = m.actionMenu.OpenBlade(bl.Name, bl.Addr, "Blade "+bl.Name, isBladeNimbied(bl.Nimby))
						return m, nil
					}
				}
			}
		case "esc":
			// Walk back one view level, unless a search box is open (the view
			// handles Esc itself to clear its search).
			if !capturing {
				switch m.activeView {
				case ViewTaskLog:
					m.activeView = ViewJobDetail
					var cmd tea.Cmd
					m.detail, cmd = m.detail.RefreshPanel()
					return m, cmd
				case ViewJobDetail:
					// Clear a task selection first; otherwise go back.
					if m.detail.SelectionCount() > 0 {
						m.detail = m.detail.ClearSelection()
						return m, nil
					}
					m.activeView = ViewJobsList
					return m, nil
				case ViewBladesList:
					// Esc exits the heatmap first; otherwise back to jobs.
					if m.blades.Heatmap() {
						m.blades = m.blades.ExitHeatmap()
						return m, nil
					}
					m.activeView = ViewJobsList
					return m, nil
				case ViewBladeDetail:
					m.activeView = ViewBladesList
					return m, nil
				}
			}
		}

	case tickMsg:
		// Auto-refresh whichever view is in front.
		var refreshCmd tea.Cmd
		switch m.activeView {
		case ViewJobDetail:
			m.detail, refreshCmd = m.detail.Refresh()
		case ViewBladesList:
			m.blades, refreshCmd = m.blades.Refresh()
		case ViewBladeDetail:
			m.bladeDetail, refreshCmd = m.bladeDetail.Refresh()
		default:
			m.jobsList, refreshCmd = m.jobsList.Refresh()
		}
		cmds = append(cmds, refreshCmd, m.fetchFarmStats())
		if m.cfg.UI.RefreshInterval > 0 {
			cmds = append(cmds, scheduleTick(time.Duration(m.cfg.UI.RefreshInterval)*time.Second))
		}
		return m, tea.Batch(cmds...)
	}

	// Delegate everything else to the active child model.
	var childCmd tea.Cmd
	switch m.activeView {
	case ViewTaskLog:
		m.logView, childCmd = m.logView.Update(msg)
	case ViewJobDetail:
		m.detail, childCmd = m.detail.Update(msg)
	case ViewBladesList:
		m.blades, childCmd = m.blades.Update(msg)
	case ViewBladeDetail:
		m.bladeDetail, childCmd = m.bladeDetail.Update(msg)
	default:
		m.jobsList, childCmd = m.jobsList.Update(msg)
	}
	cmds = append(cmds, childCmd)

	// Keep the status-bar spinner animated.
	var spinCmd tea.Cmd
	m.spinner, spinCmd = m.spinner.Update(msg)
	cmds = append(cmds, spinCmd)

	return m, tea.Batch(cmds...)
}

// capturingInput reports whether the active view has a focused text input that
// should receive raw keystrokes (filter form or task search).
func (m Model) capturingInput() bool {
	switch m.activeView {
	case ViewTaskLog:
		return m.logView.CapturingInput()
	case ViewJobDetail:
		return m.detail.CapturingInput()
	case ViewBladesList:
		return m.blades.CapturingInput()
	case ViewBladeDetail:
		return m.bladeDetail.CapturingInput()
	default:
		return m.jobsList.CapturingInput()
	}
}

// isBladeNimbied reports whether a blade's nimby field indicates it is nimbied.
func isBladeNimbied(nimby string) bool {
	n := strings.TrimSpace(nimby)
	return n != "" && n != "0"
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.showHelp {
		return m.renderHelp() + "\n" + m.statusBar()
	}
	if m.showNotes {
		return m.renderNotes() + "\n" + m.statusBar()
	}
	if m.showTrace {
		return m.renderTrace() + "\n" + m.statusBar()
	}
	if m.showSaveFilter {
		return m.renderSaveFilter() + "\n" + m.statusBar()
	}
	if m.showLoadFilter {
		return m.renderLoadFilter() + "\n" + m.statusBar()
	}
	if m.actionMenu.Active() {
		return m.actionMenu.View(m.width, m.height-1) + "\n" + m.statusBar()
	}
	var body string
	switch m.activeView {
	case ViewTaskLog:
		body = m.logView.View()
	case ViewJobDetail:
		body = m.detail.View()
	case ViewBladesList:
		body = m.blades.View()
	case ViewBladeDetail:
		body = m.bladeDetail.View()
	default:
		body = m.jobsList.View()
	}
	return body + "\n" + m.statusBar()
}

// renderHelp draws the keyboard-shortcut overlay, centred in the body area.
func (m Model) renderHelp() string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	hdrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true)

	row := func(k, d string) string {
		return keyStyle.Render(fmt.Sprintf("%-16s", k)) + descStyle.Render(d)
	}
	section := func(name string, rows ...string) string {
		return hdrStyle.Render(name) + "\n" + strings.Join(rows, "\n")
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).
		Render("Keyboard Shortcuts") + "\n\n")
	b.WriteString(section("Navigation",
		row("↑/↓  j/k", "Move selection"),
		row("PgUp/PgDn", "Page up / down"),
		row("g / G", "Jump to top / bottom"),
		row("Enter", "Open job tasks, then a task's log"),
		row("Esc", "Back one level"),
		row("Mouse wheel", "Scroll (Shift to select text)"),
	) + "\n\n")
	b.WriteString(section("Filtering & view",
		row("/", "Open filter form"),
		row("c", "Clear all filters"),
		row("s / f", "Save / recall a named filter"),
		row("m", "Toggle my jobs"),
		row("e", "Toggle errors only"),
		row("Space / A", "Select row / all (for bulk)"),
		row("w", "Watch job (notify when it finishes)"),
		row("y", "Copy id / command (clipboard)"),
		row("a", "Actions (selection if any, else row)"),
		row("n", "View notes (FLG: W=watch L=lock N=note)"),
		row("b", "Blades view"),
		row("R", "Refresh now"),
	) + "\n\n")
	b.WriteString(section("Columns",
		row("1 – 9", "Sort by column (press again to reverse)"),
		row("< / >", "Shrink / grow TITLE column"),
		row("T", "Toggle SPOOLED absolute / relative"),
	) + "\n\n")
	b.WriteString(section("Tasks view",
		row("/", "Search by title"),
		row("1 – 7", "Sort by column"),
		row("[ / ]", "Previous / next job's tasks"),
		row("Space / A", "Select task / all (for bulk)"),
		row("a", "Actions (selection if any, else task)"),
		row("Enter", "Open the task's log"),
		row("R", "Refresh tasks"),
	) + "\n\n")
	b.WriteString(section("Blades view",
		row("/", "Search by name"),
		row("1 – 9", "Sort by column"),
		row("g", "Toggle heatmap grid"),
		row("Enter", "Show blade's running commands"),
		row("t", "Trace (engine diagnostics)"),
		row("a", "Actions menu (nimby/eject/delist)"),
		row("Esc", "Back to jobs"),
		row("R", "Refresh blades"),
	) + "\n\n")
	b.WriteString(section("Log view",
		row("/", "Search log"),
		row("c", "Clear search"),
		row("n / N", "Next / previous match"),
		row("[ / ]", "Previous / next task's log"),
		row("o", "Open log in $PAGER"),
		row("w", "Toggle wrap / horizontal scroll"),
		row("←/→", "Scroll horizontally (no-wrap)"),
		row("g / G", "Top / bottom"),
		row("R", "Refresh log"),
	) + "\n\n")
	b.WriteString(section("Filter form",
		row("Tab / ⇧Tab", "Move between fields"),
		row("Space", "Toggle status chip"),
		row("↑/↓ then →", "Owner/projects autocomplete"),
		row("Enter / Esc", "Apply / cancel"),
	) + "\n\n")
	b.WriteString(section("General",
		row("?", "Toggle this help"),
		row("q", "Quit"),
	))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 3).
		Render(b.String())

	return lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) statusBar() string {
	user := m.cfg.Auth.Username
	host := m.cfg.Engine.Host

	var left string
	var loading bool
	var err error
	switch m.activeView {
	case ViewTaskLog:
		left = fmt.Sprintf(" tractor-cli  │  %s@%s  │  Job %d %s  │  Task %d %s",
			user, host,
			m.logView.JID(), parenTitle(m.logView.JobTitle()),
			m.logView.TID(), parenTitle(m.logView.TaskTitle()))
	case ViewJobDetail:
		left = fmt.Sprintf(" tractor-cli  │  %s@%s  │  Job %d %s  │  Tasks: %d",
			user, host, m.detail.JID(), parenTitle(m.detail.JobTitle()), m.detail.TaskCount())
		loading = m.detail.IsLoading()
		err = m.detail.Err()
	case ViewBladesList:
		left = fmt.Sprintf(" tractor-cli  │  %s@%s  │  Blades: %d",
			user, host, m.blades.BladeCount())
		loading = m.blades.IsLoading()
		err = m.blades.Err()
	case ViewBladeDetail:
		left = fmt.Sprintf(" tractor-cli  │  %s@%s  │  Blade %s  │  Cmds: %d",
			user, host, m.bladeDetail.BladeName(), m.bladeDetail.CmdCount())
		loading = m.bladeDetail.IsLoading()
		err = m.bladeDetail.Err()
	default:
		left = fmt.Sprintf(" tractor-cli  │  %s@%s  │  Jobs: %d",
			user, host, m.jobsList.JobCount())
		loading = m.jobsList.IsLoading()
		err = m.jobsList.Err()
	}

	if loading {
		left += "  │  " + m.spinner.View() + " loading…"
	} else if m.activeView == ViewJobsList {
		if t := m.jobsList.LastRefresh(); !t.IsZero() {
			left += "  │  Refreshed " + t.Format("15:04:05")
		}
	}

	if err != nil {
		left = " " + styles.ErrorStyle.Render("✗ "+err.Error())
	}

	if m.actionMsg != "" {
		st := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		if m.actionErr {
			st = styles.ErrorStyle
		}
		left += "  │  " + st.Render(m.actionMsg)
	}

	right := " [?] help  [q] quit "
	if m.activeView != ViewJobsList {
		right = " [Esc] back  [?] help  [q] quit "
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	return styles.StatusBar.Width(m.width).Render(
		left + fmt.Sprintf("%*s", gap, "") + right,
	)
}

// trace overlay messages
type traceLoadedMsg struct{ text string }
type traceErrMsg struct{ err error }

func (m Model) fetchTrace(name, addr string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		text, err := client.TraceBlade(name, addr)
		if err != nil {
			return traceErrMsg{err: err}
		}
		if text == "" {
			text = "(no trace output)"
		}
		return traceLoadedMsg{text: text}
	}
}

func (m Model) traceViewportHeight() int {
	h := m.height - 2 // header + status bar
	if h < 1 {
		h = 1
	}
	return h
}

// renderTrace draws the blade trace overlay (a scrollable full-area view).
func (m Model) renderTrace() string {
	title := "Blade trace — " + m.traceTitle
	if m.traceLoading {
		title += "  " + m.spinner.View()
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render("   [Esc] close   ↑/↓ PgUp/PgDn scroll")
	header := styles.Title.Render(title) + hint
	if !m.traceReady {
		return header
	}
	return header + "\n" + m.traceVP.View()
}

// notes overlay messages
type notesLoadedMsg struct {
	jid  int
	text string
}
type notesErrMsg struct {
	jid int
	err error
}

// fetchNotes loads and formats a job's notes for the overlay.
func (m Model) fetchNotes(jid int) tea.Cmd {
	client := m.client
	format := m.cfg.UI.DateFormat
	return func() tea.Msg {
		notes, err := client.JobNotes(jid)
		if err != nil {
			return notesErrMsg{jid: jid, err: err}
		}
		if len(notes) == 0 {
			return notesLoadedMsg{jid: jid, text: "(no notes)"}
		}
		var b strings.Builder
		for i, n := range notes {
			ts := ""
			if n.Time > 0 {
				ts = time.Unix(int64(n.Time), 0).Format(format)
			}
			head := strings.TrimSpace(n.User)
			if n.Type != "" {
				head += " (" + n.Type + ")"
			}
			b.WriteString(strings.TrimSpace(ts+"  "+head) + "\n")
			b.WriteString(strings.TrimSpace(n.Text) + "\n")
			if i < len(notes)-1 {
				b.WriteString("\n")
			}
		}
		return notesLoadedMsg{jid: jid, text: b.String()}
	}
}

// renderNotes draws the notes overlay, centred in the body area.
func (m Model) renderNotes() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).
		Render("Notes — " + m.notesTitle)
	var body string
	switch {
	case m.notesLoading:
		body = "Loading… " + m.spinner.View()
	case m.notesErr != nil:
		body = styles.ErrorStyle.Render("Error: " + m.notesErr.Error())
	default:
		body = m.notesText
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("press any key to close")

	width := m.width - 8
	if width > 90 {
		width = 90
	}
	if width < 20 {
		width = 20
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 3).
		Width(width).
		Render(header + "\n\n" + body + "\n\n" + hint)
	return lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, box)
}

// actionDoneMsg reports the outcome of an action invoked from the menu.
type actionDoneMsg struct {
	op    string
	label string
	err   error
}

// runAction maps a chosen action to the corresponding API call(s), run async.
// Bulk targets apply the same op to every selected job/task and report an
// aggregate result.
func (m Model) runAction(c action.ChosenMsg) tea.Cmd {
	client := m.client
	t := c.Target
	op := c.Op
	val := c.Value
	return func() tea.Msg {
		if t.Bulk {
			total, failed, firstErr := 0, 0, error(nil)
			record := func(e error) {
				total++
				if e != nil {
					failed++
					if firstErr == nil {
						firstErr = e
					}
				}
			}
			switch t.Kind {
			case "jobs":
				for _, jid := range t.JIDs {
					record(jobOpCall(client, op, jid, val))
				}
			case "tasks":
				for _, tid := range t.TIDs {
					record(taskOpCall(client, op, t.JID, tid))
				}
			}
			label := fmt.Sprintf("%s on %d", op, total)
			var err error
			if failed > 0 {
				err = fmt.Errorf("%d of %d failed (e.g. %v)", failed, total, firstErr)
			}
			return actionDoneMsg{op: op, label: label, err: err}
		}

		var err error
		switch t.Kind {
		case "blade":
			switch op {
			case "nimby":
				err = client.NimbyBlade(t.Name, t.Addr)
			case "unnimby":
				err = client.UnnimbyBlade(t.Name, t.Addr)
			case "eject":
				err = client.EjectBlade(t.Name, t.Addr)
			case "delist":
				err = client.DelistBlade(t.Name, t.Addr)
			default:
				err = fmt.Errorf("unknown action %q", op)
			}
		case "task":
			err = taskOpCall(client, op, t.JID, t.TID)
		default: // job
			err = jobOpCall(client, op, t.JID, val)
		}
		return actionDoneMsg{op: op, label: t.Label, err: err}
	}
}

// jobOpCall runs a single job operation.
func jobOpCall(client *api.Client, op string, jid int, val string) error {
	switch op {
	case "pause":
		return client.PauseJob(jid)
	case "unpause":
		return client.UnpauseJob(jid)
	case "retry-errors":
		return client.RetryErroredTasks(jid)
	case "retry-active":
		return client.RetryActiveTasks(jid)
	case "skip-errors":
		return client.SkipErroredTasks(jid)
	case "interrupt":
		return client.InterruptJob(jid)
	case "restart":
		return client.RestartJob(jid)
	case "delete":
		return client.DeleteJob(jid)
	case "chpri":
		p, perr := strconv.ParseFloat(val, 64)
		if perr != nil {
			return fmt.Errorf("invalid priority %q", val)
		}
		return client.SetJobPriority(jid, p)
	case "lock":
		return client.LockJob(jid, val)
	case "unlock":
		return client.UnlockJob(jid)
	default:
		return fmt.Errorf("unknown job action %q", op)
	}
}

// taskOpCall runs a single task operation.
func taskOpCall(client *api.Client, op string, jid, tid int) error {
	switch op {
	case "retry":
		return client.RetryTask(jid, tid)
	case "resume":
		return client.ResumeTask(jid, tid)
	case "skip":
		return client.SkipTask(jid, tid)
	case "kill":
		return client.KillTask(jid, tid)
	default:
		return fmt.Errorf("unknown task action %q", op)
	}
}

// parenTitle wraps a non-empty title in parentheses for the status bar.
func parenTitle(s string) string {
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}

// scrollActive moves the active table view's cursor by one row (mouse wheel).
func (m Model) scrollActive(up bool) (tea.Model, tea.Cmd) {
	k := tea.KeyMsg{Type: tea.KeyDown}
	if up {
		k.Type = tea.KeyUp
	}
	var cmd tea.Cmd
	switch m.activeView {
	case ViewJobDetail:
		m.detail, cmd = m.detail.Update(k)
	case ViewBladesList:
		m.blades, cmd = m.blades.Update(k)
	case ViewBladeDetail:
		m.bladeDetail, cmd = m.bladeDetail.Update(k)
	default:
		m.jobsList, cmd = m.jobsList.Update(k)
	}
	return m, cmd
}

// saveState persists the current layout and saved filters (best effort).
func (m Model) saveState() {
	st := state.State{
		TitleWidth: m.jobsList.LayoutTitleWidth(),
		SortCol:    m.jobsList.LayoutSortCol(),
		SortAsc:    m.jobsList.LayoutSortAsc(),
		RelTime:    m.jobsList.LayoutRelTime(),
		Filters:    m.stateData.Filters,
	}
	_ = st.Save()
}

func (m Model) renderSaveFilter() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("86")).
		Padding(1, 3).Width(50).
		Render(lipgloss.NewStyle().Bold(true).Render("Save current filter as") + "\n\n" +
			m.saveInput.View() + "\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Enter save   Esc cancel"))
	return lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderLoadFilter() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).
		Render("Saved filters") + "\n\n")
	for i, name := range m.loadNames {
		cursor := "  "
		st := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
		if i == m.loadCursor {
			cursor = "› "
			st = st.Bold(true).Background(lipgloss.Color("236"))
		}
		b.WriteString(cursor + st.Render(name) + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
		Render("Enter apply   d delete   Esc cancel"))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("86")).
		Padding(1, 3).Width(50).Render(b.String())
	return lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, box)
}

// bellCmd rings the terminal bell (used for watched-job notifications).
func bellCmd() tea.Cmd {
	return func() tea.Msg {
		fmt.Fprint(os.Stderr, "\a")
		return nil
	}
}

// yankCmd copies text to the clipboard, trying OSC 52 (written to the tty so it
// survives the alt-screen, with tmux passthrough) and any local clipboard tool.
func yankCmd(text string) tea.Cmd {
	return func() tea.Msg {
		copyToClipboard(text)
		return nil
	}
}

// clipboardTools are the local clipboard helpers we try, in order.
var clipboardTools = [][]string{
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"xsel", "--clipboard", "--input"},
	{"pbcopy"},
}

// hasClipboardTool reports whether any local clipboard helper is installed.
func hasClipboardTool() bool {
	for _, c := range clipboardTools {
		if _, err := exec.LookPath(c[0]); err == nil {
			return true
		}
	}
	return false
}

func copyToClipboard(text string) {
	// OSC 52, written straight to the controlling terminal. Use the ST
	// terminator (more widely accepted than BEL for OSC 52).
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x1b\\"
	if os.Getenv("TMUX") != "" {
		// tmux passthrough: wrap and double the inner ESCs.
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		_, _ = tty.WriteString(seq)
		_ = tty.Close()
	}
	// Also write to stdout, in case /dev/tty isn't the terminal.
	fmt.Fprint(os.Stdout, seq)

	// Best-effort local clipboard tools (work without OSC 52 support — the
	// reliable path on a local X11/Wayland desktop like MATE).
	for _, c := range clipboardTools {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if cmd.Run() == nil {
			break
		}
	}
}

// openInPager writes text to a temp file and opens it in $PAGER (default
// "less -R"), suspending the TUI until the pager exits.
func openInPager(text string) tea.Cmd {
	f, err := os.CreateTemp("", "tractor-log-*.txt")
	if err != nil {
		return nil
	}
	_, _ = f.WriteString(text)
	_ = f.Close()

	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less -R"
	}
	parts := strings.Fields(pager)
	args := append(parts[1:], f.Name())
	c := exec.Command(parts[0], args...)
	return tea.ExecProcess(c, func(error) tea.Msg {
		_ = os.Remove(f.Name())
		return nil
	})
}

// truncatePlain truncates a plain string to n runes with an ellipsis.
func truncatePlain(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func scheduleTick(d time.Duration) tea.Cmd {
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}
