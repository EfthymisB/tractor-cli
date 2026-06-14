package api

import (
	"fmt"
	"net/url"
	"strconv"
)

// queue issues an action request to /Tractor/queue and checks the engine's rc.
func (c *Client) queue(params url.Values) error {
	b, err := c.get("/Tractor/queue", params)
	if err != nil {
		return err
	}
	return parseRC(b)
}

// jobOp performs a queue operation on a job.
func (c *Client) jobOp(q string, jid int, extra map[string]string) error {
	p := c.authenticatedParams()
	p.Set("q", q)
	p.Set("jid", strconv.Itoa(jid))
	for k, v := range extra {
		p.Set(k, v)
	}
	if err := c.queue(p); err != nil {
		return fmt.Errorf("%s job %d: %w", q, jid, err)
	}
	return nil
}

// taskOp performs a queue operation on a single task.
func (c *Client) taskOp(q string, jid, tid int, extra map[string]string) error {
	p := c.authenticatedParams()
	p.Set("q", q)
	p.Set("jid", strconv.Itoa(jid))
	p.Set("tid", strconv.Itoa(tid))
	for k, v := range extra {
		p.Set(k, v)
	}
	if err := c.queue(p); err != nil {
		return fmt.Errorf("%s task %d/%d: %w", q, jid, tid, err)
	}
	return nil
}

// --- job actions ---

func (c *Client) PauseJob(jid int) error {
	return c.jobOp("jattr", jid, map[string]string{"set_pause": "1"})
}
func (c *Client) UnpauseJob(jid int) error {
	return c.jobOp("jattr", jid, map[string]string{"set_pause": "0"})
}
func (c *Client) RetryErroredTasks(jid int) error {
	return c.jobOp("jretry", jid, map[string]string{"tsubset": "error"})
}
func (c *Client) RetryActiveTasks(jid int) error {
	return c.jobOp("jretry", jid, map[string]string{"tsubset": "active"})
}
func (c *Client) SkipErroredTasks(jid int) error {
	return c.jobOp("tskip", jid, map[string]string{"tsubset": "error"})
}
func (c *Client) InterruptJob(jid int) error {
	return c.jobOp("jinterrupt", jid, nil)
}
func (c *Client) RestartJob(jid int) error {
	return c.jobOp("jrestart", jid, nil)
}
func (c *Client) DeleteJob(jid int) error {
	return c.jobOp("jretire", jid, nil)
}
func (c *Client) SetJobPriority(jid int, priority float64) error {
	return c.jobOp("jattr", jid, map[string]string{
		"set_priority": strconv.FormatFloat(priority, 'g', -1, 64),
	})
}
func (c *Client) LockJob(jid int, note string) error {
	extra := map[string]string{"set_lock": "1"}
	if note != "" {
		extra["note"] = note
	}
	return c.jobOp("jattr", jid, extra)
}
func (c *Client) UnlockJob(jid int) error {
	return c.jobOp("jattr", jid, map[string]string{"set_lock": "0"})
}

// --- task actions ---

func (c *Client) RetryTask(jid, tid int) error {
	return c.taskOp("tretry", jid, tid, nil)
}
func (c *Client) ResumeTask(jid, tid int) error {
	return c.taskOp("tretry", jid, tid, map[string]string{"recover": "1"})
}
func (c *Client) SkipTask(jid, tid int) error {
	return c.taskOp("tskip", jid, tid, nil)
}
func (c *Client) KillTask(jid, tid int) error {
	// A task kill is an interrupt scoped to the task.
	return c.taskOp("jinterrupt", jid, tid, nil)
}
