package follow

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/fix"
)

func (model *Model) handleFixCommand(message fixCommandMsg) {
	outcome := model.jobActions.handle(message, overlayPresent(model.overlays, OverlayConfirmation), model.fixService)
	if !outcome.matched {
		return
	}
	if outcome.refresh {
		model.agents.setPresentations(outcome.refreshedJobs, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	}
	if outcome.closeConfirmation {
		model.overlays.Pop()
	}
	if outcome.canceling {
		markJobCanceling(model.agents.Jobs, message.jobID)
	}
	if outcome.setNotice {
		model.fixNotice = outcome.notice
	}
}

func (model *Model) executeSelectedJobAction(jobID fix.JobID, action fix.JobAction, confirmation bool) (tea.Model, tea.Cmd) {
	command, notice := model.jobActions.execute(model.fixService, jobID, action, confirmation)
	if notice != "" {
		model.fixNotice = notice
	}
	return model, command
}
