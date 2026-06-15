package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

// RawJobsResponse returns the raw JSON body of /Tractor/monitor?q=jobs for debugging.
func (c *Client) RawJobsResponse() ([]byte, error) {
	params := c.authenticatedParams()
	params.Set("q", "jobs")
	return c.get("/Tractor/monitor", params)
}

// JobFilter contains optional filter parameters for QueryJobs.
// All slice fields use OR semantics within the field; all fields AND together.
type JobFilter struct {
	Owners   []string // owner must match one of these (case-insensitive)
	Statuses []string // job status must be one of these; empty = no filter
	Title    string   // case-insensitive substring; "" = no filter
	JIDs     []int    // job must have one of these JIDs
	JIDFrom  int      // inclusive range start; 0 = unset
	JIDTo    int      // inclusive range end; 0 = unset
	Priority float64  // exact priority match; 0 = no filter
	Projects []string // job.Projects must contain one of these (case-insensitive)

	// SpoolAfter / SpoolBefore are raw Tractor time tokens compared against the
	// job spooltime. Tractor's WHERE language parses both relative tokens
	// ("-24h", "-7d", "-1w", "-30m") and absolute dates ("3/15/25", "5pm").
	// "" = no filter. SpoolAfter maps to "spooltime > X", SpoolBefore to
	// "spooltime < X".
	SpoolAfter  string
	SpoolBefore string
}

// IsEmpty reports whether no filter criteria are set.
func (f JobFilter) IsEmpty() bool {
	return len(f.Owners) == 0 && len(f.Statuses) == 0 && f.Title == "" &&
		len(f.JIDs) == 0 && f.JIDFrom == 0 && f.JIDTo == 0 &&
		f.Priority == 0 && len(f.Projects) == 0 &&
		f.SpoolAfter == "" && f.SpoolBefore == ""
}

// rawJobData mirrors the actual JSON shape inside each job's "data" object.
type rawJobData struct {
	JID       int         `json:"jid"`
	Title     string      `json:"title"`
	User      string      `json:"user"`
	State     string      `json:"state"`
	Priority  float64     `json:"priority"`
	SpoolDate string      `json:"spooldate"`
	SpoolHost string      `json:"spoolhost"`
	Paused    int         `json:"paused"`
	Projects  interface{} `json:"projects"` // JSON array or null
	Metadata  string      `json:"metadata"` // arbitrary text, may be JSON
	LastNote  int         `json:"lastnoteid"`
	LockOwner string      `json:"lockowner"`
	LockNote  string      `json:"locknote"`
	// [total, active, done, error]
	NTasks []int `json:"nTasks"`
}

func (r rawJobData) toJob() models.Job {
	var numTotal, numActive, numDone, numError int
	if len(r.NTasks) >= 4 {
		numTotal = r.NTasks[0]
		numActive = r.NTasks[1]
		numDone = r.NTasks[2]
		numError = r.NTasks[3]
	}
	return models.Job{
		JID:       r.JID,
		Title:     r.Title,
		Owner:     r.User,
		Status:    deriveStatus(r.Paused, numActive, numDone, numTotal, numError),
		Priority:  r.Priority,
		SpoolTime: r.SpoolDate,
		SpoolHost: r.SpoolHost,
		Projects:  formatProjects(r.Projects),
		Metadata:  r.Metadata,
		NumTasks:  numTotal,
		NumDone:   numDone,
		NumError:  numError,
		NumActive: numActive,
		HasNote:   r.LastNote != 0,
		Locked:    r.LockOwner != "",
		LockOwner: r.LockOwner,
		LockNote:  r.LockNote,
	}
}

// selectJobRow mirrors the flat row format returned by q=select&table=Job.
// Field names match raw database column names (not the jobinfo view aliases).
type selectJobRow struct {
	JID       int         `json:"jid"`
	Title     string      `json:"title"`
	Owner     string      `json:"owner"`
	Priority  float64     `json:"priority"`
	SpoolTime interface{} `json:"spooltime"` // string timestamp or epoch number
	SpoolHost string      `json:"spoolhost"`
	NumActive int         `json:"numactive"`
	NumDone   int         `json:"numdone"`
	NumTasks  int         `json:"numtasks"`
	NumError  int         `json:"numerror"`
	Projects  interface{} `json:"projects"` // JSON array or null
	Metadata  string      `json:"metadata"`
	// pausetime is a Postgres timestamp when paused, null otherwise.
	PauseTime interface{} `json:"pausetime"`
}

func (r selectJobRow) toJob() models.Job {
	paused := 0
	if r.PauseTime != nil {
		paused = 1
	}
	spoolDate := spoolTimeString(r.SpoolTime)
	return models.Job{
		JID:       r.JID,
		Title:     r.Title,
		Owner:     r.Owner,
		Status:    deriveStatus(paused, r.NumActive, r.NumDone, r.NumTasks, r.NumError),
		Priority:  r.Priority,
		SpoolTime: spoolDate,
		SpoolHost: r.SpoolHost,
		Projects:  formatProjects(r.Projects),
		Metadata:  r.Metadata,
		NumTasks:  r.NumTasks,
		NumDone:   r.NumDone,
		NumError:  r.NumError,
		NumActive: r.NumActive,
	}
}

// spoolTimeString normalises a spooltime value (string timestamp or epoch
// number) to the "2006-01-02 15:04:05" form the UI's formatTime parses.
func spoolTimeString(v interface{}) string {
	switch t := v.(type) {
	case string:
		if len(t) >= 19 {
			return t[:19]
		}
		return t
	case float64:
		if t <= 0 {
			return ""
		}
		return time.Unix(int64(t), 0).Format("2006-01-02 15:04:05")
	}
	return ""
}

// parseJobSpoolTime parses a job's spooltime display string ("2006-01-02
// 15:04:05", optionally with a trailing fraction/zone) into a time.Time.
func parseJobSpoolTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if len(s) >= len(layout) {
			if t, err := time.ParseInLocation(layout, s[:len(layout)], time.Local); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// parseSpoolToken converts a Tractor time token into an absolute time.Time for
// client-side comparison. It understands relative tokens ("-24h", "-7d",
// "-1w", "-30m", "-15s") and a few common absolute date forms. Tractor's own
// parser is richer; unrecognised tokens return ok=false so the caller skips the
// criterion client-side (the server-side query still applies it).
func parseSpoolToken(tok string) (time.Time, bool) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return time.Time{}, false
	}
	if strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "+") {
		if d, ok := parseRelativeDuration(tok); ok {
			return time.Now().Add(d), true
		}
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"1/2/2006",
		"1/2/06",
	} {
		if t, err := time.ParseInLocation(layout, tok, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseRelativeDuration parses a signed relative token like "-24h", "-7d",
// "-1w", "-30m" or "-15s" into a time.Duration (negative = into the past).
func parseRelativeDuration(tok string) (time.Duration, bool) {
	if len(tok) < 3 {
		return 0, false
	}
	sign := time.Duration(1)
	if tok[0] == '-' {
		sign = -1
	}
	body := tok[1:]
	unit := body[len(body)-1]
	n, err := strconv.Atoi(body[:len(body)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	var per time.Duration
	switch unit {
	case 's':
		per = time.Second
	case 'm':
		per = time.Minute
	case 'h':
		per = time.Hour
	case 'd':
		per = 24 * time.Hour
	case 'w':
		per = 7 * 24 * time.Hour
	default:
		return 0, false
	}
	return sign * time.Duration(n) * per, true
}

// deriveStatus maps Tractor's internal nTasks counts to a human-readable status.
func deriveStatus(paused, numActive, numDone, numTotal, numError int) string {
	if paused == 1 {
		return "paused"
	}
	if numError > 0 {
		return "error"
	}
	if numActive > 0 {
		return "active"
	}
	if numTotal > 0 && numDone == numTotal {
		return "done"
	}
	return "blocked"
}

// QueryJobs returns jobs from the engine, optionally filtered.
// When any filter is set it uses q=select (table-level query, limit 25 000) so
// results are not capped by the engine's default q=jobs limit (~2 500).
// Falls back to q=jobs if q=select returns an error or unexpected payload.
func (c *Client) QueryJobs(filter JobFilter) ([]models.Job, error) {
	if !filter.IsEmpty() {
		jobs, err := c.queryJobsSelect(filter)
		if err == nil {
			return jobs, nil
		}
		// fall through to q=jobs on failure
	}
	return c.queryJobsList(filter)
}

// queryJobsList uses q=jobs (nested users→jid envelope, default engine limit).
func (c *Client) queryJobsList(filter JobFilter) ([]models.Job, error) {
	params := c.authenticatedParams()
	params.Set("q", "jobs")
	// Pass a single owner to the engine when we can; multiple owners or other
	// filter types are handled client-side after fetching the full list.
	if len(filter.Owners) == 1 {
		params.Set("owner", filter.Owners[0])
	}

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return nil, fmt.Errorf("queryJobsList: %w", err)
	}

	var envelope struct {
		Users map[string]map[string]struct {
			Data rawJobData `json:"data"`
		} `json:"users"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return nil, fmt.Errorf("queryJobsList: parsing response: %w", err)
	}

	jobs := make([]models.Job, 0)
	for _, userJobs := range envelope.Users {
		for _, entry := range userJobs {
			jobs = append(jobs, entry.Data.toJob())
		}
	}

	return applyClientFilters(jobs, filter), nil
}

// selectJobColumns are the job-table columns fetched for filtered queries.
const selectJobColumns = "jid,title,owner,priority,spooltime,spoolhost," +
	"numactive,numdone,numtasks,numerror,projects,metadata,pausetime"

// queryJobsSelect runs a filtered job query via tractorselect over /Tractor/db,
// which has no ~2500 result cap (unlike q=jobs). buildSelectWhere already emits
// Tractor's natural-language WHERE syntax, which is exactly what tractorselect
// expects.
func (c *Client) queryJobsSelect(filter JobFilter) ([]models.Job, error) {
	where := buildSelectWhere(filter)
	if where == "" {
		// No server-expressible criteria (e.g. projects-only): fall back.
		return c.queryJobsList(filter)
	}

	raw, err := c.tractorSelect("job", where, selectJobColumns, "jid desc")
	if err != nil {
		return nil, fmt.Errorf("queryJobsSelect: %w", err)
	}
	var rows []selectJobRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("queryJobsSelect: parsing rows: %w", err)
	}

	jobs := make([]models.Job, 0, len(rows))
	for _, r := range rows {
		jobs = append(jobs, r.toJob())
	}
	// Projects filter is client-side only (array overlap isn't expressible in
	// the WHERE language).
	return applyClientFilters(jobs, filter), nil
}

// buildSelectWhere converts a JobFilter into a Tractor tractorselect WHERE clause.
//
// Tractor uses a "natural language" query format (NOT SQL literal syntax):
//   - String equality:  owner=alice       (no quotes; Tractor auto-quotes)
//   - List match:       owner in [alice, bob]
//   - Regex match:      title like nemo   (no % wildcards)
//   - Numeric:          jid=2207510  priority=1000  jid>=1000 and jid<=2000
//   - Status aliases:   active  error  done  blocked  (bare keywords)
//
// Projects is excluded — array-contains is not expressible here; handled client-side.
func buildSelectWhere(f JobFilter) string {
	var parts []string

	// Owner(s).
	switch len(f.Owners) {
	case 1:
		parts = append(parts, "owner="+f.Owners[0])
	default:
		if len(f.Owners) > 1 {
			parts = append(parts, "owner in ["+strings.Join(f.Owners, ", ")+"]")
		}
	}

	// JID(s) and/or range.
	hasJIDs := len(f.JIDs) > 0
	hasRange := f.JIDFrom > 0 && f.JIDTo > 0
	switch {
	case hasJIDs && hasRange:
		// Mixed: build an OR expression wrapped in parens.
		jidStrs := make([]string, len(f.JIDs))
		for i, id := range f.JIDs {
			jidStrs[i] = strconv.Itoa(id)
		}
		parts = append(parts, fmt.Sprintf(
			"(jid in [%s] or (jid>=%d and jid<=%d))",
			strings.Join(jidStrs, ", "), f.JIDFrom, f.JIDTo,
		))
	case hasJIDs && len(f.JIDs) == 1:
		parts = append(parts, fmt.Sprintf("jid=%d", f.JIDs[0]))
	case hasJIDs:
		jidStrs := make([]string, len(f.JIDs))
		for i, id := range f.JIDs {
			jidStrs[i] = strconv.Itoa(id)
		}
		parts = append(parts, "jid in ["+strings.Join(jidStrs, ", ")+"]")
	case hasRange:
		parts = append(parts, fmt.Sprintf("jid>=%d and jid<=%d", f.JIDFrom, f.JIDTo))
	}

	// Status(es) — OR'd together using Tractor WHERE aliases (bare keywords).
	if len(f.Statuses) > 0 {
		var statusParts []string
		for _, s := range f.Statuses {
			switch s {
			case "active", "error", "done", "blocked":
				statusParts = append(statusParts, s)
			case "paused":
				statusParts = append(statusParts, "pausetime is not null")
			}
		}
		switch len(statusParts) {
		case 1:
			parts = append(parts, statusParts[0])
		case 0:
			// nothing recognised
		default:
			parts = append(parts, "("+strings.Join(statusParts, " or ")+")")
		}
	}

	// Priority — exact numeric match.
	if f.Priority != 0 {
		parts = append(parts, fmt.Sprintf("priority=%g", f.Priority))
	}

	// Title — regex substring match via Tractor's 'like' operator.
	if f.Title != "" {
		parts = append(parts, "title like "+f.Title)
	}

	// Spool date window. The token (relative "-24h" or absolute "3/15/25") is
	// passed verbatim — Tractor's WHERE parser understands both forms.
	if s := strings.TrimSpace(f.SpoolAfter); s != "" {
		parts = append(parts, "spooltime > "+s)
	}
	if s := strings.TrimSpace(f.SpoolBefore); s != "" {
		parts = append(parts, "spooltime < "+s)
	}

	return strings.Join(parts, " and ")
}

// applyClientFilters filters a job slice by all fields in f.
// Used for both the q=jobs fallback and projects filtering after q=select.
func applyClientFilters(jobs []models.Job, f JobFilter) []models.Job {
	if f.IsEmpty() {
		return jobs
	}
	out := make([]models.Job, 0, len(jobs))
	for _, j := range jobs {
		if matchesFilter(j, f) {
			out = append(out, j)
		}
	}
	return out
}

func matchesFilter(j models.Job, f JobFilter) bool {
	// JID: specific list OR range (both conditions are OR'd).
	if len(f.JIDs) > 0 || f.JIDFrom > 0 || f.JIDTo > 0 {
		matched := false
		for _, id := range f.JIDs {
			if j.JID == id {
				matched = true
				break
			}
		}
		if !matched && f.JIDFrom > 0 && f.JIDTo > 0 {
			matched = j.JID >= f.JIDFrom && j.JID <= f.JIDTo
		}
		if !matched {
			return false
		}
	}

	// Owner: any of the listed values (case-insensitive).
	if len(f.Owners) > 0 {
		matched := false
		lo := strings.ToLower(j.Owner)
		for _, o := range f.Owners {
			if lo == strings.ToLower(o) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Status: job status must be one of the selected statuses.
	if len(f.Statuses) > 0 {
		matched := false
		for _, s := range f.Statuses {
			if j.Status == s {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Priority: exact match.
	if f.Priority != 0 && j.Priority != f.Priority {
		return false
	}

	// Title: case-insensitive substring.
	if f.Title != "" && !strings.Contains(strings.ToLower(j.Title), strings.ToLower(f.Title)) {
		return false
	}

	// Spool date window. Best-effort client-side: if either the job spooltime
	// or the filter token can't be parsed here, the criterion is skipped (the
	// authoritative filtering happens server-side via buildSelectWhere; this
	// path is only the q=jobs fallback).
	if f.SpoolAfter != "" || f.SpoolBefore != "" {
		if jt, ok := parseJobSpoolTime(j.SpoolTime); ok {
			if f.SpoolAfter != "" {
				if at, ok := parseSpoolToken(f.SpoolAfter); ok && !jt.After(at) {
					return false
				}
			}
			if f.SpoolBefore != "" {
				if bt, ok := parseSpoolToken(f.SpoolBefore); ok && !jt.Before(bt) {
					return false
				}
			}
		}
	}

	// Projects: job.Projects must contain at least one of the filter terms.
	if len(f.Projects) > 0 {
		matched := false
		lo := strings.ToLower(j.Projects)
		for _, p := range f.Projects {
			if strings.Contains(lo, strings.ToLower(p)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// formatProjects converts the Tractor projects StrArrayField (JSON array or
// null) to a display string.  Multiple projects are joined with commas.
func formatProjects(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []interface{}:
		parts := make([]string, 0, len(val))
		for _, p := range val {
			parts = append(parts, fmt.Sprintf("%v", p))
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprintf("%v", v)
}
