package fixapp

import (
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixprompt"
)

func ApplyFormValues(input FixInput, values FormValues) (FixInput, error) {
	result := cloneFixInput(input)
	result.TargetScore, result.Focus, result.ChangeScope = values.TargetScore, append([]fix.MetricGoal(nil), values.Focus...), values.ChangeScope
	allowed, err := pathsForScope(values.ChangeScope, result.Targets, result.PlannedPaths)
	if err != nil {
		return FixInput{}, err
	}
	result.AllowedPaths = allowed
	result.DeliveryPlan, result.BranchName = values.DeliveryPlan, values.BranchName
	if values.DeliveryPlan.Git == fix.GitCommitCurrent {
		result.BranchName = result.Workspace.CurrentBranch
	} else if values.DeliveryPlan.Git == fix.GitLeaveUncommitted {
		result.BranchName = ""
	}
	result.Baseline.Contract.Goal.MaximumScore = values.TargetScore
	result.Baseline.Contract.Goal.Focus = append([]fix.MetricGoal(nil), values.Focus...)
	instructions, err := fixprompt.Compile(fixprompt.Input{Contract: result.Baseline.Contract, AllowedScope: values.ChangeScope,
		AllowedPaths: result.AllowedPaths, BranchName: result.BranchName, Template: result.Preferences.Fix.PromptTemplate})
	if err != nil {
		return FixInput{}, err
	}
	result.Instructions = instructions
	return result, nil
}

func measuredFocusMetrics(ids []fix.MetricID) []fix.MetricID {
	result := make([]fix.MetricID, 0, len(ids))
	for _, id := range ids {
		if id != fix.MetricScore {
			result = append(result, id)
		}
	}
	return result
}

func configuredFocusGoals(ids []fix.MetricID, targets []fix.TargetSnapshot, targetScore float64) ([]fix.MetricGoal, error) {
	goals := make([]fix.MetricGoal, 0, len(ids))
	for _, id := range ids {
		goal, err := focusGoal(id, targets, targetScore)
		if err != nil {
			return nil, err
		}
		if goal != nil {
			goals = append(goals, *goal)
		}
	}
	return goals, nil
}

func focusGoal(id fix.MetricID, targets []fix.TargetSnapshot, targetScore float64) (*fix.MetricGoal, error) {
	if id == fix.MetricScore {
		return &fix.MetricGoal{Metric: id, Maximum: targetScore}, nil
	}
	maximum, found, err := maximumMetric(targets, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &fix.MetricGoal{Metric: id, Maximum: maximum}, nil
}

func maximumMetric(targets []fix.TargetSnapshot, id fix.MetricID) (float64, bool, error) {
	maximum, found := 0.0, false
	for _, target := range targets {
		metric, ok := target.Metrics[id]
		if !ok {
			continue
		}
		if !found || metric.Value > maximum {
			maximum, found = metric.Value, true
		}
	}
	return maximum, found, nil
}
