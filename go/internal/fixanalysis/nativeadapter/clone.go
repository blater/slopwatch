package nativeadapter

import (
	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/fixanalysis"
)

func cloneGoal(value fix.ScoringGoal) fix.ScoringGoal {
	result := value
	result.Focus = append([]fix.MetricGoal(nil), value.Focus...)
	result.AllowedRegression = make(map[fix.MetricID]float64, len(value.AllowedRegression))
	for metric, allowance := range value.AllowedRegression {
		result.AllowedRegression[metric] = allowance
	}
	return result
}

func cloneContract(value fix.ScoringContract) fix.ScoringContract {
	result := value
	result.Goal = cloneGoal(value.Goal)
	result.Targets = make([]fix.TargetSnapshot, len(value.Targets))
	for index, target := range value.Targets {
		result.Targets[index] = target
		result.Targets[index].Metrics = cloneMetrics(target.Metrics)
		result.Targets[index].Evidence = make([]fix.MetricEvidence, len(target.Evidence))
		for evidenceIndex, evidence := range target.Evidence {
			result.Targets[index].Evidence[evidenceIndex] = evidence
			result.Targets[index].Evidence[evidenceIndex].Values = append([]float64(nil), evidence.Values...)
			result.Targets[index].Evidence[evidenceIndex].Paths = append([]fix.RepoPath(nil), evidence.Paths...)
		}
	}
	return result
}

func cloneMetrics(values map[fix.MetricID]fix.MetricValue) map[fix.MetricID]fix.MetricValue {
	result := make(map[fix.MetricID]fix.MetricValue, len(values))
	for metric, value := range values {
		result[metric] = value
	}
	return result
}

func cloneVerification(value fixanalysis.VerificationResult) fixanalysis.VerificationResult {
	result := value
	result.Files = make([]fixanalysis.FileResult, len(value.Files))
	for index, file := range value.Files {
		result.Files[index] = file
		result.Files[index].Metrics = cloneMetrics(file.Metrics)
	}
	return result
}

func contractPaths(contract fix.ScoringContract) []fix.RepoPath {
	result := make([]fix.RepoPath, len(contract.Targets))
	for index, target := range contract.Targets {
		result[index] = target.Path
	}
	return result
}
