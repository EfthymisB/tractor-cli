// Package tui is the root bubbletea model for tractor-cli.
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
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

	width, height int
}

// New creates and returns the root TUI model.
func New(client *api.Client, cfg *config.Config) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

	return Model{
		activeView: ViewJobsList,
		jobsList:   jobs.New(client, cfg),
		detail:     jobs.NewDetail(client, cfg),
		logView:     logview.New(client, cfg),
		blades:      blades.New(client, cfg),
		bladeDetail: blades.NewDetail(client, cfg),
		actionMenu:  action.New(),
		client:     client,
		cfg:        cfg,
		spinner:    sp,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.jobsList.Init(),
		m.spinner.Tick,
		scheduleTick(time.Duration(m.cfg.UI.RefreshInterval)*time.Second),
	)
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
		return m, nil

	case action.ChosenMsg:
		return m, m.runAction(msg)

	case action.CancelMsg:
		return m, nil

	case actionDoneMsg:
		m.actionErr = msg.err != nil
		if msg.err != nil {
			m.actionMsg = "✗ " + msg.op + ": " + msg.err.Error()
		} else {
			m.actionMsg = "✓ " + msg.op + " — " + msg.label
		}
		// Reflect the change by refreshing the active view.
		var refresh tea.Cmd
		switch m.activeView {
		case ViewJobDetail:
			m.detail, refresh = m.detail.Refresh()
		case ViewBladesList:
			m.blades, refresh = m.blades.Refresh()
		default:
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
					m.bladeDetail, cmd = m.bladeDetail.Load(bl.Name)
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
					if job, ok := m.jobsList.SelectedJob(); ok {
						label := fmt.Sprintf("Job %d — %s", job.JID, job.Title)
						m.actionMenu = m.actionMenu.OpenJob(job.JID, label, job.Status == "paused")
						return m, nil
					}
				case ViewJobDetail:
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
					m.activeView = ViewJobsList
					return m, nil
				case ViewBladesList:
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
		cmds = append(cmds, refreshCmd)
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
	) + "\n\n")
	b.WriteString(section("Filtering & view",
		row("/", "Open filter form"),
		row("c", "Clear all filters"),
		row("m", "Toggle my jobs"),
		row("e", "Toggle errors only"),
		row("a", "Actions menu (job/task)"),
		row("b", "Blades view"),
		row("R", "Refresh now"),
	) + "\n\n")
	b.WriteString(section("Columns",
		row("1 – 9", "Sort by column (press again to reverse)"),
		row("< / >", "Shrink / grow TITLE column"),
	) + "\n\n")
	b.WriteString(section("Tasks view",
		row("/", "Search by title"),
		row("1 – 7", "Sort by column"),
		row("[ / ]", "Previous / next job's tasks"),
		row("a", "Actions menu (task)"),
		row("Enter", "Open the task's log"),
		row("R", "Refresh tasks"),
	) + "\n\n")
	b.WriteString(section("Blades view",
		row("/", "Search by name"),
		row("1 – 9", "Sort by column"),
		row("Enter", "Show blade's running commands"),
		row("a", "Actions menu (nimby/eject/delist)"),
		row("Esc", "Back to jobs"),
		row("R", "Refresh blades"),
	) + "\n\n")
	b.WriteString(section("Log view",
		row("/", "Search log"),
		row("c", "Clear search"),
		row("n / N", "Next / previous match"),
		row("[ / ]", "Previous / next task's log"),
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

// actionDoneMsg reports the outcome of an action invoked from the menu.
type actionDoneMsg struct {
	op    string
	label string
	err   error
}

// runAction maps a chosen action to the corresponding API call, run async.
func (m Model) runAction(c action.ChosenMsg) tea.Cmd {
	client := m.client
	t := c.Target
	op := c.Op
	val := c.Value
	return func() tea.Msg {
		var err error
		switch op {
		case "pause":
			err = client.PauseJob(t.JID)
		case "unpause":
			err = client.UnpauseJob(t.JID)
		case "retry-errors":
			err = client.RetryErroredTasks(t.JID)
		case "retry-active":
			err = client.RetryActiveTasks(t.JID)
		case "skip-errors":
			err = client.SkipErroredTasks(t.JID)
		case "interrupt":
			err = client.InterruptJob(t.JID)
		case "restart":
			err = client.RestartJob(t.JID)
		case "delete":
			err = client.DeleteJob(t.JID)
		case "chpri":
			p, perr := strconv.ParseFloat(val, 64)
			if perr != nil {
				err = fmt.Errorf("invalid priority %q", val)
			} else {
				err = client.SetJobPriority(t.JID, p)
			}
		case "retry":
			err = client.RetryTask(t.JID, t.TID)
		case "resume":
			err = client.ResumeTask(t.JID, t.TID)
		case "skip":
			err = client.SkipTask(t.JID, t.TID)
		case "kill":
			err = client.KillTask(t.JID, t.TID)
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
		return actionDoneMsg{op: op, label: t.Label, err: err}
	}
}

// parenTitle wraps a non-empty title in parentheses for the status bar.
func parenTitle(s string) string {
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}

func scheduleTick(d time.Duration) tea.Cmd {
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}
