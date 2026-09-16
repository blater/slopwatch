package follow

import (
	"github.com/blater/slopwatch/internal/fix"
)

func (state configSettingsState) choices(field int) []fixDialogChoice {
	switch state.kind {
	case configFix:
		switch field {
		case 1:
			values := []string{"targets-only", "targets-and-tests", "repository"}
			choices := make([]fixDialogChoice, 0, len(values))
			for _, value := range values {
				choices = append(choices, fixDialogChoice{value: value, label: changeScopeLabel(value), selected: value == state.working.Fix.ChangeScope})
			}
			return choices
		case 2:
			choices := make([]fixDialogChoice, 0, len(state.working.Profiles))
			for _, profile := range state.working.Profiles {
				choices = append(choices, fixDialogChoice{value: string(profile.ID), label: agentProfileChoiceLabel(profile), selected: profile.ID == state.working.Fix.Profile})
			}
			return choices
		case 3:
			probe, _ := readyAgentProbe(state.probes, state.working.Fix.Profile)
			choices := []fixDialogChoice{{label: "Runtime default", selected: state.working.Fix.Model == ""}}
			for _, option := range probe.Capabilities.Models {
				choices = append(choices, fixDialogChoice{value: string(option.ID), label: nonemptySetting(option.Label, string(option.ID)), selected: option.ID == state.working.Fix.Model})
			}
			return choices
		case 4:
			probe, _ := readyAgentProbe(state.probes, state.working.Fix.Profile)
			choices := []fixDialogChoice{{label: "Runtime default", selected: state.working.Fix.Effort == ""}}
			for _, option := range probe.Capabilities.Efforts {
				choices = append(choices, fixDialogChoice{value: string(option.ID), label: nonemptySetting(option.Label, string(option.ID)), selected: option.ID == state.working.Fix.Effort})
			}
			return choices
		}
	case configDelivery:
		field = deliverySettingField(state.working.Delivery, field)
		switch field {
		case deliverySettingWorkspace:
			return []fixDialogChoice{
				{value: string(fix.WorkspaceCurrent), label: workspaceModeLabel(fix.WorkspaceCurrent), selected: state.working.Delivery.DefaultPlan.Workspace == fix.WorkspaceCurrent},
				{value: string(fix.WorkspaceWorktree), label: workspaceModeLabel(fix.WorkspaceWorktree), selected: state.working.Delivery.DefaultPlan.Workspace == fix.WorkspaceWorktree},
			}
		case deliverySettingGit:
			return []fixDialogChoice{
				{value: string(fix.GitLeaveUncommitted), label: gitModeLabel(fix.GitLeaveUncommitted), selected: state.working.Delivery.DefaultPlan.Git == fix.GitLeaveUncommitted},
				{value: string(fix.GitCommitCurrent), label: gitModeLabel(fix.GitCommitCurrent), selected: state.working.Delivery.DefaultPlan.Git == fix.GitCommitCurrent, disabled: state.working.Delivery.DefaultPlan.Workspace == fix.WorkspaceWorktree},
				{value: string(fix.GitCommitNewBranch), label: gitModeLabel(fix.GitCommitNewBranch), selected: state.working.Delivery.DefaultPlan.Git == fix.GitCommitNewBranch},
			}
		case deliverySettingPublish:
			return []fixDialogChoice{
				{value: string(fix.PublishLocal), label: publishModeLabel(fix.PublishLocal), selected: state.working.Delivery.DefaultPlan.Publish == fix.PublishLocal},
				{value: string(fix.PublishPush), label: publishModeLabel(fix.PublishPush), selected: state.working.Delivery.DefaultPlan.Publish == fix.PublishPush},
				{value: string(fix.PublishPullRequest), label: publishModeLabel(fix.PublishPullRequest), selected: state.working.Delivery.DefaultPlan.Publish == fix.PublishPullRequest},
			}
		case deliverySettingPRState:
			return []fixDialogChoice{
				{value: "draft", label: "Draft", selected: state.working.Delivery.DraftPullRequests},
				{value: "ready", label: "Ready for review", selected: !state.working.Delivery.DraftPullRequests},
			}
		}
	}
	return nil
}

func configChoiceFieldForState(state configSettingsState, field int) bool {
	return len(state.choices(field)) > 1
}
