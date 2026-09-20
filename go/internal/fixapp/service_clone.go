package fixapp

import (
	"sort"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
)

func cloneFixInput(input FixInput) FixInput {
	input.Targets = append([]fix.RepoPath(nil), input.Targets...)
	input.AllowedPaths = append([]fix.RepoPath(nil), input.AllowedPaths...)
	input.PlannedPaths = append([]fix.RepoPath(nil), input.PlannedPaths...)
	input.Profile = cloneProfile(input.Profile)
	input.Probe = cloneProbe(input.Probe)
	input.Preferences = cloneResolved(input.Preferences)
	input.Baseline.Contract = cloneContract(input.Baseline.Contract)
	input.Focus = append([]fix.MetricGoal(nil), input.Focus...)
	return input
}

func cloneResolved(value appconfig.Resolved) appconfig.Resolved {
	result := value
	result.Origins = make(map[string]appconfig.Origin, len(value.Origins))
	for key, origin := range value.Origins {
		result.Origins[key] = origin
	}
	result.Fix.Focus = append([]fix.MetricID(nil), value.Fix.Focus...)
	result.Profiles = make([]agent.Profile, len(value.Profiles))
	for index, profile := range value.Profiles {
		result.Profiles[index] = cloneProfile(profile)
	}
	return result
}

func cloneContract(value fix.ScoringContract) fix.ScoringContract {
	result := value
	result.Goal.Focus = append([]fix.MetricGoal(nil), value.Goal.Focus...)
	result.Goal.AllowedRegression = cloneRegression(value.Goal.AllowedRegression)
	result.Targets = make([]fix.TargetSnapshot, len(value.Targets))
	for index, target := range value.Targets {
		result.Targets[index] = target
		result.Targets[index].Metrics = make(map[fix.MetricID]fix.MetricValue, len(target.Metrics))
		for metric, metricValue := range target.Metrics {
			result.Targets[index].Metrics[metric] = metricValue
		}
		result.Targets[index].Evidence = append([]fix.MetricEvidence(nil), target.Evidence...)
	}
	return result
}

func cloneRegression(source map[fix.MetricID]float64) map[fix.MetricID]float64 {
	result := make(map[fix.MetricID]float64, len(source))
	for metric, allowance := range source {
		result[metric] = allowance
	}
	return result
}

func cloneProfile(profile agent.Profile) agent.Profile {
	profile.Options = cloneStringMap(profile.Options)
	return profile
}

func cloneProbe(probe agent.ProbeResult) agent.ProbeResult {
	probe.Capabilities.Models = append([]agent.Option[agent.ModelID](nil), probe.Capabilities.Models...)
	probe.Capabilities.Efforts = append([]agent.Option[agent.EffortID](nil), probe.Capabilities.Efforts...)
	probe.Capabilities.Network.ToolDomains = append([]string(nil), probe.Capabilities.Network.ToolDomains...)
	return probe
}

func cloneStringMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func isQuiescent(phase fix.Phase) bool {
	return phase == fix.PhaseFailed || phase == fix.PhaseCompleted || phase == fix.PhaseCanceled || phase == fix.PhaseDiscarded
}

func sortPresentations(values []fix.JobPresentation) {
	sort.SliceStable(values, func(i, j int) bool { return values[i].CreatedAt.Before(values[j].CreatedAt) })
}
