package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// tractorSelect runs a single-table tractorselect() over the /Tractor/db proxy
// (the supported query path used by tractor.api.query) and returns the rows
// array as raw JSON for the caller to unmarshal into a typed slice.
func (c *Client) tractorSelect(table, where, columns, orderby string) (json.RawMessage, error) {
	// The where clause is embedded inside a single-quoted SQL literal, so its
	// own single quotes must be doubled.
	q := fmt.Sprintf("tractorselect('%s', '%s', '%s', '%s', %d, 'f', 'None')",
		table, strings.ReplaceAll(where, "'", "''"), columns, orderby, 100000)

	p := c.authenticatedParams()
	p.Set("q", q)
	b, err := c.get("/Tractor/db", p)
	if err != nil {
		return nil, err
	}
	var resp struct {
		RC   int             `json:"rc"`
		Msg  string          `json:"msg"`
		Rows json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	if resp.RC != 0 {
		return nil, fmt.Errorf("rc=%d: %s", resp.RC, resp.Msg)
	}
	return resp.Rows, nil
}

// BladeDetails returns the commands currently running on a blade. There is no
// monitor query for this, so we assemble it from tractorselect over the
// invocation (active invocations on the blade), job (owner) and command (argv)
// tables, joined client-side.
func (c *Client) BladeDetails(bladeID string) ([]models.BladeCommand, error) {
	where := fmt.Sprintf("bladeid='%s' and not stoptime", bladeID)
	invRaw, err := c.tractorSelect("invocation", where, "jid,tid,cid,numslots", "jid,tid")
	if err != nil {
		return nil, fmt.Errorf("BladeDetails invocations: %w", err)
	}
	var invs []struct {
		JID      int `json:"jid"`
		TID      int `json:"tid"`
		CID      int `json:"cid"`
		NumSlots int `json:"numslots"`
	}
	if err := json.Unmarshal(invRaw, &invs); err != nil {
		return nil, fmt.Errorf("BladeDetails: parsing invocations: %w", err)
	}
	if len(invs) == 0 {
		return nil, nil
	}

	// Unique jids for the follow-up owner/command lookups.
	seen := make(map[int]struct{})
	var jids []string
	for _, iv := range invs {
		if _, ok := seen[iv.JID]; !ok {
			seen[iv.JID] = struct{}{}
			jids = append(jids, strconv.Itoa(iv.JID))
		}
	}
	jidIn := "jid in [" + strings.Join(jids, ",") + "]"

	// Owner per job.
	ownerByJID := map[int]string{}
	if raw, err := c.tractorSelect("job", jidIn, "jid,owner", "jid"); err == nil {
		var jobs []struct {
			JID   int    `json:"jid"`
			Owner string `json:"owner"`
		}
		if json.Unmarshal(raw, &jobs) == nil {
			for _, j := range jobs {
				ownerByJID[j.JID] = j.Owner
			}
		}
	}

	// argv per (jid,tid,cid).
	type key struct{ jid, tid, cid int }
	argvByKey := map[key]string{}
	if raw, err := c.tractorSelect("command", jidIn, "jid,tid,cid,argv", "jid,tid,cid"); err == nil {
		var cmds []struct {
			JID  int      `json:"jid"`
			TID  int      `json:"tid"`
			CID  int      `json:"cid"`
			Argv []string `json:"argv"`
		}
		if json.Unmarshal(raw, &cmds) == nil {
			for _, cm := range cmds {
				argvByKey[key{cm.JID, cm.TID, cm.CID}] = strings.Join(cm.Argv, " ")
			}
		}
	}

	out := make([]models.BladeCommand, 0, len(invs))
	for _, iv := range invs {
		out = append(out, models.BladeCommand{
			JID:      iv.JID,
			TID:      iv.TID,
			CID:      iv.CID,
			Owner:    ownerByJID[iv.JID],
			Command:  argvByKey[key{iv.JID, iv.TID, iv.CID}],
			NumSlots: iv.NumSlots,
		})
	}
	return out, nil
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

// TraceBlade returns the engine's plain-text trace/diagnostics for a blade.
// It uses a short timeout because the engine relays to the blade, which can be
// slow or unresponsive — we'd rather fail fast than appear stuck.
func (c *Client) TraceBlade(name, addr string) (string, error) {
	p := c.authenticatedParams()
	p.Set("q", "tracer")
	p.Set("t", bladeID(name, addr))
	p.Set("fmt", "plain")

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/Tractor/ctrl?"+p.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("TraceBlade: %w", err)
	}
	b, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("TraceBlade: %w", err)
	}
	return string(b), nil
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
