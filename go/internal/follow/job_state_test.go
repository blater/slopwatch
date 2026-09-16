package follow

import (
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestJobMonitorApplyClampsOffsetAfterReplacingJob(t *testing.T) {
	state := jobMonitorState{
		generation: 4,
		jobID:      "job",
		job:        fix.JobPresentation{ID: "job", Goal: "long", Actors: make([]fix.ActorPresentation, 30)},
		offset:     19,
	}
	pending := state.apply(jobMonitorMsg{
		generation: 4,
		found:      true,
		job:        fix.JobPresentation{ID: "job", Goal: "short"},
	}, 80, 24, false)
	if pending || state.offset != 0 {
		t.Fatalf("replacement offset=%d pending=%t, want clamped to zero", state.offset, pending)
	}
}

func TestJobMonitorLineCountIncludesDeliveryRows(t *testing.T) {
	job := fix.JobPresentation{
		ID: "job",
		DeliveryPlan: fix.DeliveryPlan{
			Workspace: fix.WorkspaceCurrent,
			Git:       fix.GitLeaveUncommitted,
			Publish:   fix.PublishLocal,
		},
	}
	state := jobMonitorState{job: job}
	if got := state.lineCount(job); got != 8 {
		t.Fatalf("delivery line count=%d, want 8", got)
	}
}
