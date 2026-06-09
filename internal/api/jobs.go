package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

const jobColumns = "jid,title,owner,status,priority,spooltime,starttime,stoptime,numerror,numtasks,numdone"

// JobFilter contains optional filter parameters for QueryJobs.
type JobFilter struct {
	Owner  string
	Status string
	Title  string
}

// tql builds the TQL expression for a job query.
func (f JobFilter) tql() string {
	base := "jobs"
	var clauses []string
	if f.Owner != "" {
		clauses = append(clauses, fmt.Sprintf("owner='%s'", f.Owner))
	}
	if f.Status != "" {
		clauses = append(clauses, fmt.Sprintf("status='%s'", f.Status))
	}
	if f.Title != "" {
		clauses = append(clauses, fmt.Sprintf("title like '%%%s%%'", f.Title))
	}
	if len(clauses) == 0 {
		return base
	}
	return base + " where " + strings.Join(clauses, " and ")
}

// QueryJobs returns all jobs matching the given filter.
func (c *Client) QueryJobs(filter JobFilter) ([]models.Job, error) {
	items, err := c.query(filter.tql(), jobColumns)
	if err != nil {
		return nil, fmt.Errorf("QueryJobs: %w", err)
	}
	jobs := make([]models.Job, 0, len(items))
	for _, raw := range items {
		var j models.Job
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("parsing job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
