package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// loginResponse is the JSON body returned by /Tractor/monitor?q=login.
type loginResponse struct {
	RC   int    `json:"rc"`
	Msg  string `json:"msg"`
	TSID string `json:"tsid"`
}

// Login authenticates with the Tractor engine and stores the session token (tsid).
// The password parameter should be the site-specific password hash/value passed as
// the "c" parameter. All subsequent authenticated requests will include this tsid.
//
// Reference: GET /Tractor/monitor?q=login&user=UUUU&c=CCCCC
func (c *Client) Login(username, password string) error {
	params := url.Values{
		"q":    {"login"},
		"user": {username},
	}
	if password != "" {
		params.Set("c", password)
	}
	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	var resp loginResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return fmt.Errorf("parsing login response: %w", err)
	}
	if resp.RC != 0 {
		return fmt.Errorf("login failed (rc=%d): %s", resp.RC, resp.Msg)
	}
	if resp.TSID == "" {
		return fmt.Errorf("login succeeded but no tsid returned; check credentials")
	}
	c.tsid = resp.TSID
	return nil
}
