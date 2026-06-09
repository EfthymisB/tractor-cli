package api

import (
	"encoding/json"
	"fmt"
)

// cmdRequest is the JSON body for /tractor/cmd.
type cmdRequest struct {
	JID    int    `json:"jid"`
	TID    int    `json:"tid,omitempty"`
	Action string `json:"action"`
}

// cmdResponse is the response envelope from /tractor/cmd.
type cmdResponse struct {
	RC  int    `json:"rc"`
	Msg string `json:"msg"`
}

func (c *Client) sendCommand(req cmdRequest) error {
	b, err := c.postJSON("/tractor/cmd", req)
	if err != nil {
		return err
	}
	var resp cmdResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		// Non-JSON response — treat as success.
		return nil
	}
	if resp.RC != 0 {
		return fmt.Errorf("command %s failed (rc=%d): %s", req.Action, resp.RC, resp.Msg)
	}
	return nil
}

// RetryJob retries all errored tasks in a job.
func (c *Client) RetryJob(jid int) error {
	return c.sendCommand(cmdRequest{JID: jid, Action: "jretry"})
}

// KillJob kills (pauses) a running job.
func (c *Client) KillJob(jid int) error {
	return c.sendCommand(cmdRequest{JID: jid, Action: "jkill"})
}

// DeleteJob deletes a job.
func (c *Client) DeleteJob(jid int) error {
	return c.sendCommand(cmdRequest{JID: jid, Action: "jdelete"})
}

// SkipJob skips all errored tasks in a job.
func (c *Client) SkipJob(jid int) error {
	return c.sendCommand(cmdRequest{JID: jid, Action: "jskip"})
}

// RetryTask retries a specific task.
func (c *Client) RetryTask(jid, tid int) error {
	return c.sendCommand(cmdRequest{JID: jid, TID: tid, Action: "tretry"})
}

// SkipTask skips a specific task.
func (c *Client) SkipTask(jid, tid int) error {
	return c.sendCommand(cmdRequest{JID: jid, TID: tid, Action: "tskip"})
}

// KillTask kills a running task.
func (c *Client) KillTask(jid, tid int) error {
	return c.sendCommand(cmdRequest{JID: jid, TID: tid, Action: "tkill"})
}
