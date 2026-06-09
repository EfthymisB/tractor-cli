package api

import (
	"encoding/json"
	"fmt"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

// jtreeResponse is the envelope returned by /Tractor/monitor?q=jtree.
type jtreeResponse struct {
	RC   int           `json:"rc"`
	Msg  string        `json:"msg"`
	Tasks []models.Task `json:"tasks"`
}

// QueryTasks returns all tasks for a given job ID.
// Reference: GET /Tractor/monitor?q=jtree&jid=1234
func (c *Client) QueryTasks(jid int) ([]models.Task, error) {
	params := c.authenticatedParams()
	params.Set("q", "jtree")
	params.Set("jid", fmt.Sprintf("%d", jid))

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return nil, fmt.Errorf("QueryTasks: %w", err)
	}

	var resp jtreeResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("QueryTasks: parsing response: %w", err)
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("QueryTasks: engine error (rc=%d): %s", resp.RC, resp.Msg)
	}
	return resp.Tasks, nil
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
