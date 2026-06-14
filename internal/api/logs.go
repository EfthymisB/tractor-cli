package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// taskLogsResponse is the envelope returned by /Tractor/monitor?q=tasklogs. The
// engine answers with one or more logging-service URIs rather than inline text.
type taskLogsResponse struct {
	RC              int      `json:"rc"`
	Msg             string   `json:"msg"`
	LoggingRedirect []string `json:"LoggingRedirect"`
}

// TaskLog retrieves and concatenates the command logs for a task. The engine
// returns log URIs (LoggingRedirect); each is fetched and joined in order.
// owner is optional and may be "".
func (c *Client) TaskLog(jid, tid int, owner string) (string, error) {
	params := c.authenticatedParams()
	params.Set("q", "tasklogs")
	params.Set("jid", strconv.Itoa(jid))
	params.Set("tid", strconv.Itoa(tid))
	if owner != "" {
		params.Set("owner", owner)
	}

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return "", fmt.Errorf("TaskLog: %w", err)
	}

	var resp taskLogsResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		// Some engines may return the log text directly.
		return string(b), nil
	}
	if resp.RC != 0 {
		return "", fmt.Errorf("TaskLog: engine error (rc=%d): %s", resp.RC, resp.Msg)
	}
	if len(resp.LoggingRedirect) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, uri := range resp.LoggingRedirect {
		// The engine leaves a "%u" placeholder for the job owner in the log
		// path; substitute it (an unescaped "%u" is also an invalid URL escape).
		if owner != "" {
			uri = strings.ReplaceAll(uri, "%u", owner)
		}
		body, err := c.getLogURI(uri)
		if err != nil {
			fmt.Fprintf(&sb, "\n[tractor-cli: error fetching log segment %s: %v]\n", uri, err)
			continue
		}
		sb.Write(body)
	}
	return sb.String(), nil
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
