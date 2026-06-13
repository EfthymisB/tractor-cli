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
		// Give the active child everything except the bottom status bar line.
		m.jobsList = m.jobsList.SetSize(m.width, m.height-1)
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

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			// Don't quit while the filter form is capturing keystrokes — "q"
			// may be part of a username or title being typed.
			if !m.jobsList.CapturingInput() {
				return m, tea.Quit
			}
		case "?":
			// Open help, unless the filter form wants the literal "?".
			if !m.jobsList.CapturingInput() {
				m.showHelp = true
				return m, nil
			}
		}

	case tickMsg:
		var refreshCmd tea.Cmd
		m.jobsList, refreshCmd = m.jobsList.Refresh()
		cmds = append(cmds, refreshCmd)
		if m.cfg.UI.RefreshInterval > 0 {
			cmds = append(cmds, scheduleTick(time.Duration(m.cfg.UI.RefreshInterval)*time.Second))
		}
		return m, tea.Batch(cmds...)
	}

	// Delegate everything else to the active child model.
	var childCmd tea.Cmd
	m.jobsList, childCmd = m.jobsList.Update(msg)
	cmds = append(cmds, childCmd)

	// Keep spinner animated.
	var spinCmd tea.Cmd
	m.spinner, spinCmd = m.spinner.Update(msg)
	cmds = append(cmds, spinCmd)

	return m, tea.Batch(cmds...)
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
	case ViewJobsList:
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

	left := fmt.Sprintf(" tractor-cli  │  %s@%s  │  Jobs: %d",
		user, host, m.jobsList.JobCount())

	if m.jobsList.IsLoading() {
		left += "  │  " + m.spinner.View() + " loading…"
	} else if t := m.jobsList.LastRefresh(); !t.IsZero() {
		left += "  │  Refreshed " + t.Format("15:04:05")
	}

	if err := m.jobsList.Err(); err != nil {
		left = " " + styles.ErrorStyle.Render("✗ "+err.Error())
	}

	right := " [?] help  [q] quit "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	return styles.StatusBar.Width(m.width).Render(
		left + fmt.Sprintf("%*s", gap, "") + right,
	)
}

func scheduleTick(d time.Duration) tea.Cmd {
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}
