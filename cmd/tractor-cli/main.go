package main

import (
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/tractor-cli/tractor-cli/internal/api"
	"github.com/tractor-cli/tractor-cli/internal/config"
	"github.com/tractor-cli/tractor-cli/internal/session"
	"github.com/tractor-cli/tractor-cli/internal/tui"
	"github.com/tractor-cli/tractor-cli/internal/users"
)

// version is overridden at build time via -ldflags "-X main.version=…".
var version = "dev"

func main() {
	var sanity bool
	var raw bool

	root := &cobra.Command{
		Use:     "tractor-cli",
		Short:   "Interactive TUI for Pixar Tractor render farm",
		Version: version,
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
	client, err := api.NewClient(cfg.BaseURL())
	if err != nil {
		return nil, nil, fmt.Errorf("creating client: %w", err)
	}

	sessPath := session.Path(cfg.Engine.Host, cfg.Engine.Port, cfg.Auth.Username)

	// 1. Try to reuse a saved session — no password needed if it's still valid.
	if tsid, ok := session.Load(sessPath); ok {
		client.UseSession(cfg.Auth.Username, tsid)
		if client.ValidateSession() {
			return client, cfg, nil
		}
		// Stale/invalid tsid: discard and fall through to a fresh login.
		session.Delete(sessPath)
	}

	// 2. Log in with a password (from $TRACTOR_PASSWORD, or prompted).
	if err := login(client, cfg); err != nil {
		return nil, nil, err
	}

	// 3. Persist the new session for next time (best effort).
	if tsid := client.TSID(); tsid != "" {
		if err := session.Save(sessPath, tsid); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not save session file: %v\n", err)
		}
	}
	return client, cfg, nil
}

// login authenticates client. It first tries $TRACTOR_PASSWORD; if the engine
// requires a password and none is set (or it was wrong), it prompts interactively
// when stdin is a terminal, allowing a few retries.
func login(client *api.Client, cfg *config.Config) error {
	user := cfg.Auth.Username
	password := config.Password()

	err := client.Login(user, password)
	if err == nil {
		return nil
	}

	// Only prompt when the engine actually wants a password and we can read one.
	interactive := term.IsTerminal(os.Stdin.Fd())
	needsPrompt := errors.Is(err, api.ErrPasswordRequired) ||
		(errors.Is(err, api.ErrAuthFailed) && password == "")
	if !needsPrompt || !interactive {
		return fmt.Errorf("login: %w", err)
	}

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		password, err = promptPassword(user)
		if err != nil {
			return fmt.Errorf("reading password: %w", err)
		}
		if password == "" {
			return fmt.Errorf("login aborted: no password entered")
		}
		err = client.Login(user, password)
		if err == nil {
			return nil
		}
		if !errors.Is(err, api.ErrAuthFailed) {
			// Network/parse error — retrying the password won't help.
			return fmt.Errorf("login: %w", err)
		}
		if attempt < maxAttempts {
			fmt.Fprintln(os.Stderr, "Incorrect password, try again.")
		}
	}
	return fmt.Errorf("login failed after %d attempts", maxAttempts)
}

// promptPassword reads a password from the terminal without echoing it.
func promptPassword(user string) (string, error) {
	fmt.Fprintf(os.Stderr, "Tractor password for %s: ", user)
	b, err := term.ReadPassword(os.Stdin.Fd())
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ensureLDAPTicket makes sure a Kerberos ticket exists for the AD owner-
// autocomplete lookup. It does nothing unless LDAP is fully configured (so
// users who don't use LDAP are never bothered) and no ticket is already
// present. With ldap.auto_kinit it tries $TRACTOR_PASSWORD non-interactively;
// otherwise, on a terminal, it offers an optional password prompt that the user
// can skip with an empty entry. Only one attempt is made, to avoid tripping AD
// account-lockout policies.
func ensureLDAPTicket(cfg *config.Config) {
	l := cfg.LDAP
	if l.URI == "" || l.BaseDN == "" || l.Realm == "" {
		return // LDAP not configured — nothing to do.
	}
	if users.HasTicket() {
		return
	}
	principal := cfg.Auth.Username + "@" + l.Realm

	// Non-interactive: auto_kinit with the env password, if enabled.
	if l.AutoKinit {
		if pw := config.Password(); pw != "" {
			if err := users.Kinit(principal, pw); err != nil {
				fmt.Fprintf(os.Stderr, "warning: auto kinit failed: %v\n", err)
			}
		}
		return
	}

	// Interactive: offer an optional prompt (skippable).
	if !term.IsTerminal(os.Stdin.Fd()) {
		return
	}
	fmt.Fprintf(os.Stderr,
		"No Kerberos ticket found; LDAP owner autocomplete needs one.\n"+
			"Enter AD password for %s to run kinit (or press Enter to skip): ", principal)
	b, err := term.ReadPassword(os.Stdin.Fd())
	fmt.Fprintln(os.Stderr)
	if err != nil || len(b) == 0 {
		return // skipped or unreadable — fall back to getent + job owners.
	}
	if err := users.Kinit(principal, string(b)); err != nil {
		fmt.Fprintf(os.Stderr, "kinit failed: %v (continuing without LDAP autocomplete)\n", err)
	}
}

func runTUI() error {
	client, cfg, err := buildClient()
	if err != nil {
		return err
	}
	// Owner autocomplete queries Active Directory over GSSAPI, which needs a
	// Kerberos ticket. Offer to acquire one before the TUI takes over the
	// terminal; declining just falls back to getent + job owners.
	ensureLDAPTicket(cfg)
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
