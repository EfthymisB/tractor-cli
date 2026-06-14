package api

import "testing"

func TestDecodeTaskState(t *testing.T) {
	cases := []struct {
		in       string
		state    string
		hasLog   bool
		progress float64
	}{
		{"", "blocked", false, 0},
		{"-A,50", "active", false, 50},
		{"+D", "done", true, 0},
		{"-E", "error", false, 0},
		{"-B", "blocked", false, 0},
		{"+A,100", "active", true, 100},
	}
	for _, c := range cases {
		state, hasLog, progress := decodeTaskState(c.in)
		if state != c.state || hasLog != c.hasLog || progress != c.progress {
			t.Errorf("decodeTaskState(%q)=(%q,%v,%v) want (%q,%v,%v)",
				c.in, state, hasLog, progress, c.state, c.hasLog, c.progress)
		}
	}
}
