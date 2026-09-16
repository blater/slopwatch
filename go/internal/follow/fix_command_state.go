package follow

import (
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func (state cancelConfirmation) matches(message fixCommandMsg) bool {
	return message.jobID == state.jobID && message.action == state.action
}

func (state jobCommandState) matches(message fixCommandMsg) bool {
	return state.pending && message.jobID == state.jobID && message.action == state.action
}

func (state *cancelConfirmation) clearPending() { state.pending = false }
func (state *jobCommandState) clearPending()    { state.pending = false }

func (state *cancelConfirmation) setError(message string) { state.errorText = message }

func (state *cancelConfirmation) refreshError(job fix.JobPresentation, action fix.JobAction, errorText string) {
	state.allowed = containsFixAction(job.AllowedActions, action)
	state.errorText = errorText
	if !state.allowed {
		state.errorText += "; this job can no longer be canceled"
	}
}

func markJobCanceling(jobs []fix.JobPresentation, id fix.JobID) {
	for index := range jobs {
		if jobs[index].ID != id {
			continue
		}
		job := &jobs[index]
		job.Phase, job.Attention, job.CurrentAction = fix.PhaseCanceling, fix.AttentionNone, "Canceling"
		job.AllowedActions = nil
		job.Issue = &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}
		return
	}
}

func unavailableActionNotice(job fix.JobPresentation, action fix.JobAction, errorText string) string {
	if containsFixAction(job.AllowedActions, action) {
		return errorText
	}
	return errorText + "; " + strings.ToLower(jobActionLabel(action)) + " is no longer available"
}
