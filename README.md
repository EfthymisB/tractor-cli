# tractor-cli

An interactive terminal UI (Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea))
for Pixar's [Tractor](https://rmanwiki.pixar.com/display/TRA) render farm — browse
and act on jobs, tasks, logs and blades without leaving the terminal.

## Build & run

```bash
go build -o tractor-cli ./cmd/tractor-cli
./tractor-cli
```

or just:

```bash
go run ./cmd/tractor-cli
```

## Configuration

Settings are read from `~/.config/tractor-cli/config.yaml` (all optional) and
environment variables.

```yaml
engine:
  host: tractor-engine
  port: 80
auth:
  username: your-name        # defaults to $USER
ui:
  refresh_interval: 30       # seconds (0 = no auto-refresh)
  date_format: "15:04 02/01"
ldap:                        # owner-filter autocomplete (Active Directory)
  uri: ldap://dc.example.com
  base_dn: dc=example,dc=com
  realm: EXAMPLE.COM
  auto_kinit: false          # opt-in; runs kinit with TRACTOR_PASSWORD if no ticket
keys:                        # rebind global shortcuts (defaults shown)
  quit: q
  help: "?"
  actions: a
  blades: b
  notes: n
  trace: t
  yank: y
  save_filter: s
  load_filter: f
  next: "]"
  prev: "["
```

Only the global shortcuts above are rebindable; view-internal keys (sort numbers,
`/` filter, `Enter`, `Esc`, etc.) are fixed. `?` always lists the current keys.

Environment overrides: `TRACTOR_HOST`, `TRACTOR_PORT`, `TRACTOR_USER`,
`TRACTOR_LDAP_URI`, `TRACTOR_LDAP_BASE_DN`, `TRACTOR_LDAP_REALM`,
`TRACTOR_LDAP_AUTO_KINIT`.

> **The Tractor password is only ever read from the `TRACTOR_PASSWORD`
> environment variable — never from the config file.**
>
> ```bash
> export TRACTOR_PASSWORD='…'
> ```

UI preferences (sort, column width, relative-time, saved filters) persist to
`~/.config/tractor-cli/state.json`.

## Optional system dependencies

- **`xclip`** (or `xsel` / `wl-copy`) — required for the clipboard yank (`y`) to
  work. MATE Terminal and other VTE-based terminals block the OSC 52 escape
  used for clipboard, so a local clipboard tool is needed:

  ```bash
  sudo dnf install xclip
  ```

- **`ldapsearch`** + a Kerberos ticket (`kinit`) — used to populate owner-filter
  autocomplete from Active Directory. Without it, autocomplete falls back to
  `getent passwd` and the owners present in loaded jobs.
- **`$PAGER`** (default `less -R`) — used by `o` to open a task log externally.

## Keys

Press `?` in the app for the full, always-current shortcut list. Highlights:

| Key | Action |
|-----|--------|
| `/` | Filter form (jobs) / search (tasks, blades, logs) |
| `Enter` | Drill in: job → tasks → task log |
| `Esc` | Back one level |
| `a` | Actions menu (job / task / blade; or the current selection) |
| `Space` / `A` | Select row / all, for bulk actions |
| `w` | Watch a job (notify on finish/error) |
| `y` | Copy id / command to the clipboard |
| `n` | View a job's notes |
| `b` | Blades view (`g` toggles the heatmap) |
| `1`–`9` | Sort by column |
| `s` / `f` | Save / recall a named filter |
| `q` | Quit |

### Filter form

The jobs filter form (`/`) has fields for owner, status, JID (list or range),
priority, projects, title, and a **spool-date window**:

- **Spooled >** — only jobs spooled *after* this point.
- **Spooled <** — only jobs spooled *before* this point.

Both accept Tractor's native time syntax: relative tokens (`-24h`, `-7d`,
`-1w`, `-30m`, `-15s` — e.g. `-24h` for the last 24 hours) or absolute dates
(`3/15/25`, `2025-03-15`). Either bound may be left blank.
