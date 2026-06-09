// Package tui is the root bubbletea model for tractor-cli.
package tui

import (
	"fmt"
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
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
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
	var body string
	switch m.activeView {
	case ViewJobsList:
		body = m.jobsList.View()
	}
	return body + "\n" + m.statusBar()
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
