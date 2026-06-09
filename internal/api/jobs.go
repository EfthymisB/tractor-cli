package api

import (
	"encoding/json"
	"fmt"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

// jobsResponse is the envelope returned by /Tractor/monitor?q=jobs.
type jobsResponse struct {
	RC   int          `json:"rc"`
	Msg  string       `json:"msg"`
	Jobs []models.Job `json:"jobs"`
}

// JobFilter contains optional filter parameters for QueryJobs.
type JobFilter struct {
	Owner  string
	Status string
	Title  string
}

// QueryJobs returns jobs from the engine, optionally filtered by owner.
// Reference: GET /Tractor/monitor?q=jobs&owner=NAME&tsid=SSS
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

	// Try structured envelope first.
	var resp jobsResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("QueryJobs: parsing response: %w", err)
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("QueryJobs: engine error (rc=%d): %s", resp.RC, resp.Msg)
	}

	jobs := resp.Jobs

	// Client-side filtering for status and title (not supported as server params).
	if filter.Status != "" || filter.Title != "" {
		filtered := jobs[:0]
		for _, j := range jobs {
			if filter.Status != "" && j.Status != filter.Status {
				continue
			}
			if filter.Title != "" && !containsFold(j.Title, filter.Title) {
				continue
			}
			filtered = append(filtered, j)
		}
		jobs = filtered
	}

	return jobs, nil
}

func containsFold(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(substr) == 0 ||
		containsCI(s, substr))
}

func containsCI(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
