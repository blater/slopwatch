package follow

import (
	"context"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	tea "github.com/charmbracelet/bubbletea"
)

type jobCommandState struct {
	jobID   fix.JobID
	action  fix.JobAction
	pending bool
}

type cancelConfirmation struct {
	jobID     fix.JobID
	action    fix.JobAction
	allowed   bool
	pending   bool
	errorText string
}

// jobActionState owns the two command lifecycles used by follow mode: a
// direct action and the confirmation required for cancellation.
type jobActionState struct {
	command      jobCommandState
	confirmation cancelConfirmation
}

type jobActionOutcome struct {
	matched           bool
	closeConfirmation bool
	canceling         bool
	setNotice         bool
	notice            string
	refresh           bool
	refreshedJobs     []fix.JobPresentation
}

func (state *jobActionState) beginConfirmation(jobID fix.JobID, action fix.JobAction) {
	state.confirmation = cancelConfirmation{jobID: jobID, action: action, allowed: true}
}

func (state *jobActionState) beginDirect(jobID fix.JobID, action fix.JobAction) {
	state.command = jobCommandState{jobID: jobID, action: action, pending: true}
}

func (state *jobActionState) execute(service FixService, jobID fix.JobID, action fix.JobAction, confirmation bool) (tea.Cmd, string) {
	if !confirmation {
		state.beginDirect(jobID, action)
	}
	requestID, err := fix.NewCommandID()
	if err != nil {
		if confirmation {
			state.confirmation.errorText = err.Error()
			return nil, ""
		}
		return nil, "Could not create job command: " + err.Error()
	}
	if confirmation {
		state.confirmation.pending = true
	}
	command := fix.JobCommand{RequestID: requestID, JobID: jobID, Action: action}
	return func() tea.Msg {
		receipt, executeErr := service.Execute(context.Background(), command)
		return fixCommandMsg{jobID: command.JobID, action: action, receipt: receipt, err: executeErr}
	}, ""
}

// handle consumes only a response matching the active confirmation first,
// then the active direct command. Refreshes are marked separately so an empty
// authoritative snapshot is still applied by the Model.
func (state *jobActionState) handle(message fixCommandMsg, confirmationOpen bool, service FixService) jobActionOutcome {
	confirmation := confirmationOpen && state.confirmation.matches(message)
	direct := !confirmation && state.command.matches(message)
	if !confirmation && !direct {
		return jobActionOutcome{}
	}
	if confirmation {
		state.confirmation.clearPending()
	} else {
		state.command.clearPending()
	}
	outcome := jobActionOutcome{matched: true}
	if message.err != nil {
		return state.actionError(service, message, confirmation, outcome)
	}
	if !message.receipt.Accepted {
		if confirmation {
			state.confirmation.setError(message.receipt.Message)
		} else {
			outcome.setNotice = true
			outcome.notice = message.receipt.Message
		}
		return outcome
	}
	outcome.closeConfirmation = confirmation
	outcome.canceling = message.action == fix.ActionCancel
	outcome.setNotice = true
	outcome.notice = jobActionPastTense(message.action) + " requested for " + string(message.jobID)
	return outcome
}

func (state *jobActionState) actionError(service FixService, message fixCommandMsg, confirmation bool, outcome jobActionOutcome) jobActionOutcome {
	snapshot := service.Jobs(fixapp.JobFilter{IncludeFinished: true})
	outcome.refresh = true
	outcome.refreshedJobs = snapshot.Jobs
	job, ok := jobByID(snapshot.Jobs, message.jobID)
	if !ok {
		if confirmation {
			state.confirmation.setError(message.err.Error() + "; job is no longer available")
		} else {
			outcome.setNotice = true
			outcome.notice = message.err.Error() + "; job is no longer available"
		}
		return outcome
	}
	if confirmation {
		state.confirmation.refreshError(job, message.action, message.err.Error())
	} else {
		outcome.setNotice = true
		outcome.notice = unavailableActionNotice(job, message.action, message.err.Error())
	}
	return outcome
}
