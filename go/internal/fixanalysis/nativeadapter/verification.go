package nativeadapter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/report"
)

func requiredMetricsComplete(values map[fix.MetricID]fix.MetricValue, goal fix.ScoringGoal) error {
	for _, focus := range goal.Focus {
		if focus.Metric == fix.MetricScore {
			continue
		}
		if !values[focus.Metric].Complete {
			return fmt.Errorf("focus metric %q is incomplete", focus.Metric)
		}
	}
	for metric := range goal.AllowedRegression {
		if !values[metric].Complete {
			return fmt.Errorf("regression metric %q is incomplete", metric)
		}
	}
	return nil
}

func verifyFile(baseline fix.TargetSnapshot, file report.File, goal fix.ScoringGoal, requireComplete bool) (fixanalysis.FileResult, error) {
	metrics := metricValues(file)
	complete := freshAndComplete(file)
	if err := requiredMetricsComplete(metrics, goal); err != nil {
		complete = false
	}
	targetMet := (!requireComplete || complete) && file.Score <= goal.MaximumScore
	diagnostics := make([]string, 0)
	if file.Score > goal.MaximumScore {
		diagnostics = append(diagnostics, fmt.Sprintf("score %.1f exceeds %.1f", file.Score, goal.MaximumScore))
	}
	focused := make(map[fix.MetricID]struct{}, len(goal.Focus))
	for _, focus := range goal.Focus {
		focused[focus.Metric] = struct{}{}
		if focus.Metric == fix.MetricScore {
			continue
		}
		value := metrics[focus.Metric]
		if !value.Complete || value.Value > focus.Maximum {
			targetMet = false
			diagnostics = append(diagnostics, fmt.Sprintf("%s %.1f exceeds %.1f", value.Label, value.Value, focus.Maximum))
		}
	}
	metricIDs := make([]fix.MetricID, 0, len(baseline.Metrics))
	for metric := range baseline.Metrics {
		if _, isFocus := focused[metric]; !isFocus {
			metricIDs = append(metricIDs, metric)
		}
	}
	sort.Slice(metricIDs, func(i, j int) bool { return metricIDs[i] < metricIDs[j] })
	for _, metric := range metricIDs {
		before := baseline.Metrics[metric]
		after := metrics[metric]
		if !before.Complete {
			continue
		}
		allowance := goal.AllowedRegression[metric]
		if !after.Complete || after.Value > before.Value+allowance {
			targetMet = false
			diagnostics = append(diagnostics, fmt.Sprintf("%s regressed from %.1f to %.1f (allowance %.1f)", before.Label, before.Value, after.Value, allowance))
		}
	}
	if !complete {
		diagnostics = append(diagnostics, "analysis result is incomplete")
	}
	return fixanalysis.FileResult{
		Path: baseline.Path, Score: file.Score, Metrics: metrics,
		Complete: complete, TargetMet: targetMet, Diagnostic: strings.Join(diagnostics, "; "),
	}, nil
}
