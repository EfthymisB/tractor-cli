// Package api provides a client for the Tractor render farm REST API.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a Tractor API client. It holds the base URL, session token (tsid),
// and username (sent as TractorUser cookie on every request).
type Client struct {
	baseURL    string
	username   string
	tsid       string
	httpClient *http.Client
}

// NewClient creates a new API client for the given base URL (e.g. "http://host:80").
// Call Login before making authenticated requests.
func NewClient(baseURL string) (*Client, error) {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// TSID returns the current session token, or empty string if not logged in.
func (c *Client) TSID() string {
	return c.tsid
}

// do performs an HTTP request, injecting the TractorUser cookie and returning
// the raw response body.
func (c *Client) do(req *http.Request) ([]byte, error) {
	if c.username != "" {
		req.AddCookie(&http.Cookie{Name: "TractorUser", Value: c.username})
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL, err)
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

// get performs a GET request and returns the raw response body.
func (c *Client) get(path string, params url.Values) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
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
	return c.do(req)
}

// getLogURI fetches a logging-service URI returned by q=tasklogs. Absolute
// URIs are used as-is; relative ones are resolved against the engine base URL.
// The TractorUser cookie is sent in case the logging service requires it.
func (c *Client) getLogURI(uri string) ([]byte, error) {
	full := uri
	if !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://") {
		full = c.baseURL + uri
	}
	req, err := http.NewRequest(http.MethodGet, full, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// authenticatedParams returns a url.Values pre-populated with the session tsid.
func (c *Client) authenticatedParams() url.Values {
	p := url.Values{}
	if c.tsid != "" {
		p.Set("tsid", c.tsid)
	}
	return p
}
