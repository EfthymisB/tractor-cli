package api

import (
	"fmt"
	"net/url"
)

// FetchLog retrieves the log for a specific task.
func (c *Client) FetchLog(jid int, tid int, taskName string) (string, error) {
	params := url.Values{
		"jid":  {fmt.Sprintf("%d", jid)},
		"tid":  {fmt.Sprintf("%d", tid)},
		"task": {taskName},
	}
	body, err := c.get("/tractor/log", params)
	if err != nil {
		return "", fmt.Errorf("FetchLog: %w", err)
	}
	return string(body), nil
}
