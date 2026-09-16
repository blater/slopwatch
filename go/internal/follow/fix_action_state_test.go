package follow

import (
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
)

func TestRejectedDirectActionWithEmptyMessageClearsOldNotice(t *testing.T) {
	model := Model{fixNotice: "previous action failed"}
	model.jobActions.command = jobCommandState{jobID: "job-1", action: fix.ActionCancel, pending: true}
	model.handleFixCommand(fixCommandMsg{
		jobID: "job-1", action: fix.ActionCancel,
		receipt: fixapp.CommandReceipt{Accepted: false},
	})
	if model.fixNotice != "" {
		t.Fatalf("rejected empty response retained stale notice %q", model.fixNotice)
	}
}
