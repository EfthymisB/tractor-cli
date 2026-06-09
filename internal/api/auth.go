package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// loginResponse is the JSON body returned by /tractor/login.
type loginResponse struct {
	RC  int    `json:"rc"`
	Msg string `json:"msg"`
}

// Login authenticates with the Tractor engine and stores the session cookie in
// the client's cookie jar. All subsequent API calls will send the cookie automatically.
//
// Tractor 2.x accepts credentials as GET query parameters on /tractor/login.
func (c *Client) Login(username, password string) error {
	params := url.Values{
		"user":     {username},
		"password": {password},
	}
	b, err := c.get("/tractor/login", params)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	var resp loginResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		// Some Tractor versions return plain text on success; treat non-JSON as OK.
		return nil
	}
	if resp.RC != 0 {
		return fmt.Errorf("login failed (rc=%d): %s", resp.RC, resp.Msg)
	}
	return nil
}
