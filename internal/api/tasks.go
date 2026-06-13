package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

// jtree response shapes. q=jtree returns:
//
//	{"users": {"<user>": {"J<jid>": {"data": {...job...}, "children": [task...]}}}}
//
// where each task node is {"data": {...}, "children": [task...]} and the task
// tree is only present when the graph flag is requested.
type jtreeNode struct {
	Data     jtreeTaskData `json:"data"`
	Children []jtreeNode   `json:"children"`
}

type jtreeJob struct {
	Children []jtreeNode `json:"children"`
}

// jtreeTaskData mirrors the per-task "data" object produced by tasktreeForRows.
// "state" is encoded as <log><state>[,<progress>], e.g. "-A,50" or "+D".
type jtreeTaskData struct {
	TID        int     `json:"tid"`
	Title      string  `json:"title"`
	State      string  `json:"state"`
	Service    string  `json:"service"`
	Blade      string  `json:"blade"`
	StateTime  float64 `json:"statetime"`
	ActiveTime float64 `json:"activetime"`
}

// QueryTasks returns all tasks for a given job ID, flattened from the job's
// task tree (q=jtree) and ordered by task id.
func (c *Client) QueryTasks(jid int) ([]models.Task, error) {
	params := c.authenticatedParams()
	params.Set("q", "jtree")
	params.Set("jid", strconv.Itoa(jid))
	// Request the task graph. Different engine builds have used different
	// parameter names for this flag; send both to be safe (unknown query
	// params are ignored by the engine).
	params.Set("graph", "1")
	params.Set("getgraph", "1")
	params.Set("notes", "0")

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return nil, fmt.Errorf("QueryTasks: %w", err)
	}

	var resp struct {
		RC    int                            `json:"rc"`
		Msg   string                         `json:"msg"`
		Users map[string]map[string]jtreeJob `json:"users"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("QueryTasks: parsing response: %w", err)
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("QueryTasks: engine error (rc=%d): %s", resp.RC, resp.Msg)
	}

	var tasks []models.Task
	for _, jobs := range resp.Users {
		for _, job := range jobs {
			flattenTasks(jid, job.Children, &tasks)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TID < tasks[j].TID })
	return tasks, nil
}

// flattenTasks walks the task tree depth-first, appending each node as a Task.
func flattenTasks(jid int, nodes []jtreeNode, out *[]models.Task) {
	for _, n := range nodes {
		state, hasLog, progress := decodeTaskState(n.Data.State)
		*out = append(*out, models.Task{
			TID:        n.Data.TID,
			JID:        jid,
			Title:      n.Data.Title,
			State:      state,
			Service:    n.Data.Service,
			Progress:   progress,
			HasLog:     hasLog,
			Blade:      n.Data.Blade,
			StateTime:  n.Data.StateTime,
			ActiveTime: n.Data.ActiveTime,
		})
		flattenTasks(jid, n.Children, out)
	}
}

// decodeTaskState parses the jtree state encoding "<log><state>[,<progress>]".
// log: '+' has a log, '-' none. state: A=active, B=blocked/ready, D=done,
// E=error. An optional ",<n>" suffix is the progress percentage.
func decodeTaskState(s string) (state string, hasLog bool, progress float64) {
	state = "blocked"
	if s == "" {
		return
	}
	i := 0
	switch s[0] {
	case '+':
		hasLog = true
		i = 1
	case '-':
		i = 1
	}
	if i < len(s) {
		switch s[i] {
		case 'A':
			state = "active"
		case 'B':
			state = "blocked"
		case 'D':
			state = "done"
		case 'E':
			state = "error"
		}
	}
	if idx := strings.IndexByte(s, ','); idx >= 0 && idx+1 < len(s) {
		if p, err := strconv.ParseFloat(s[idx+1:], 64); err == nil {
			progress = p
		}
	}
	return
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
