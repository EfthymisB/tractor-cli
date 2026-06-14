// Package tui is the root bubbletea model for tractor-cli.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
	"github.com/tractor-cli/tractor-cli/internal/tui/styles"
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
)

// Model is the root bubbletea model.
type Model struct {
	activeView View

	jobsList jobs.ListModel
	detail   jobs.DetailModel
	logView  logview.Model

	client *api.Client
	cfg    *config.Config

	spinner spinner.Model

	showHelp bool

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
		logView:    logview.New(client, cfg),
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
		return m, nil

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
			}
		case "]", "[":
			// In the log view, jump to the next/previous task's log.
			if m.activeView == ViewTaskLog && !capturing {
				dir := 1
				if msg.String() == "[" {
					dir = -1
				}
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
				}
			}
		}

	case tickMsg:
		// Auto-refresh whichever view is in front.
		var refreshCmd tea.Cmd
		if m.activeView == ViewJobDetail {
			m.detail, refreshCmd = m.detail.Refresh()
		} else {
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
	default:
		return m.jobsList.CapturingInput()
	}
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.showHelp {
		return m.renderHelp() + "\n" + m.statusBar()
	}
	var body string
	switch m.activeView {
	case ViewTaskLog:
		body = m.logView.View()
	case ViewJobDetail:
		body = m.detail.View()
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
		row("R", "Refresh now"),
	) + "\n\n")
	b.WriteString(section("Columns",
		row("1 – 9", "Sort by column (press again to reverse)"),
		row("< / >", "Shrink / grow TITLE column"),
	) + "\n\n")
	b.WriteString(section("Tasks view",
		row("/", "Search by title"),
		row("1 – 7", "Sort by column"),
		row("Enter", "Open the task's log"),
		row("R", "Refresh tasks"),
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
		row("→", "Accept owner autocomplete"),
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
