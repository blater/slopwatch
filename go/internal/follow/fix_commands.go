package follow

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
)

func (model *Model) handleFixCommand(message fixCommandMsg) {
	confirmation := overlayPresent(model.overlays, OverlayConfirmation) && model.cancelConfirmation.matches(message)
	direct := !confirmation && model.jobCommand.matches(message)
	if !confirmation && !direct {
		return
	}
	if confirmation {
		model.cancelConfirmation.clearPending()
	} else {
		model.jobCommand.clearPending()
	}
	if message.err != nil {
		model.refreshActionAfterError(message, confirmation)
		return
	}
	if !message.receipt.Accepted {
		model.setCommandError(message.receipt.Message, confirmation)
		return
	}
	if confirmation {
		model.overlays.Pop()
	}
	if message.action == fix.ActionCancel {
		markJobCanceling(model.agents.Jobs, message.jobID)
	}
	model.fixNotice = jobActionPastTense(message.action) + " requested for " + string(message.jobID)
}

func (model *Model) setCommandError(message string, confirmation bool) {
	if confirmation {
		model.cancelConfirmation.setError(message)
		return
	}
	model.fixNotice = message
}

func (model *Model) refreshActionAfterError(message fixCommandMsg, confirmation bool) {
	errorText := message.err.Error()
	snapshot := model.fixService.Jobs(fixapp.JobFilter{IncludeFinished: true})
	model.agents.setPresentations(snapshot.Jobs, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	job, ok := findJob(snapshot.Jobs, message.jobID)
	if !ok {
		model.setCommandError(errorText+"; job is no longer available", confirmation)
		return
	}
	model.refreshExistingActionError(job, message.action, errorText, confirmation)
}

func findJob(jobs []fix.JobPresentation, id fix.JobID) (fix.JobPresentation, bool) {
	for _, job := range jobs {
		if job.ID == id {
			return job, true
		}
	}
	return fix.JobPresentation{}, false
}

func (model *Model) refreshExistingActionError(job fix.JobPresentation, action fix.JobAction, errorText string, confirmation bool) {
	if confirmation {
		model.cancelConfirmation.refreshError(job, action, errorText)
		return
	}
	model.fixNotice = unavailableActionNotice(job, action, errorText)
}

func (model *Model) executeSelectedJobAction(jobID fix.JobID, action fix.JobAction, confirmation bool) (tea.Model, tea.Cmd) {
	requestID, err := fix.NewCommandID()
	if err != nil {
		if confirmation {
			model.cancelConfirmation.errorText = err.Error()
		} else {
			model.fixNotice = "Could not create job command: " + err.Error()
		}
		return model, nil
	}
	if confirmation {
		model.cancelConfirmation.pending = true
	} else {
		model.jobCommand.pending = true
	}
	service := model.fixService
	command := fix.JobCommand{RequestID: requestID, JobID: jobID, Action: action}
	return model, func() tea.Msg {
		receipt, executeErr := service.Execute(context.Background(), command)
		return fixCommandMsg{jobID: command.JobID, action: action, receipt: receipt, err: executeErr}
	}
}
