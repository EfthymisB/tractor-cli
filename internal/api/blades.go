package api

import (
	"encoding/json"
	"fmt"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

// bladesResponse is the envelope returned by /Tractor/monitor?q=blades.
type bladesResponse struct {
	RC     int            `json:"rc"`
	Msg    string         `json:"msg"`
	Blades []models.Blade `json:"blades"`
}

// QueryBlades returns all blades visible to the Tractor engine.
// Reference: GET /Tractor/monitor?q=blades
func (c *Client) QueryBlades() ([]models.Blade, error) {
	params := c.authenticatedParams()
	params.Set("q", "blades")

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return nil, fmt.Errorf("QueryBlades: %w", err)
	}

	var resp bladesResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("QueryBlades: parsing response: %w", err)
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("QueryBlades: engine error (rc=%d): %s", resp.RC, resp.Msg)
	}
	return resp.Blades, nil
}
