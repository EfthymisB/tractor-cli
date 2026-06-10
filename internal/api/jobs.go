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
type JobFilter struct {
	Owner  string
	Status string
	Title  string
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
	if filter.Owner != "" || filter.Status != "" || filter.Title != "" {
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
	if filter.Owner != "" {
		params.Set("owner", filter.Owner)
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

	// Client-side filter for status and title (q=jobs doesn't support them server-side).
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
	return jobs, nil
}

// buildSelectWhere converts a JobFilter into a Tractor tractorselect WHERE clause.
// Tractor WHERE syntax: field='value', aliases like "active", "error", title like '%foo%'.
func buildSelectWhere(f JobFilter) string {
	var parts []string
	if f.Owner != "" {
		// escape single quotes in username to prevent query breakage
		safe := strings.ReplaceAll(f.Owner, "'", "''")
		parts = append(parts, fmt.Sprintf("owner='%s'", safe))
	}
	if f.Status != "" {
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
	}
	if f.Title != "" {
		safe := strings.ReplaceAll(f.Title, "'", "''")
		safe = strings.ReplaceAll(safe, "%", "\\%")
		safe = strings.ReplaceAll(safe, "_", "\\_")
		parts = append(parts, fmt.Sprintf("title like '%%%s%%'", safe))
	}
	return strings.Join(parts, " and ")
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

// applyClientFilters does local filtering for status and title on a job slice.
func applyClientFilters(jobs []models.Job, filter JobFilter) []models.Job {
	if filter.Status == "" && filter.Title == "" {
		return jobs
	}
	out := jobs[:0]
	for _, j := range jobs {
		if filter.Status != "" && j.Status != filter.Status {
			continue
		}
		if filter.Title != "" && !strings.Contains(strings.ToLower(j.Title), strings.ToLower(filter.Title)) {
			continue
		}
		out = append(out, j)
	}
	return out
}
