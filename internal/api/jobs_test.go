package api

import (
	"testing"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		paused, active, done, total, errs int
		want                              string
	}{
		{1, 0, 0, 10, 0, "paused"},
		{0, 0, 0, 10, 2, "error"},
		{0, 3, 0, 10, 0, "active"},
		{0, 0, 10, 10, 0, "done"},
		{0, 0, 3, 10, 0, "blocked"},
		{1, 5, 0, 10, 5, "paused"}, // paused wins
	}
	for _, c := range cases {
		if got := deriveStatus(c.paused, c.active, c.done, c.total, c.errs); got != c.want {
			t.Errorf("deriveStatus(%v)=%q want %q", c, got, c.want)
		}
	}
}

func TestBuildSelectWhere(t *testing.T) {
	cases := []struct {
		name string
		f    JobFilter
		want string
	}{
		{"single owner", JobFilter{Owners: []string{"alice"}}, "owner=alice"},
		{"multi owner", JobFilter{Owners: []string{"a", "b"}}, "owner in [a, b]"},
		{"single jid", JobFilter{JIDs: []int{5}}, "jid=5"},
		{"jid list", JobFilter{JIDs: []int{1, 2}}, "jid in [1, 2]"},
		{"jid range", JobFilter{JIDFrom: 10, JIDTo: 20}, "jid>=10 and jid<=20"},
		{"status", JobFilter{Statuses: []string{"error"}}, "error"},
		{"paused", JobFilter{Statuses: []string{"paused"}}, "pausetime is not null"},
		{"priority", JobFilter{Priority: 100}, "priority=100"},
		{"title", JobFilter{Title: "comp"}, "title like comp"},
		{"combo", JobFilter{Owners: []string{"a"}, Statuses: []string{"error"}}, "owner=a and error"},
		{"empty", JobFilter{}, ""},
	}
	for _, c := range cases {
		if got := buildSelectWhere(c.f); got != c.want {
			t.Errorf("%s: buildSelectWhere=%q want %q", c.name, got, c.want)
		}
	}
}

func TestSpoolTimeString(t *testing.T) {
	if got := spoolTimeString("2024-01-02 15:04:05.123+00"); got != "2024-01-02 15:04:05" {
		t.Errorf("string trim: got %q", got)
	}
	if got := spoolTimeString(float64(0)); got != "" {
		t.Errorf("zero epoch: got %q", got)
	}
	if got := spoolTimeString(nil); got != "" {
		t.Errorf("nil: got %q", got)
	}
}

func TestApplyClientFilters(t *testing.T) {
	jobs := []models.Job{
		{JID: 1, Owner: "alice", Status: "error"},
		{JID: 2, Owner: "bob", Status: "active"},
		{JID: 3, Owner: "alice", Status: "done"},
	}
	got := applyClientFilters(jobs, JobFilter{Owners: []string{"alice"}})
	if len(got) != 2 {
		t.Fatalf("owner filter: got %d jobs, want 2", len(got))
	}
	got = applyClientFilters(jobs, JobFilter{Statuses: []string{"error", "active"}})
	if len(got) != 2 {
		t.Fatalf("status filter: got %d jobs, want 2", len(got))
	}
	got = applyClientFilters(jobs, JobFilter{JIDs: []int{2}})
	if len(got) != 1 || got[0].JID != 2 {
		t.Fatalf("jid filter: got %v", got)
	}
}
