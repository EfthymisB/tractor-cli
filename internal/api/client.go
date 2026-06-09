// Package api provides a client for the Tractor render farm REST API.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

// Client is a Tractor API client. It holds the base URL and session cookie jar.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// queryResponse is the envelope returned by all /tractor/query calls.
type queryResponse struct {
	RC   int               `json:"rc"`
	Msg  string            `json:"msg"`
	Data []json.RawMessage `json:"data"`
}

// NewClient creates a new API client for the given base URL (e.g. "http://host:80").
func NewClient(baseURL string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", err)
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
	}, nil
}

// get performs a GET request and returns the raw response body.
func (c *Client) get(path string, params url.Values) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}
	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u.String(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

// postForm performs a POST request with a form-encoded body and returns the raw response.
func (c *Client) postForm(path string, body url.Values) ([]byte, error) {
	resp, err := c.httpClient.PostForm(c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	return b, nil
}

// postJSON performs a POST request with a JSON body and returns the raw response.
func (c *Client) postJSON(path string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshalling payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	return b, nil
}

// query executes a TQL query and returns raw JSON data items.
func (c *Client) query(tql string, columns string) ([]json.RawMessage, error) {
	params := url.Values{"q": {tql}}
	if columns != "" {
		params.Set("columns", columns)
	}
	body, err := c.get("/tractor/query", params)
	if err != nil {
		return nil, err
	}
	var qr queryResponse
	if err := json.Unmarshal(body, &qr); err != nil {
		return nil, fmt.Errorf("parsing query response: %w", err)
	}
	if qr.RC != 0 {
		return nil, fmt.Errorf("TQL error (rc=%d): %s", qr.RC, qr.Msg)
	}
	return qr.Data, nil
}
