package filter

import (
	"reflect"
	"testing"

	"github.com/tractor-cli/tractor-cli/internal/api"
)

func TestSplitCSV(t *testing.T) {
	got := splitCSV("a, b ,,c ")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitCSV=%v want %v", got, want)
	}
	if got := splitCSV("  "); len(got) != 0 {
		t.Errorf("splitCSV(blank)=%v want empty", got)
	}
}

func TestLastSegment(t *testing.T) {
	if got := lastSegment("alice,bo"); got != "bo" {
		t.Errorf("lastSegment=%q want %q", got, "bo")
	}
	if got := lastSegment("alice"); got != "alice" {
		t.Errorf("lastSegment=%q want %q", got, "alice")
	}
}

func TestParseJIDFilter(t *testing.T) {
	var f api.JobFilter
	parseJIDFilter("1,2-4,7", &f)
	if !reflect.DeepEqual(f.JIDs, []int{1, 7}) {
		t.Errorf("JIDs=%v want [1 7]", f.JIDs)
	}
	if f.JIDFrom != 2 || f.JIDTo != 4 {
		t.Errorf("range=%d-%d want 2-4", f.JIDFrom, f.JIDTo)
	}
}

func TestJIDFilterToString(t *testing.T) {
	f := api.JobFilter{JIDs: []int{1, 2}, JIDFrom: 10, JIDTo: 20}
	if got := jidFilterToString(f); got != "1,2,10-20" {
		t.Errorf("jidFilterToString=%q want %q", got, "1,2,10-20")
	}
}
