package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
)

func main() {
	root := &cobra.Command{
		Use:   "tractor",
		Short: "Interactive TUI for Pixar Tractor render farm",
		RunE:  runSanityCheck,
	}
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// runSanityCheck authenticates and prints all active jobs to stdout.
// This is the Phase 1 smoke test — it will be replaced by the TUI in Phase 2.
func runSanityCheck(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	password := config.Password()
	if password == "" {
		return fmt.Errorf("TRACTOR_PASSWORD environment variable is not set")
	}

	fmt.Printf("Connecting to %s as %s...\n", cfg.BaseURL(), cfg.Auth.Username)

	client, err := api.NewClient(cfg.BaseURL())
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	if err := client.Login(cfg.Auth.Username, password); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	fmt.Printf("Authenticated successfully (tsid: %s).\n", client.TSID())

	jobs, err := client.QueryJobs(api.JobFilter{})
	if err != nil {
		return fmt.Errorf("querying jobs: %w", err)
	}

	fmt.Printf("\nFound %d job(s):\n\n", len(jobs))
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
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
