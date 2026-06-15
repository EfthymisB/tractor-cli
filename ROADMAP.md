# tractor-cli — Project Guide & Roadmap

> This file is a self-contained handoff for a fresh assistant/chat with no prior
> context. It covers what the project is, how to work on it, the engine quirks
> learned the hard way, the full feature set, and what's left.

An interactive terminal UI (Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea))
for Pixar's **Tractor** render farm — browse and act on jobs, tasks, logs and
blades from the terminal.

- **Repo**: https://github.com/EfthymisB/tractor-cli (branch `master`)
- **Module**: `github.com/tractor-cli/tractor-cli`, **Go 1.24** (`go.mod`/`go.sum` committed)
- **Status**: all six phases complete + a quality-of-life batch. Feature-complete; ready to tag `v0.1.0`.

---

## ⚠️ How development works here (read first)

The dev setup is unusual — keep these in mind for every change:

- **Windows dev box HAS a Go toolchain** (Go 1.26.4, installed at
  `G:\Tools\Go\bin\go.exe`). You can `go build`/`go vet`/`go test` locally on
  Windows to catch compile errors and run the pure-logic unit tests. Note: a
  freshly spawned shell may not have `G:\Tools\Go\bin` on its `PATH` yet —
  invoke the full path if `go` isn't found.
- **You still cannot exercise Tractor itself from Windows** — there's no engine
  to talk to. Anything that hits the live farm API (auth, queries, actions) must
  be tested by the user on the Linux farm.
- **The app also builds & runs on the Linux farm** (Alma 9, machine `rlin165`),
  directly on its **MATE terminal** (not over SSH). The user runs `go run ./cmd/tractor-cli`
  or `go test ./...` there and pastes farm-interaction results back.
- **Git pushes happen from the Windows clone** (`E:\tractor_cli`). The **farm
  account lacks write access** to GitHub (403 on push), so the assistant commits
  & pushes from Windows; the user pulls on the farm.
- **Commit/push after every change** (user preference). End commit messages with the
  `Co-Authored-By` trailer.
- **Shell on Windows is PowerShell.** Multi-line commit messages use a
  single-quoted here-string:
  ```powershell
  git commit -m @'
  subject line

  body…
  '@
  ```
  Avoid embedding `"` or other quotes in the message — they break the here-string.
- **Reference Tractor source** is in `tractor_source/` (gitignored, local only).
  It's the real Tractor Python source — grep it to learn the engine API rather
  than guessing. The HTTP dispatch table is NOT there (it's in the engine binary),
  so some query names had to be inferred/tested.
- **Memory**: the user prefers PowerShell on Windows and "commit+push after every
  change" (already captured in assistant memory).

---

## Build / test / run

```bash
go build ./cmd/tractor-cli  # or: go run ./cmd/tractor-cli
go test ./...               # unit tests (pure logic)
make build | run | test | fmt | snapshot
```

Tooling: `Makefile`, `.goreleaser.yaml` (linux/darwin × amd64/arm64),
`.github/workflows/ci.yml` (gofmt report, vet, build, test on Go 1.24),
`--version` flag wired via `-ldflags -X main.version`.

---

## Configuration

`~/.config/tractor-cli/config.yaml` (all optional) + env overrides
(`TRACTOR_HOST/PORT/USER`, `TRACTOR_LDAP_*`). Sections: `engine`, `auth`, `ui`,
`ldap`, `keys`. UI prefs + saved filters persist to
`~/.config/tractor-cli/state.json`.

- **`TRACTOR_PASSWORD` is read ONLY from the environment — never the config file.**
  This rule is intentional and load-bearing; keep it.
- `keys:` rebinds the global shortcuts (see below); view-internal keys are fixed.

### Optional system deps
- **`xclip`** (or `xsel`/`wl-copy`) — required for clipboard yank (`y`). MATE/VTE
  terminals block the OSC 52 escape, so a local clipboard tool is the reliable path
  (`sudo dnf install xclip`). The app warns in the status bar if none is installed.
- **`ldapsearch` + Kerberos ticket** — owner-filter autocomplete from AD; falls
  back to `getent passwd` + job owners.
- **`$PAGER`** (default `less -R`) — `o` opens a task log externally.

---

## Architecture & file map

Elm-style Model/Update/View throughout. Root model in `internal/tui/app.go`
switches between views and owns overlays (help, notes, trace, save/load-filter,
copy chooser, action menu) and the status bar.

```
cmd/tractor-cli/main.go          entry (cobra); --sanity/--raw/--version
internal/config/config.go        YAML+env config; KeymapConfig; Password() (env only)
internal/state/state.go          ~/.config/tractor-cli/state.json (layout + saved filters)
internal/users/users.go          AD (ldapsearch GSSAPI) + getent username sources
internal/models/                 Job, Task, TaskDetail, Blade, BladeCommand, JobNote
internal/api/
  client.go                      challenge-response login, TractorUser cookie + tsid
  jobs.go                        QueryJobs (q=jobs + tractorselect), buildSelectWhere
  tasks.go                       QueryTasks (q=jtree), JobNotes, decodeTaskState, TaskDetails
  logs.go                        TaskLog (q=tasklogs → LoggingRedirect, %u owner)
  actions.go                     job/task ops (q=queue: jattr/jretry/tskip/jinterrupt/…)
  blades.go                      QueryBlades, BladeDetails (tractorselect), nimby/eject/delist/trace
internal/tui/app.go              root model, key dispatch, overlays, status bar, clipboard
internal/tui/views/
  jobs/list.go                   jobs table, filter, summary line, FLG, watch, panel
  jobs/detail.go                 task list, task info panel, multi-select
  filter/bar.go                  multi-field filter form (owner/projects autocomplete)
  logview/logview.go             log viewer (syntax colour, search, wrap/scroll, gutter)
  blades/blades.go               blade list + heatmap grid
  blades/detail.go               blade running-commands list
  action/action.go               action menu (menu→input→confirm; job/task/blade/bulk)
  styles/styles.go               shared lipgloss styles
```

Key dispatch: `app.go` resolves raw keys through a configured key→action map
(`keyAction`), so the global switch matches action names. Structural keys
(`ctrl+c`/`enter`/`esc`) and view-internal keys are literal/fixed. `capturingInput()`
makes global keys defer to a focused text input.

---

## Tractor engine API — quirks learned

- **Auth**: `GET /Tractor/monitor?q=login` challenge-response (gentoken +
  `trInternalPasswordHash`), then `Cookie: TractorUser=<user>` + `tsid` on every request.
- **Jobs**: `q=jobs` (nested `users→J<jid>→data`, `nTasks=[total,active,done,error]`).
  Filtered queries go through **`tractorselect(...)` over `/Tractor/db`** (the
  supported path; no ~2500 cap). **`q=select` on the monitor is NOT valid** (HTTP 400).
- **Tasks**: `q=jtree` (task tree; state packed as `<log><state>[,<prog>]`, e.g. `-A,50`).
- **Task details**: `q=taskdetails` (argv, blades, rss/vsz in GB→×1024 for MB, cpu, slots, env, service, min/max).
- **Logs**: `q=tasklogs` → `{LoggingRedirect:[uris]}`; fetch each URI; substitute
  the **`%u` owner placeholder** in the path (also an invalid URL escape if left in).
- **Notes**: `q=jtree&notes=1` returns `notes` under the job; lock/note flags come
  from jobinfo (`lockowner`, `locknote`, `lastnoteid`).
- **Blades**: `q=blades` (terse keys: `hnm`,`siu`,`as`,`t`,…). Blade running commands:
  no monitor query — assembled via **`tractorselect`** on invocation+job+command.
  `TractorJSON*` functions are NOT whitelisted for the db proxy ("invalid db proxy call").
- **Actions** (`/Tractor/queue?q=…`): `jattr` (pause/lock/priority via `set_*`),
  `jretry`, `tskip`, `jinterrupt`, `jrestart`, `jretire`, `tretry`.
- **Blade control**: nimby `battribute` (`/Tractor/ctrl`), `ejectall` (`/Tractor/queue`),
  `delist` (`/Tractor/btrack`), `tracer` (`/Tractor/ctrl`, plain text, 12s timeout).
- **WHERE syntax** is Tractor natural-language, NOT SQL: unquoted values
  (`owner=alice`), lists (`owner in [a, b]`), regex `like`, status aliases
  (`active`/`error`/`done`/`blocked`, `pausetime is not null`). Time columns
  accept relative tokens (`spooltime > -24h`, `-7d`, `-1w`, `-30m`, `-15s`) and
  absolute dates (`spooltime < 3/15/25`).

---

## Status — all phases done

| Phase | Title | Status |
|-------|-------|--------|
| 1 | Foundation (config, API client, auth, models) | ✅ |
| 2 | Jobs list view | ✅ |
| 3 | Job detail + task list + task logs | ✅ |
| 4 | Actions (incl. multi-select, lock/unlock, notes) | ✅ |
| 5 | Blades view (list, detail, heatmap, trace) | ✅ |
| 6 | Polish & release (README, CI, goreleaser, tests, keymap) | ✅ |
| + | Quality-of-life batch (summary, watch, clipboard, saved filters, …) | ✅ |

### Feature summary
- **Jobs**: auto-refresh table; multi-colour PROGRESS bar; full sort; resizable TITLE;
  multi-field filter form with owner/projects autocomplete + a spool-date window
  (Spooled >/< accepting relative tokens like `-24h`/`-7d` or absolute dates);
  my/errors toggles;
  metadata + lock/notes panel; FLG column (W watch / L lock / N note); farm summary
  line; relative-time toggle.
- **Tasks**: list (sort/search), info panel (RSS/VSZ/CPU/slots/cmd/env/service via
  `q=taskdetails`), multi-select.
- **Logs**: syntax colour, search (`n`/`N`), wrap/horizontal-scroll toggle, line
  numbers, `$PAGER`.
- **Actions** (`a`): job (pause/unpause, retry errored/active, skip errored, set
  priority, lock/unlock, interrupt, restart, delete), task (retry/resume/skip/kill),
  blade (nimby/unnimby, eject, delist); confirmation for destructive; **bulk** across
  a selection (Space/A select, aggregate result).
- **Blades** (`b`): list, info panel, **heatmap** (`g`; idle=light-grey, active=green,
  nimby=yellow, down=grey; selection = red side-frames), running-commands detail (Enter),
  trace (`t`).
- **QoL**: watch+notify (`w`, bell+banner), clipboard yank (`y`; task view chooses
  Command/Env/Service), saved filters (`s`/`f`), persisted layout, mouse wheel.

### Keymap (global keys configurable via `keys:`)
`/` filter/search · `Enter` drill in · `Esc` back · `a` actions · `Space`/`A` select ·
`w` watch · `y` copy · `n` notes · `b` blades · `g` heatmap · `t` trace · `o` pager ·
`s`/`f` save/load filter · `[`/`]` prev/next sibling · `1`–`9` sort · `<`/`>` TITLE width ·
`T` rel-time · `R` refresh · `?` help · `q` quit. (`?` always lists the live keys.)

---

## Known issues / tech debt
- **Log auth**: log URIs fetched with the `TractorUser` cookie; a site whose logging
  service needs different auth may need work.
- **Selected jobs row** PROGRESS bar renders monochrome (keeps Bubble Tea's selection
  highlight instead of per-segment colour).
- **Log wrapping** is character-based (no word-aware wrap).
- **OSC 52 clipboard** is blocked by VTE terminals (MATE) → relies on `xclip` etc.
- Filtered job results lose FLG (lock/note) flags (not in the `tractorselect` columns).

## Possible next steps (optional)
- Tag `v0.1.0` (`git tag v0.1.0 && git push --tags` from a write-access machine;
  optional `goreleaser release --clean`).
- Delay/undelay job actions (parked); per-command listing in task/blade panels;
  word-aware log wrap; configurable view-internal keys; permission/ownership guard
  (currently engine-enforced, error surfaced).

_Last updated: 2026-06-15 — all phases complete; Windows dev box now has a Go
toolchain (Go 1.26.4) for local build/vet/test._
