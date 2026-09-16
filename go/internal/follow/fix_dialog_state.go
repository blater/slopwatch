package follow

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	tea "github.com/charmbracelet/bubbletea"
)

func (state fixDialogState) targetPaths() []fix.RepoPath {
	if len(state.targets) > 0 {
		return append([]fix.RepoPath(nil), state.targets...)
	}
	if state.target != "" {
		return []fix.RepoPath{state.target}
	}
	return nil
}

func availableFixMetrics(input fixapp.FixInput) []fix.MetricID {
	targets := input.Baseline.Contract.Targets
	if len(targets) == 0 {
		return []fix.MetricID{fix.MetricScore}
	}
	seen := map[fix.MetricID]int{}
	for _, target := range targets {
		for id, metric := range target.Metrics {
			if metric.Complete {
				seen[id]++
			}
		}
	}
	result := make([]fix.MetricID, 0, len(seen)+1)
	for id, count := range seen {
		if count == len(targets) {
			result = append(result, id)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return append([]fix.MetricID{fix.MetricScore}, result...)
}

func (state fixDialogState) fixFormValues() fixapp.FormValues {
	goals := make([]fix.MetricGoal, 0, len(state.focus))
	for _, id := range state.metrics {
		if !state.focus[id] {
			continue
		}
		if goal, ok := state.metricGoal(id); ok {
			goals = append(goals, goal)
		}
	}
	return fixapp.FormValues{
		TargetScore: state.input.TargetScore,
		Focus:       goals, ChangeScope: state.input.ChangeScope,
		DeliveryPlan: state.input.DeliveryPlan, BranchName: state.branch.Value(),
	}
}

func (state fixDialogState) metricGoal(id fix.MetricID) (fix.MetricGoal, bool) {
	if id == fix.MetricScore {
		return fix.MetricGoal{Metric: id, Maximum: state.input.TargetScore}, true
	}
	maximum, found := 0.0, false
	for _, target := range state.input.Baseline.Contract.Targets {
		if metric, ok := target.Metrics[id]; ok && metric.Complete && (!found || metric.Value > maximum) {
			maximum, found = metric.Value, true
		}
	}
	return fix.MetricGoal{Metric: id, Maximum: maximum}, found
}

func (state *fixDialogState) syncInput() bool {
	revised, err := fixapp.ApplyFormValues(state.input, state.fixFormValues())
	if err != nil {
		state.errorText = err.Error()
		return false
	}
	state.input = revised
	state.errorText = ""
	return true
}

func (state *fixDialogState) handleTargetScoreKey(key tea.KeyMsg) (targetScoreEditResult, tea.Cmd) {
	switch key.String() {
	case "enter":
		value, err := strconv.ParseFloat(strings.TrimSpace(state.score.Value()), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			state.scoreError = "Enter a non-negative number"
			return targetScoreEditResult{}, nil
		}
		previous := state.input.TargetScore
		state.input.TargetScore = value
		if !state.syncInput() {
			state.input.TargetScore = previous
			state.scoreError = cleanAgentText(state.errorText)
			state.errorText = ""
			return targetScoreEditResult{}, nil
		}
		state.scoreEditing = false
		state.score.Blur()
		state.scoreOriginal = value
		state.scoreError = ""
		return targetScoreEditResult{accepted: true, value: value}, nil
	case "esc", "escape":
		state.scoreEditing = false
		state.score.Blur()
		state.score.SetValue(formatTargetScore(state.scoreOriginal))
		state.scoreError = ""
		return targetScoreEditResult{canceled: true}, nil
	default:
		updated, command := state.score.Update(key)
		state.score = updated
		state.scoreError = ""
		return targetScoreEditResult{}, command
	}
}

func fixChoiceField(field int) bool {
	switch field {
	case fixFieldFocus, fixFieldProfile, fixFieldModel, fixFieldEffort, fixFieldScope, fixFieldWorkspace, fixFieldGit, fixFieldPublish:
		return true
	default:
		return false
	}
}

func (state fixDialogState) choices(field int) []fixDialogChoice {
	switch field {
	case fixFieldFocus:
		return metricChoices(state.metrics, state.focus)
	case fixFieldProfile:
		return profileChoices(state.input.Preferences.Profiles, state.input.Profile.ID)
	case fixFieldModel:
		return stringOptionChoices(state.input.Probe.Capabilities.Models, state.input.Model)
	case fixFieldEffort:
		return stringOptionChoices(state.input.Probe.Capabilities.Efforts, state.input.Effort)
	case fixFieldScope:
		return scopeChoices(state.input.ChangeScope)
	case fixFieldWorkspace:
		return workspaceChoices(state.input.DeliveryPlan.Workspace, state.input.Workspace.GitCommonDir != "")
	case fixFieldGit:
		return gitChoices(state.input.DeliveryPlan, state.input.Workspace.GitCommonDir != "")
	case fixFieldPublish:
		return publishChoices(state.input.DeliveryPlan.Publish)
	default:
		return nil
	}
}

func metricChoices(metrics []fix.MetricID, focus map[fix.MetricID]bool) []fixDialogChoice {
	choices := make([]fixDialogChoice, 0, len(metrics))
	for _, id := range metrics {
		choices = append(choices, fixDialogChoice{value: string(id), label: fixMetricLabel(id), selected: focus[id]})
	}
	return choices
}

func profileChoices(profiles []agent.Profile, selected agent.ProfileID) []fixDialogChoice {
	choices := make([]fixDialogChoice, 0, len(profiles))
	for _, profile := range profiles {
		choices = append(choices, fixDialogChoice{value: string(profile.ID), label: agentProfileChoiceLabel(profile), selected: profile.ID == selected})
	}
	return choices
}

func stringOptionChoices[T ~string](options []agent.Option[T], selected T) []fixDialogChoice {
	choices := make([]fixDialogChoice, 0, len(options))
	for _, option := range options {
		label := nonemptySetting(option.Label, string(option.ID))
		choices = append(choices, fixDialogChoice{value: string(option.ID), label: label, selected: option.ID == selected})
	}
	return choices
}

func scopeChoices(selected string) []fixDialogChoice {
	values := []string{"targets-only", "targets-and-tests", "repository"}
	choices := make([]fixDialogChoice, 0, len(values))
	for _, value := range values {
		choices = append(choices, fixDialogChoice{value: value, label: changeScopeLabel(value), selected: value == selected})
	}
	return choices
}

func workspaceChoices(workspace fix.WorkspaceMode, gitAvailable bool) []fixDialogChoice {
	return []fixDialogChoice{{value: string(fix.WorkspaceCurrent), label: workspaceModeLabel(fix.WorkspaceCurrent), selected: workspace == fix.WorkspaceCurrent}, {value: string(fix.WorkspaceWorktree), label: workspaceModeLabel(fix.WorkspaceWorktree), selected: workspace == fix.WorkspaceWorktree, disabled: !gitAvailable}}
}

func gitChoices(plan fix.DeliveryPlan, gitAvailable bool) []fixDialogChoice {
	values := []fix.GitMode{fix.GitLeaveUncommitted, fix.GitCommitCurrent, fix.GitCommitNewBranch}
	choices := make([]fixDialogChoice, 0, len(values))
	for _, value := range values {
		disabled := !gitAvailable && value != fix.GitLeaveUncommitted || plan.Workspace == fix.WorkspaceWorktree && value == fix.GitCommitCurrent
		choices = append(choices, fixDialogChoice{value: string(value), label: gitModeLabel(value), selected: value == plan.Git, disabled: disabled})
	}
	return choices
}

func publishChoices(selected fix.PublishMode) []fixDialogChoice {
	values := []fix.PublishMode{fix.PublishLocal, fix.PublishPush, fix.PublishPullRequest}
	choices := make([]fixDialogChoice, 0, len(values))
	for _, value := range values {
		choices = append(choices, fixDialogChoice{value: string(value), label: publishModeLabel(value), selected: value == selected})
	}
	return choices
}

func (state fixDialogState) runnable() bool {
	return state.hasInput
}

func (state fixDialogState) remediationSettingsKind() (configSettingsKind, bool) {
	if state.hasInput {
		if kind, ok := inputRemediationKind(state.input); ok {
			return kind, true
		}
	}
	return errorRemediationKind(state.errorText)
}

func inputRemediationKind(input fixapp.FixInput) (configSettingsKind, bool) {
	if input.Probe.State == agent.ProbeUnauthenticated || input.Probe.State == agent.ProbeUnavailable || input.Probe.State == agent.ProbeIncompatible || input.Probe.State == agent.ProbeDegraded || !input.Probe.Capabilities.Isolation.EligibleForMutation() {
		return configAgents, true
	}
	if !fixOptionContains(input.Probe.Capabilities.Models, input.Model) || !fixOptionContains(input.Probe.Capabilities.Efforts, input.Effort) {
		return configFix, true
	}
	if strings.TrimSpace(input.BranchName) == "" {
		return configDelivery, true
	}
	return "", false
}

func errorRemediationKind(value string) (configSettingsKind, bool) {
	message := strings.ToLower(value)
	if containsAny(message, "delivery", "branch", "remote", "pull request") {
		return configDelivery, true
	}
	if containsAny(message, "agent", "profile", "runtime", "executable", "authentication", "codex", "claude", "grok") {
		return configAgents, true
	}
	if containsAny(message, "config", "preference") {
		return configFix, true
	}
	return "", false
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func (state *fixDialogState) moveCursor(direction int) {
	fields := state.visibleFields()
	if len(fields) == 0 {
		return
	}
	position := fixFieldPosition(fields, state.cursor)
	position = min(max(0, position+direction), len(fields)-1)
	state.cursor = fields[position]
}

func (state *fixDialogState) ensureCursorVisible() {
	fields := state.visibleFields()
	if len(fields) == 0 {
		state.cursor = fixFieldTargetScore
		return
	}
	for _, field := range fields {
		if field == state.cursor {
			return
		}
	}
	state.cursor = fields[min(fixFieldPosition(fields, state.cursor), len(fields)-1)]
}

func cycleAgentOption[T ~string](options []agent.Option[T], current T, direction int) T {
	if len(options) == 0 {
		return current
	}
	index := 0
	for candidate := range options {
		if options[candidate].ID == current {
			index = candidate
		}
	}
	return options[fixCycleIndex(index, direction, len(options))].ID
}

func fixCycleString(options []string, current string, direction int) string {
	if len(options) == 0 {
		return current
	}
	index := 0
	for candidate := range options {
		if options[candidate] == current {
			index = candidate
		}
	}
	return options[fixCycleIndex(index, direction, len(options))]
}

func fixCycleIndex(current, direction, length int) int {
	if length <= 0 {
		return 0
	}
	return (current + direction%length + length) % length
}

func formatTargetScore(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func (state *fixDialogState) openChoice() {
	choices := state.choices(state.cursor)
	if len(choices) == 0 {
		return
	}
	state.choiceOpen = true
	state.choiceField = state.cursor
	state.choiceCursor = 0
	for index, choice := range choices {
		if !choice.disabled {
			state.choiceCursor = index
			break
		}
	}
	for index, choice := range choices {
		if choice.selected && !choice.disabled {
			state.choiceCursor = index
			break
		}
	}
}

func (state *fixDialogState) handleChoiceKey(name string) (fixDialogChoice, bool) {
	choices := state.choices(state.choiceField)
	if len(choices) == 0 {
		state.choiceOpen = false
		return fixDialogChoice{}, false
	}
	switch name {
	case "esc", "escape", "q":
		state.choiceOpen = false
	case "up", "k":
		state.choiceCursor = max(0, state.choiceCursor-1)
	case "down", "j":
		state.choiceCursor = min(len(choices)-1, state.choiceCursor+1)
	default:
		if isToggleKey(name) && !choices[state.choiceCursor].disabled {
			return choices[state.choiceCursor], true
		}
	}
	return fixDialogChoice{}, false
}
