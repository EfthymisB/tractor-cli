package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
	"github.com/tractor-cli/tractor-cli/internal/tui"
)

func main() {
	var sanity bool
	var raw bool

	root := &cobra.Command{
		Use:   "tractor",
		Short: "Interactive TUI for Pixar Tractor render farm",
		RunE: func(cmd *cobra.Command, args []string) error {
			if raw {
				return runRaw()
			}
			if sanity {
				return runSanityCheck()
			}
			return runTUI()
		},
	}
	root.Flags().BoolVar(&sanity, "sanity", false, "print jobs to stdout and exit (Phase 1 smoke test)")
	root.Flags().BoolVar(&raw, "raw", false, "print raw JSON from /Tractor/monitor?q=jobs and exit")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func buildClient() (*api.Client, *config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}
	password := config.Password()
	if password == "" {
		return nil, nil, fmt.Errorf("TRACTOR_PASSWORD environment variable is not set")
	}
	client, err := api.NewClient(cfg.BaseURL())
	if err != nil {
		return nil, nil, fmt.Errorf("creating client: %w", err)
	}
	if err := client.Login(cfg.Auth.Username, password); err != nil {
		return nil, nil, fmt.Errorf("login: %w", err)
	}
	return client, cfg, nil
}

func runTUI() error {
	client, cfg, err := buildClient()
	if err != nil {
		return err
	}
	p := tea.NewProgram(
		tui.New(client, cfg),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err = p.Run()
	return err
}

func runRaw() error {
	client, cfg, err := buildClient()
	if err != nil {
		return err
	}
	fmt.Printf("Connected as %s\n\n", cfg.Auth.Username)
	raw, err := client.RawJobsResponse()
	if err != nil {
		return fmt.Errorf("fetching jobs: %w", err)
	}
	fmt.Println(string(raw))
	return nil
}

func runSanityCheck() error {
	client, cfg, err := buildClient()
	if err != nil {
		return err
	}
	fmt.Printf("Connected to %s as %s (tsid: %s)\n\n",
		cfg.BaseURL(), cfg.Auth.Username, client.TSID())

	jobs, err := client.QueryJobs(api.JobFilter{})
	if err != nil {
		return fmt.Errorf("querying jobs: %w", err)
	}
	fmt.Printf("Found %d job(s):\n\n", len(jobs))
	fmt.Printf("%-8s %-40s %-12s %-10s %8s %5s %5s\n",
		"JID", "TITLE", "OWNER", "STATUS", "PRIORITY", "DONE", "ERR")
	fmt.Println(repeat("-", 95))
	for _, j := range jobs {
		fmt.Printf("%-8d %-40s %-12s %-10s %8.0f %5d %5d\n",
			j.JID, truncate(j.Title, 40), j.Owner, j.Status,
			j.Priority, j.NumDone, j.NumError)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func repeat(s string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = s[0]
	}
	return string(b)
}
