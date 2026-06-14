# tractor-cli — Roadmap

An interactive terminal UI (Go + Bubble Tea) for Pixar's Tractor render farm.
Developed on Windows, deployed on Linux (Alma 9).

_Last updated: 2026-06-10 (after Phase 3)._

---

## Status at a glance

| Phase | Title | Status |
|-------|-------|--------|
| 1 | Foundation (config, API client, auth, models) | ✅ Done |
| 2 | Jobs list view | ✅ Done |
| 3 | Job detail + task list + task logs | ✅ Done |
| 4 | Actions (retry / kill / delete / skip / pause …) | 🚧 In progress |
| 5 | Blades view | ✅ Done |
| 6 | Polish & release | ⬜ Planned |

---

## Architecture notes

- **TUI**: Bubble Tea (Elm-style Model/Update/View); `bubbles` table, textinput,
  viewport, spinner; `lipgloss` for styling.
- **Entry point**: `cmd/tractor/main.go` (cobra). Flags: `--sanity`, `--raw`.
- **Root model**: `internal/tui/app.go` switches between views
  (`ViewJobsList`, `ViewJobDetail`, `ViewTaskLog`) and owns the status bar +
  help overlay. Key handling consults a shared `capturingInput()` so global
  keys defer to a focused text input.
- **Views**: `internal/tui/views/jobs` (list + detail), `.../filter` (filter
  form), `.../logview` (log viewer).
- **API**: `internal/api` — challenge-response login, `TractorUser` cookie +
  `tsid` on every request.
- **Engine quirks learned**:
  - Job/task queries use `q=jobs` and `q=jtree`; **`q=select` is not a valid
    monitor query** on our engine (returns HTTP 400).
  - Job filtering falls back to `q=jobs` + client-side filtering (the
    high-limit `q=select` path is effectively dead — see Known issues).
  - Task tree (`q=jtree`) packs state as `<log><state>[,<progress>]`
    (e.g. `-A,50`).
  - Task details: `q=taskdetails`; logs: `q=tasklogs` → `LoggingRedirect`
    URIs (with a `%u` owner placeholder to substitute).
  - WHERE clauses use Tractor's natural-language syntax (unquoted values,
    `in [a, b]`, regex `like`), not SQL.

---

## Phase 1 — Foundation ✅

- YAML + env config (`internal/config`); `TRACTOR_PASSWORD` only ever from the
  environment, **never** the config file.
- API client with challenge-response auth (`gentoken` +
  `trInternalPasswordHash` + `TractorUser` cookie).
- Core models; `--sanity` / `--raw` debug paths.

## Phase 2 — Jobs list view ✅

- Auto-refreshing jobs table with per-row status colouring.
- **Columns**: JID, PROGRESS (multi-colour bar: done/err/active/remaining),
  TITLE, OWNER, STATUS, ACT, ERR, DONE, TOTAL, PRI, SPOOLED, PROJECTS,
  SPOOLHOST. Auto-sized to content; TITLE fills remaining width and is
  user-resizable (`<` / `>`).
- **Sorting** by column (number keys `1`–`9`, press again to reverse).
- **Filter form** (`/`) — one field per filter, preserved on reopen:
  - Owner (multi-value, AD/getent + job-owner autocomplete)
  - Status (multi-select chips)
  - JID (list and ranges, e.g. `123, 200-300`)
  - Priority, Projects (autocomplete), Title
- **Owner autocomplete** sourced from Active Directory via `ldapsearch -Y
  GSSAPI` (configurable; opt-in `kinit`), falling back to `getent passwd` and
  always merged with owners present in loaded jobs.
- Quick toggles: `m` (my jobs), `e` (errors only); `c` clears all filters;
  `R` refresh.
- **Bottom info panel**: metadata for the highlighted job.
- **Help overlay** (`?`).

## Phase 3 — Job detail, tasks & logs ✅

- **Task list** (Enter on a job): TID, TITLE, STATE, PROG, BLADE, SERVICE,
  ACTIVE; rows coloured by state.
- Sortable columns (`1`–`7`) and title search (`/`).
- **Task info panel** (`q=taskdetails`): command, blades, RSS/VSZ (MB),
  CPU %, slots in use, service key, env key, min/max slots, min/max run —
  fetched per selection and on refresh.
- **Task log viewer** (Enter on a task):
  - Fetched via `q=tasklogs` (handles `LoggingRedirect` + `%u` owner).
  - VS Code-style syntax colouring (timestamps, log levels, strings, paths,
    URLs, numbers).
  - Search (`/`, `n`/`N`, `c` to clear), `g`/`G` top/bottom, `R` refresh.
  - Line wrapping with a **line-number gutter**; `w` toggles to
    horizontal-scroll mode (`←`/`→`).
- **Sibling navigation**: `[` / `]` jump to previous/next task's log (in the
  log view) and previous/next job's tasks (in the task view); selection in the
  parent view follows along.
- Job and task **titles** shown in headers and the status bar.

---

## Phase 4 — Actions 🚧 (in progress)

Mutating operations on jobs and tasks, via an `a` action menu with an optional
value-input step and a confirmation step for destructive actions.

- [x] Action menu component (`internal/tui/views/action`) + confirmation.
- [x] Job actions: pause/unpause, retry errored, retry active, skip errored,
      interrupt, restart, delete, change priority.
- [x] Task actions: retry, resume, skip, kill.
- [x] Mapped to engine `q=queue` operations (verified against `EngineClient.py`).
- [x] Result + errors surfaced in the status bar; active view refreshes after.
- [ ] Delay / undelay and lock / unlock.
- [x] Multi-select / bulk actions: Space toggles a row, A selects all, Esc
      clears; `a` then applies the chosen op across the whole selection and
      reports an aggregate result.
- [ ] Permission/ownership awareness (currently relies on the engine to reject;
      we surface its error).

## Phase 5 — Blades view ✅

- [x] Blade list (`q=blades`): NAME, STATUS (up/down from heartbeat), NIMBY,
      SLOTS (in-use/total), CPU load, MEM, DISK, TASKS, PROFILE, OWNERS.
- [x] Reached with `b` from the jobs list; `Esc` returns.
- [x] Name search (`/`), column sort (`1`–`9`), row colouring
      (down=grey, nimby=yellow, up=green), auto-refresh.
- [x] Info panel: addr/port, OS, GPU, version, UDI, CPUs, heartbeat, uptime,
      note.
- [x] Blade actions via `a`: nimby/unnimby, eject (confirm), delist (confirm).
- [x] Blade detail view (Enter): the blade's running commands (`q=bladedetails`)
      — JID, TID, OWNER, SLOTS, STARTED, COMMAND; searchable and sortable.
- [ ] Trace output (`q=tracer`).

## Phase 6 — Polish & release ⬜

- [ ] README + usage docs; document config + `TRACTOR_PASSWORD` rule.
- [ ] `goreleaser` builds; CI (build/test/lint).
- [ ] Configurable keymap; consistent help.
- [ ] Tests for API parsing and filter logic.

---

## Known issues / tech debt

- **`q=select` is unsupported** on our engine, so filtered job queries fall
  back to `q=jobs` (capped at the engine default, ~2500). The high-limit
  filtered path is dead code and still costs a failing request per filtered
  search — should be removed, and investigate whether the cap can be raised.
- **Owner-in-`%u` / log auth**: log URIs are fetched with the `TractorUser`
  cookie; if a site's logging service needs different auth this may need work.
- **Selected row** in the jobs PROGRESS column renders monochrome (the row
  keeps the Bubble Tea selection highlight instead of per-segment colour).
- **Long log lines**: wrap is character-based; no word-aware wrapping.
- Cannot compile on the Windows dev box — every change is verified by building
  on the Linux farm (`go run ./cmd/tractor/`).

## Conventions

- Commit and push to `origin` after each change.
- PowerShell on Windows; code transferred/built on Linux.
