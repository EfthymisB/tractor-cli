package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
	Status   string   // exact status; "" = no filter
	Title    string   // case-insensitive substring; "" = no filter
	JIDs     []int    // job must have one of these JIDs
	JIDFrom  int      // inclusive range start; 0 = unset
	JIDTo    int      // inclusive range end; 0 = unset
	Priority float64  // exact priority match; 0 = no filter
	Projects []string // job.Projects must contain one of these (case-insensitive)
}

// IsEmpty reports whether no filter criteria are set.
func (f JobFilter) IsEmpty() bool {
	return len(f.Owners) == 0 && f.Status == "" && f.Title == "" &&
		len(f.JIDs) == 0 && f.JIDFrom == 0 && f.JIDTo == 0 &&
		f.Priority == 0 && len(f.Projects) == 0
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
		Metadata:  formatMetadata(r.Metadata),
		NumTasks:  numTotal,
		NumDone:   numDone,
		NumError:  numError,
	}
}

// selectJobRow mirrors the flat row format returned by q=select&table=Job.
// Field names match raw database column names (not the jobinfo view aliases).
type selectJobRow struct {
	JID       int         `json:"jid"`
	Title     string      `json:"title"`
	Owner     string      `json:"owner"`
	Priority  float64     `json:"priority"`
	SpoolTime string      `json:"spooltime"` // full Postgres timestamp; first 19 chars match our parse format
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
	spoolDate := r.SpoolTime
	if len(spoolDate) >= 19 {
		spoolDate = spoolDate[:19]
	}
	return models.Job{
		JID:       r.JID,
		Title:     r.Title,
		Owner:     r.Owner,
		Status:    deriveStatus(paused, r.NumActive, r.NumDone, r.NumTasks, r.NumError),
		Priority:  r.Priority,
		SpoolTime: spoolDate,
		SpoolHost: r.SpoolHost,
		Projects:  formatProjects(r.Projects),
		Metadata:  formatMetadata(r.Metadata),
		NumTasks:  r.NumTasks,
		NumDone:   r.NumDone,
		NumError:  r.NumError,
	}
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

// queryJobsSelect uses q=select&table=Job for filtered queries with a high row
// limit. This bypasses the engine's default q=jobs result cap.
func (c *Client) queryJobsSelect(filter JobFilter) ([]models.Job, error) {
	params := c.authenticatedParams()
	params.Set("q", "select")
	params.Set("table", "Job")
	params.Set("limit", "25000")
	params.Set("orderby", "jid desc")

	if where := buildSelectWhere(filter); where != "" {
		params.Set("where", where)
	}

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return nil, fmt.Errorf("queryJobsSelect: %w", err)
	}

	var resp struct {
		RC   int            `json:"rc"`
		Rows []selectJobRow `json:"rows"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("queryJobsSelect: parsing response: %w", err)
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("queryJobsSelect: engine returned rc=%d", resp.RC)
	}
	if resp.Rows == nil {
		return nil, fmt.Errorf("queryJobsSelect: no rows field in response")
	}

	jobs := make([]models.Job, 0, len(resp.Rows))
	for _, r := range resp.Rows {
		jobs = append(jobs, r.toJob())
	}
	// Projects filter is client-side only (array contains not easily expressed
	// in Tractor's WHERE language).
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

	// Status (Tractor WHERE aliases — bare keywords, not SQL).
	switch f.Status {
	case "active":
		parts = append(parts, "active")
	case "error":
		parts = append(parts, "error")
	case "done":
		parts = append(parts, "done")
	case "blocked":
		parts = append(parts, "blocked")
	case "paused":
		parts = append(parts, "pausetime is not null")
	}

	// Priority — exact numeric match.
	if f.Priority != 0 {
		parts = append(parts, fmt.Sprintf("priority=%g", f.Priority))
	}

	// Title — regex substring match via Tractor's 'like' operator.
	if f.Title != "" {
		parts = append(parts, "title like "+f.Title)
	}

	return strings.Join(parts, " and ")
}

// applyClientFilters filters a job slice by all fields in f.
// Used for both the q=jobs fallback and projects filtering after q=select.
func applyClientFilters(jobs []models.Job, f JobFilter) []models.Job {
	if f.IsEmpty() {
		return jobs
	}
	out := jobs[:0]
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

	// Status: exact match.
	if f.Status != "" && j.Status != f.Status {
		return false
	}

	// Priority: exact match.
	if f.Priority != 0 && j.Priority != f.Priority {
		return false
	}

	// Title: case-insensitive substring.
	if f.Title != "" && !strings.Contains(strings.ToLower(j.Title), strings.ToLower(f.Title)) {
		return false
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

// formatMetadata formats the metadata TextField for display.  When the value
// is a JSON object (the common pipeline convention) the dict values are sorted
// by key and joined with "/" — e.g. {"show":"film","seq":"abc","shot":"001"}
// becomes "film/abc/001".  Plain strings are returned as-is.
func formatMetadata(s string) string {
	if s == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil || len(m) == 0 {
		return s
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%v", m[k]))
	}
	return strings.Join(parts, "/")
}
