package api

import (
	"fmt"
)

// queueCmd sends a command to /Tractor/queue with the given parameters.
func (c *Client) queueCmd(params map[string]string) error {
	p := c.authenticatedParams()
	for k, v := range params {
		p.Set(k, v)
	}
	b, err := c.get("/Tractor/queue", p)
	if err != nil {
		return err
	}
	return parseRC(b)
}

// RetryJob retries all errored tasks in a job.
// Reference: GET /Tractor/queue?q=jretry&jid=NNN&tsubset=error&tsid=SSS
func (c *Client) RetryJob(jid int) error {
	return c.queueCmd(map[string]string{
		"q": "jretry", "jid": fmt.Sprintf("%d", jid), "tsubset": "error",
	})
}

// KillJob interrupts a running job, killing active tasks and pausing dispatching.
// Reference: GET /Tractor/queue?q=jinterrupt&jid=NNN&tsid=SSS
func (c *Client) KillJob(jid int) error {
	return c.queueCmd(map[string]string{
		"q": "jinterrupt", "jid": fmt.Sprintf("%d", jid),
	})
}

// DeleteJob retires (deletes) a job from the active queue.
// Reference: GET /Tractor/queue?q=jretire&jid=NNN&tsid=SSS
func (c *Client) DeleteJob(jid int) error {
	return c.queueCmd(map[string]string{
		"q": "jretire", "jid": fmt.Sprintf("%d", jid),
	})
}

// SkipJob skips all errored tasks in a job.
// Reference: GET /Tractor/queue?q=tskip&jid=NNN&tid=error&tsid=SSS
func (c *Client) SkipJob(jid int) error {
	return c.queueCmd(map[string]string{
		"q": "tskip", "jid": fmt.Sprintf("%d", jid), "tid": "error",
	})
}

// RetryTask retries a specific task.
// Reference: GET /Tractor/queue?q=jretry&jid=NNN&tsubset=TID&tsid=SSS
func (c *Client) RetryTask(jid, tid int) error {
	return c.queueCmd(map[string]string{
		"q": "jretry", "jid": fmt.Sprintf("%d", jid), "tsubset": fmt.Sprintf("%d", tid),
	})
}

// SkipTask skips a specific task.
// Reference: GET /Tractor/queue?q=tskip&jid=NNN&tid=TID&tsid=SSS
func (c *Client) SkipTask(jid, tid int) error {
	return c.queueCmd(map[string]string{
		"q": "tskip", "jid": fmt.Sprintf("%d", jid), "tid": fmt.Sprintf("%d", tid),
	})
}

// KillTask interrupts a specific task.
// Reference: GET /Tractor/queue?q=jinterrupt&jid=NNN&tid=TID&tsid=SSS
func (c *Client) KillTask(jid, tid int) error {
	return c.queueCmd(map[string]string{
		"q": "jinterrupt", "jid": fmt.Sprintf("%d", jid), "tid": fmt.Sprintf("%d", tid),
	})
}
