package follow

import (
	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

func (state *configSettingsState) openChoice() {
	choices := state.choices(state.cursor)
	if len(choices) < 2 {
		return
	}
	state.choiceOpen = true
	state.choiceCursor = 0
	for index, choice := range choices {
		if choice.selected {
			state.choiceCursor = index
			break
		}
	}
}

func (state *configSettingsState) handleChoiceKey(name string) configKeyAction {
	choices := state.choices(state.cursor)
	if len(choices) == 0 {
		state.choiceOpen = false
		return configKeyAction{}
	}
	switch name {
	case "esc", "escape", "q":
		state.choiceOpen = false
	case "up", "k":
		state.choiceCursor = max(0, state.choiceCursor-1)
	case "down", "j":
		state.choiceCursor = min(len(choices)-1, state.choiceCursor+1)
	case "enter":
		choice := choices[state.choiceCursor]
		switch state.kind {
		case configFix:
			switch state.cursor {
			case 1:
				state.working.Fix.ChangeScope = choice.value
			case 2:
				state.working.Fix.Profile = agent.ProfileID(choice.value)
				probe, ready := readyAgentProbe(state.probes, state.working.Fix.Profile)
				reconcileFixAgentOptions(&state.working.Fix, probe, ready)
			case 3:
				state.working.Fix.Model = agent.ModelID(choice.value)
			case 4:
				state.working.Fix.Effort = agent.EffortID(choice.value)
			}
		case configDelivery:
			switch deliverySettingField(state.working.Delivery, state.cursor) {
			case deliverySettingWorkspace:
				state.working.Delivery.DefaultPlan.Workspace = fix.WorkspaceMode(choice.value)
				if state.working.Delivery.DefaultPlan.Workspace == fix.WorkspaceWorktree && state.working.Delivery.DefaultPlan.Git == fix.GitCommitCurrent {
					state.working.Delivery.DefaultPlan.Git = fix.GitLeaveUncommitted
					state.working.Delivery.DefaultPlan.Publish = fix.PublishLocal
				}
			case deliverySettingGit:
				state.working.Delivery.DefaultPlan.Git = fix.GitMode(choice.value)
				if state.working.Delivery.DefaultPlan.Git == fix.GitLeaveUncommitted {
					state.working.Delivery.DefaultPlan.Publish = fix.PublishLocal
				}
			case deliverySettingPublish:
				state.working.Delivery.DefaultPlan.Publish = fix.PublishMode(choice.value)
			case deliverySettingPRState:
				state.working.Delivery.DraftPullRequests = choice.value == "draft"
			}
		}
		state.choiceOpen = false
		state.dirty = true
		return configKeyAction{kind: configKeySave}
	}
	return configKeyAction{}
}
