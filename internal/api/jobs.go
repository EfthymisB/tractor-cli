package api

import (
	"encoding/json"
	"fmt"
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
	JID      int     `json:"jid"`
	Title    string  `json:"title"`
	User     string  `json:"user"`
	State    string  `json:"state"`
	Priority float64 `json:"priority"`
	SpoolDate string `json:"spooldate"`
	Paused   int     `json:"paused"`
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
		JID:      r.JID,
		Title:    r.Title,
		Owner:    r.User,
		Status:   deriveStatus(r.Paused, numActive, numDone, numTotal, numError),
		Priority: r.Priority,
		SpoolTime: r.SpoolDate,
		NumTasks: numTotal,
		NumDone:  numDone,
		NumError: numError,
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

// QueryJobs returns jobs from the engine, optionally filtered by owner.
// Response shape: {"users": {"username": {"jid_str": {"data": {...}}}}}
func (c *Client) QueryJobs(filter JobFilter) ([]models.Job, error) {
	params := c.authenticatedParams()
	params.Set("q", "jobs")
	if filter.Owner != "" {
		params.Set("owner", filter.Owner)
	}

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return nil, fmt.Errorf("QueryJobs: %w", err)
	}

	// Parse the nested envelope.
	var envelope struct {
		Users map[string]map[string]struct {
			Data rawJobData `json:"data"`
		} `json:"users"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return nil, fmt.Errorf("QueryJobs: parsing response: %w", err)
	}

	jobs := make([]models.Job, 0)
	for _, userJobs := range envelope.Users {
		for _, entry := range userJobs {
			jobs = append(jobs, entry.Data.toJob())
		}
	}

	// Client-side filtering for status and title.
	if filter.Status != "" || filter.Title != "" {
		filtered := jobs[:0]
		for _, j := range jobs {
			if filter.Status != "" && j.Status != filter.Status {
				continue
			}
			if filter.Title != "" && !strings.Contains(strings.ToLower(j.Title), strings.ToLower(filter.Title)) {
				continue
			}
			filtered = append(filtered, j)
		}
		jobs = filtered
	}

	return jobs, nil
}
