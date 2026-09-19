package follow

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/fix"
)

func (model *Model) handleFixCommand(message fixCommandMsg) {
	outcome := model.runtime.jobActions.handle(message, overlayPresent(model.overlays, OverlayConfirmation), model.fixService)
	if !outcome.matched {
		return
	}
	if outcome.refresh {
		model.agents.setPresentations(outcome.refreshedJobs, makeAgentLayout(model.width, model.height, bodyHeight(model.mainView, model.height)))
	}
	if outcome.closeConfirmation {
		model.overlays.Pop()
	}
	if outcome.canceling {
		markJobCanceling(model.agents.Jobs, message.jobID)
	}
	if outcome.setNotice {
		if message.err != nil || !message.receipt.Accepted {
			model.fixNotice = ""
			notice := outcome.notice
			if notice == "" {
				notice = jobActionLabel(message.action) + " was rejected"
			}
			showRuntimeError(model, errors.New(notice))
		} else {
			model.fixNotice = outcome.notice
		}
	}
}

func (model *Model) executeSelectedJobAction(jobID fix.JobID, action fix.JobAction, confirmation bool) (tea.Model, tea.Cmd) {
	command, notice := model.runtime.jobActions.execute(model.fixService, jobID, action, confirmation)
	if notice != "" {
		showRuntimeError(model, errors.New(notice))
	}
	return model, command
}
