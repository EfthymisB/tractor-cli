package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

// bladeRow mirrors a blade record from q=blades (note the terse field names).
type bladeRow struct {
	Name       string   `json:"hnm"`
	BladeID    string   `json:"bladeid"`
	Addr       string   `json:"addr"`
	Port       int      `json:"lp"`
	Version    string   `json:"vers"`
	Profile    string   `json:"profile"`
	OSName     string   `json:"osname"`
	OSVersion  string   `json:"osversion"`
	GPULabel   string   `json:"gpulabel"`
	Heartbeat  float64  `json:"t"`
	StartTime  float64  `json:"t0"`
	NumCPU     int      `json:"ncpu"`
	NumSlots   int      `json:"ns"`
	SlotsInUse int      `json:"siu"`
	AvailSlots int      `json:"as"`
	TaskCount  int      `json:"numcmd"`
	LoadAvg    float64  `json:"cpu"`
	MemGB      float64  `json:"mem"`
	DiskGB     float64  `json:"disk"`
	UDI        float64  `json:"udi"`
	Nimby      string   `json:"nimby"`
	Note       string   `json:"note"`
	Owners     []string `json:"owners"`
}

func (r bladeRow) toBlade() models.Blade {
	return models.Blade{
		Name:       r.Name,
		Addr:       r.Addr,
		Port:       r.Port,
		Profile:    r.Profile,
		Version:    r.Version,
		OSName:     r.OSName,
		OSVersion:  r.OSVersion,
		GPULabel:   r.GPULabel,
		Nimby:      r.Nimby,
		Note:       r.Note,
		Heartbeat:  r.Heartbeat,
		StartTime:  r.StartTime,
		NumCPU:     r.NumCPU,
		NumSlots:   r.NumSlots,
		SlotsInUse: r.SlotsInUse,
		AvailSlots: r.AvailSlots,
		TaskCount:  r.TaskCount,
		LoadAvg:    r.LoadAvg,
		MemGB:      r.MemGB,
		DiskGB:     r.DiskGB,
		UDI:        r.UDI,
		Owners:     r.Owners,
		BladeID:    r.BladeID,
	}
}

// QueryBlades returns all blades known to the engine.
func (c *Client) QueryBlades() ([]models.Blade, error) {
	params := c.authenticatedParams()
	params.Set("q", "blades")

	b, err := c.get("/Tractor/monitor", params)
	if err != nil {
		return nil, fmt.Errorf("QueryBlades: %w", err)
	}

	var resp struct {
		RC     int        `json:"rc"`
		Msg    string     `json:"msg"`
		Blades []bladeRow `json:"blades"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("QueryBlades: parsing response: %w", err)
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("QueryBlades: engine error (rc=%d): %s", resp.RC, resp.Msg)
	}

	blades := make([]models.Blade, 0, len(resp.Blades))
	for _, r := range resp.Blades {
		blades = append(blades, r.toBlade())
	}
	return blades, nil
}

// BladeDetails returns the commands currently running on a blade (q=bladedetails).
func (c *Client) BladeDetails(name string) ([]models.BladeCommand, error) {
	p := c.authenticatedParams()
	p.Set("q", "bladeinfo")
	// The engine function takes "bladename"; send both common param names so a
	// minor naming difference doesn't yield an empty result.
	p.Set("blade", name)
	p.Set("bladename", name)

	b, err := c.get("/Tractor/monitor", p)
	if err != nil {
		return nil, fmt.Errorf("BladeDetails: %w", err)
	}

	var resp struct {
		RC   int    `json:"rc"`
		Msg  string `json:"msg"`
		Cmds []struct {
			JID      int      `json:"jid"`
			TID      int      `json:"tid"`
			CID      int      `json:"cid"`
			Owner    string   `json:"juser"`
			Argv     []string `json:"argv"`
			Start    float64  `json:"t"`
			NumSlots int      `json:"numslots"`
		} `json:"cmds"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("BladeDetails: parsing response: %w", err)
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("BladeDetails: engine error (rc=%d): %s", resp.RC, resp.Msg)
	}

	cmds := make([]models.BladeCommand, 0, len(resp.Cmds))
	for _, r := range resp.Cmds {
		cmds = append(cmds, models.BladeCommand{
			JID:       r.JID,
			TID:       r.TID,
			CID:       r.CID,
			Owner:     r.Owner,
			Command:   strings.Join(r.Argv, " "),
			StartTime: r.Start,
			NumSlots:  r.NumSlots,
		})
	}
	return cmds, nil
}

// bladeID builds the "name/ipaddr" identifier the engine expects.
func bladeID(name, addr string) string { return name + "/" + addr }

// NimbyBlade sets (allow=false) or clears (allow=true) nimby on a blade.
func (c *Client) NimbyBlade(name, addr string) error {
	return c.bladeAttr(name, addr, "1")
}

// UnnimbyBlade clears nimby on a blade.
func (c *Client) UnnimbyBlade(name, addr string) error {
	return c.bladeAttr(name, addr, "0")
}

func (c *Client) bladeAttr(name, addr, nimby string) error {
	p := c.authenticatedParams()
	p.Set("q", "battribute")
	p.Set("b", bladeID(name, addr))
	p.Set("nimby", nimby)
	b, err := c.get("/Tractor/ctrl", p)
	if err != nil {
		return fmt.Errorf("nimby %s: %w", name, err)
	}
	return parseRC(b)
}

// EjectBlade retries (ejects) all active tasks on a blade.
func (c *Client) EjectBlade(name, addr string) error {
	p := c.authenticatedParams()
	p.Set("q", "ejectall")
	p.Set("blade", bladeID(name, addr))
	b, err := c.get("/Tractor/queue", p)
	if err != nil {
		return fmt.Errorf("eject %s: %w", name, err)
	}
	return parseRC(b)
}

// DelistBlade removes a blade's entry from the engine database.
func (c *Client) DelistBlade(name, addr string) error {
	p := c.authenticatedParams()
	p.Set("q", "delist")
	p.Set("id", bladeID(name, addr))
	b, err := c.get("/Tractor/btrack", p)
	if err != nil {
		return fmt.Errorf("delist %s: %w", name, err)
	}
	return parseRC(b)
}
