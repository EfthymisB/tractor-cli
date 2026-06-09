package api

import (
	"encoding/json"
	"fmt"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

const bladeColumns = "name,status,nimby,numslots,availslots,profile,oslabel"

// QueryBlades returns all blades visible to the Tractor engine.
func (c *Client) QueryBlades() ([]models.Blade, error) {
	items, err := c.query("blades", bladeColumns)
	if err != nil {
		return nil, fmt.Errorf("QueryBlades: %w", err)
	}
	blades := make([]models.Blade, 0, len(items))
	for _, raw := range items {
		var b models.Blade
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("parsing blade: %w", err)
		}
		blades = append(blades, b)
	}
	return blades, nil
}
