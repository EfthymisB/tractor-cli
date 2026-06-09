package api

import (
	"encoding/json"
	"fmt"
)

// taskLogsResponse is the envelope returned by /Tractor/monitor?q=tasklogs.
type taskLogsResponse struct {
	RC  int    `json:"rc"`
	Msg string `json:"msg"`
	Log string `json:"log"`
	URL string `json:"url"`
}

// FetchLog retrieves the log for a specific task.
// Reference: GET /Tractor/monitor?q=tasklogs&owner=name&jid=1234&tid=11
func (c *Client) FetchLog(owner string, jid, tid int) (string, error) {
	params := c.authenticatedParams()
	params.Set("q", "tasklogs")
	params.Set("owner", owner)
	params.Set("jid", fmt.Sprintf("%d", jid))
	params.Set("tid", fmt.Sprintf("%d", tid))

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return "", fmt.Errorf("FetchLog: %w", err)
	}

	// The engine may return the log URL or inline log content.
	var resp taskLogsResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		// Plain-text log returned directly.
		return string(b), nil
	}
	if resp.RC != 0 {
		return "", fmt.Errorf("FetchLog: engine error (rc=%d): %s", resp.RC, resp.Msg)
	}
	if resp.Log != "" {
		return resp.Log, nil
	}
	// If only a URL was returned, fetch the actual log content.
	if resp.URL != "" {
		content, err := c.get(resp.URL, nil)
		if err != nil {
			return "", fmt.Errorf("FetchLog: fetching log URL: %w", err)
		}
		return string(content), nil
	}
	return "", nil
}

// parseRC checks a raw JSON response for rc/msg fields and returns an error if rc != 0.
func parseRC(b []byte) error {
	var r struct {
		RC  int    `json:"rc"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil // non-JSON response treated as success
	}
	if r.RC != 0 {
		return fmt.Errorf("rc=%d: %s", r.RC, r.Msg)
	}
	return nil
}
