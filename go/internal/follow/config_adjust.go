package follow

import (
	"math"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
)

func (model *Model) adjustConfigSetting(direction int) tea.Cmd {
	state := &model.configSettings
	if !state.adjust(direction) {
		return nil
	}
	return model.saveConfigSettings()
}

func (state *configSettingsState) adjust(direction int) bool {
	if state.loading {
		return false
	}
	changed := false
	switch state.kind {
	case configFix:
		changed = adjustFixSetting(&state.working, state.cursor, direction, state.probes)
	case configConcurrency:
		changed = adjustConcurrencySetting(&state.working.Concurrency, state.cursor, direction)
	case configDelivery:
		changed = adjustDeliverySetting(&state.working.Delivery, state.cursor, direction)
	}
	if changed {
		state.dirty = true
	}
	return changed
}

const (
	fixSettingsPromptRow   = 5
	fixSettingsMetricStart = 6
)

func adjustFixSetting(value *appconfig.Resolved, cursor, direction int, probes map[agent.ProfileID]agent.ProbeResult) bool {
	switch cursor {
	case 0:
		value.Fix.TargetScore = math.Max(0, value.Fix.TargetScore+float64(direction*5))
	case 1:
		value.Fix.ChangeScope = cycleString(value.Fix.ChangeScope, []string{"targets-only", "targets-and-tests", "repository"}, direction)
	case 2:
		profiles := []agent.ProfileID{""}
		for _, profile := range value.Profiles {
			profiles = append(profiles, profile.ID)
		}
		value.Fix.Profile = cycleTyped(value.Fix.Profile, profiles, direction)
		probe, ready := readyAgentProbe(probes, value.Fix.Profile)
		reconcileFixAgentOptions(&value.Fix, probe, ready)
	case 3:
		probe, _ := readyAgentProbe(probes, value.Fix.Profile)
		options := modelIDs(probe)
		if len(options) == 0 {
			return false
		}
		value.Fix.Model = cycleTyped(value.Fix.Model, options, direction)
	case 4:
		probe, _ := readyAgentProbe(probes, value.Fix.Profile)
		options := effortIDs(probe)
		if len(options) == 0 {
			return false
		}
		value.Fix.Effort = cycleTyped(value.Fix.Effort, options, direction)
	case fixSettingsPromptRow:
		// Edited in the multiline master-prompt editor.
		return false
	default:
		metrics := fixSettingsMetrics()
		index := cursor - fixSettingsMetricStart
		if index < 0 || index >= len(metrics) {
			return false
		}
		value.Fix.Focus = toggleMetric(value.Fix.Focus, metrics[index])
	}
	return true
}

func adjustConcurrencySetting(value *appconfig.Concurrency, cursor, direction int) bool {
	switch cursor {
	case 0:
		value.MaxAgents = max(1, value.MaxAgents+direction)
	case 1:
		value.MaxVerifiers = max(1, value.MaxVerifiers+direction)
	case 2:
		value.MaxActorsPerJob = max(1, value.MaxActorsPerJob+direction)
	case 3:
		value.MaxCandidatePreviewBytes = maxInt64(1, value.MaxCandidatePreviewBytes+int64(direction)*256*1024)
	case 4:
		value.MaxCandidatePreviewLines = max(1, value.MaxCandidatePreviewLines+direction*100)
	default:
		return false
	}
	return true
}

const (
	deliverySettingWorkspace = iota
	deliverySettingGit
	deliverySettingPublish
	deliverySettingBranch
	deliverySettingRemote
	deliverySettingBase
	deliverySettingPRState
	deliverySettingCommitTitle
	deliverySettingCommitBody
	deliverySettingPRTitle
	deliverySettingPRBody
)

func deliverySettingFields(value appconfig.Delivery) []int {
	fields := []int{deliverySettingWorkspace, deliverySettingGit}
	if value.DefaultPlan.Git == fix.GitLeaveUncommitted {
		return fields
	}
	fields = append(fields, deliverySettingPublish)
	if value.DefaultPlan.Git == fix.GitCommitNewBranch {
		fields = append(fields, deliverySettingBranch)
	}
	if value.DefaultPlan.Publish != fix.PublishLocal {
		fields = append(fields, deliverySettingRemote)
	}
	if value.DefaultPlan.Publish == fix.PublishPullRequest {
		fields = append(fields, deliverySettingBase, deliverySettingPRState)
	}
	fields = append(fields, deliverySettingCommitTitle, deliverySettingCommitBody)
	if value.DefaultPlan.Publish == fix.PublishPullRequest {
		fields = append(fields, deliverySettingPRTitle, deliverySettingPRBody)
	}
	return fields
}

func deliverySettingField(value appconfig.Delivery, cursor int) int {
	fields := deliverySettingFields(value)
	if cursor < 0 || cursor >= len(fields) {
		return -1
	}
	return fields[cursor]
}

func adjustDeliverySetting(value *appconfig.Delivery, cursor, direction int) bool {
	switch deliverySettingField(*value, cursor) {
	case deliverySettingPRState:
		value.DraftPullRequests = !value.DraftPullRequests
	default:
		return false
	}
	return true
}
