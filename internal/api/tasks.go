package api

import (
	"encoding/json"
	"fmt"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

// taskRow mirrors a row returned by q=select&table=task.
type taskRow struct {
	TID        int         `json:"tid"`
	JID        int         `json:"jid"`
	Title      string      `json:"title"`
	State      string      `json:"state"`
	Service    string      `json:"service"`
	Progress   float64     `json:"progress"`
	ActiveTime interface{} `json:"activetime"` // timestamp or null
	ReadyTime  interface{} `json:"readytime"`  // timestamp or null
	RetryCount int         `json:"retrycount"`
	HasLog     bool        `json:"haslog"`
}

func (r taskRow) toTask() models.Task {
	return models.Task{
		TID:        r.TID,
		JID:        r.JID,
		Title:      r.Title,
		State:      r.State,
		Service:    r.Service,
		Progress:   r.Progress,
		ActiveTime: timestampString(r.ActiveTime),
		ReadyTime:  timestampString(r.ReadyTime),
		RetryCount: r.RetryCount,
		HasLog:     r.HasLog,
	}
}

// QueryTasks returns all tasks for a given job ID, ordered by task id.
// Uses q=select&table=task (the same table-level query used for jobs).
func (c *Client) QueryTasks(jid int) ([]models.Task, error) {
	params := c.authenticatedParams()
	params.Set("q", "select")
	params.Set("table", "task")
	params.Set("where", fmt.Sprintf("jid=%d", jid))
	params.Set("orderby", "tid")
	params.Set("limit", "100000")

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return nil, fmt.Errorf("QueryTasks: %w", err)
	}

	var resp struct {
		RC   int       `json:"rc"`
		Rows []taskRow `json:"rows"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("QueryTasks: parsing response: %w", err)
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("QueryTasks: engine returned rc=%d", resp.RC)
	}

	tasks := make([]models.Task, 0, len(resp.Rows))
	for _, r := range resp.Rows {
		tasks = append(tasks, r.toTask())
	}
	return tasks, nil
}

// timestampString normalises a Postgres timestamp value (which may be null) to
// the "2006-01-02 15:04:05" form the UI's formatTime understands.
func timestampString(v interface{}) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	if len(s) >= 19 {
		return s[:19]
	}
	return s
}

// TaskDetails returns the commands associated with a specific task.
// Reference: GET /Tractor/monitor?q=taskdetails&jid=1234&tid=11
func (c *Client) TaskDetails(jid, tid int) ([]json.RawMessage, error) {
	params := c.authenticatedParams()
	params.Set("q", "taskdetails")
	params.Set("jid", fmt.Sprintf("%d", jid))
	params.Set("tid", fmt.Sprintf("%d", tid))

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return nil, fmt.Errorf("TaskDetails: %w", err)
	}

	var resp struct {
		RC   int               `json:"rc"`
		Msg  string            `json:"msg"`
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("TaskDetails: parsing response: %w", err)
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("TaskDetails: engine error (rc=%d): %s", resp.RC, resp.Msg)
	}
	return resp.Data, nil
}
