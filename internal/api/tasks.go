package api

import (
	"encoding/json"
	"fmt"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

const taskColumns = "tid,jid,title,status,bladename,starttime,stoptime,rss,progress"

// QueryTasks returns all tasks for a given job ID.
func (c *Client) QueryTasks(jid int) ([]models.Task, error) {
	tql := fmt.Sprintf("tasks where jid=%d", jid)
	items, err := c.query(tql, taskColumns)
	if err != nil {
		return nil, fmt.Errorf("QueryTasks: %w", err)
	}
	tasks := make([]models.Task, 0, len(items))
	for _, raw := range items {
		var t models.Task
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("parsing task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
