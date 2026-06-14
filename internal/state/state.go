// Package state persists UI preferences and saved filters between runs.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/tractor-cli/tractor-cli/internal/api"
)

// State is the on-disk preferences document.
type State struct {
	TitleWidth int                      `json:"title_width"`
	SortCol    int                      `json:"sort_col"`
	SortAsc    bool                     `json:"sort_asc"`
	RelTime    bool                     `json:"rel_time"`
	Filters    map[string]api.JobFilter `json:"filters"`
}

func filePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "tractor-cli", "state.json")
}

// Load reads the saved state, returning a usable zero value on any error.
func Load() State {
	s := State{Filters: map[string]api.JobFilter{}}
	p := filePath()
	if p == "" {
		return s
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	if s.Filters == nil {
		s.Filters = map[string]api.JobFilter{}
	}
	return s
}

// Save writes the state to disk (best effort).
func (s State) Save() error {
	p := filePath()
	if p == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// FilterNames returns the saved filter names in sorted order.
func (s State) FilterNames() []string {
	names := make([]string, 0, len(s.Filters))
	for n := range s.Filters {
		names = append(names, n)
	}
	sortStrings(names)
	return names
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
