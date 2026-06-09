package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

type loginSchemeResponse struct {
	RC         int    `json:"rc"`
	Msg        string `json:"msg"`
	Validation string `json:"validation"`
}

type genTokenResponse struct {
	RC        int    `json:"rc"`
	Msg       string `json:"msg"`
	Challenge string `json:"challenge"`
}

type loginResponse struct {
	RC   int    `json:"rc"`
	Msg  string `json:"msg"`
	TSID string `json:"tsid"`
}

// Login authenticates with the Tractor engine using a challenge-response scheme:
//  1. Check /Tractor/monitor?q=loginscheme to see if passwords are required.
//  2. If yes, fetch a one-time challenge from /Tractor/monitor?q=gentoken.
//  3. XOR-hash the password against the challenge (trInternalPasswordHash).
//  4. Hex-encode (challenge + "|" + hash) and send as the "c" parameter.
//
// Reference: TrHttpRPC.py Login() / trInternalPasswordHash()
func (c *Client) Login(username, password string) error {
	c.username = username

	needsPassword, err := c.checkLoginScheme()
	if err != nil {
		return fmt.Errorf("checking login scheme: %w", err)
	}

	params := url.Values{
		"q":    {"login"},
		"user": {username},
	}

	if needsPassword {
		if password == "" {
			return fmt.Errorf("engine requires a password; set TRACTOR_PASSWORD")
		}
		challenge, err := c.fetchChallenge()
		if err != nil {
			return fmt.Errorf("fetching challenge token: %w", err)
		}
		hash := internalPasswordHash(password, challenge)
		challengePass := challenge + "|" + hash
		params.Set("c", strToHex(challengePass))
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
		return fmt.Errorf("login succeeded but no tsid in response")
	}
	c.tsid = resp.TSID
	return nil
}

// checkLoginScheme returns true if the engine requires a password.
func (c *Client) checkLoginScheme() (bool, error) {
	b, err := c.get("/Tractor/monitor", url.Values{"q": {"loginscheme"}})
	if err != nil {
		// If the endpoint doesn't exist, assume no password needed.
		return false, nil
	}
	var resp loginSchemeResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return false, nil
	}
	// "internal:..." means PAM-style password hashing is in use.
	return len(resp.Validation) > 0, nil
}

// fetchChallenge retrieves a one-time challenge token from the engine.
func (c *Client) fetchChallenge() (string, error) {
	b, err := c.get("/Tractor/monitor", url.Values{"q": {"gentoken"}})
	if err != nil {
		return "", err
	}
	var resp genTokenResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return "", fmt.Errorf("parsing gentoken response: %w", err)
	}
	if resp.RC != 0 {
		return "", fmt.Errorf("gentoken error (rc=%d): %s", resp.RC, resp.Msg)
	}
	if resp.Challenge == "" {
		return "", fmt.Errorf("gentoken returned empty challenge")
	}
	return resp.Challenge, nil
}

// internalPasswordHash implements Tractor's trInternalPasswordHash:
// XOR each byte of passwd with the corresponding challenge byte,
// prefix with "1" (variant marker), output as lowercase hex pairs.
func internalPasswordHash(passwd, challenge string) string {
	n := len(passwd)
	if len(challenge) < n {
		n = len(challenge)
	}
	h := "1"
	for i := 0; i < n; i++ {
		k := int(passwd[i]) ^ int(challenge[i])
		if k > 255 {
			k = 255
		}
		h += fmt.Sprintf("%02x", k)
	}
	return h
}

// strToHex encodes each byte of s as a 2-digit lowercase hex string.
// Equivalent to TrHttpRPC.trStrToHex().
func strToHex(s string) string {
	out := make([]byte, len(s)*2)
	const hx = "0123456789abcdef"
	for i := 0; i < len(s); i++ {
		out[i*2] = hx[s[i]>>4]
		out[i*2+1] = hx[s[i]&0xf]
	}
	return string(out)
}
