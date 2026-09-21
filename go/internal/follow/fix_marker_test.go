package follow

import (
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/fix"
)

func TestCompletedFixDoesNotLeaveFileMarker(t *testing.T) {
	job := fix.JobPresentation{
		Phase: fix.PhaseCompleted, UpdatedAt: time.Now().AddDate(-3, 0, 0),
		Targets: []fix.FilePresentation{{Path: "example.go"}},
	}
	if marker := fixMarkerForPath([]fix.JobPresentation{job}, "example.go"); marker != "" {
		t.Fatalf("completed fix left a file marker: %q", marker)
	}
	job.Phase = fix.PhaseRunning
	if marker := fixMarkerForPath([]fix.JobPresentation{job}, "example.go"); marker != "▶" {
		t.Fatalf("running fix marker lost: %q", marker)
	}
}
