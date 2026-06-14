package jobs

import (
	"reflect"
	"testing"

	"github.com/tractor-cli/tractor-cli/internal/models"
)

func TestMetadataDisplay(t *testing.T) {
	if got := metadataDisplay(`{"shot":"010","seq":"abc"}`); got != "seq: abc   shot: 010" {
		t.Errorf("metadataDisplay=%q", got)
	}
	if got := metadataDisplay(""); got != "(none)" {
		t.Errorf("empty: got %q", got)
	}
	if got := metadataDisplay("plain text"); got != "plain text" {
		t.Errorf("non-json: got %q", got)
	}
}

func TestProgressSegments(t *testing.T) {
	j := models.Job{NumTasks: 100, NumDone: 50, NumError: 5, NumActive: 20}
	done, errs, act, rem := progressSegments(j, 20)
	if done != 10 || errs != 1 || act != 4 || rem != 5 {
		t.Errorf("progressSegments=(%d,%d,%d,%d) want (10,1,4,5)", done, errs, act, rem)
	}
	if done+errs+act+rem != 20 {
		t.Errorf("segments don't sum to width")
	}
	// zero tasks -> all remaining
	_, _, _, rem = progressSegments(models.Job{}, 12)
	if rem != 12 {
		t.Errorf("empty job rem=%d want 12", rem)
	}
}

func TestDistinctOwners(t *testing.T) {
	jobs := []models.Job{{Owner: "a"}, {Owner: "a"}, {Owner: "b"}, {Owner: ""}}
	got := distinctOwners(jobs)
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("distinctOwners=%v want [a b]", got)
	}
}
