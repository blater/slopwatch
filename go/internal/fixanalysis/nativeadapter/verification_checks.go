package nativeadapter

import (
	"fmt"
	"sort"

	"github.com/blater/slopwatch/internal/fix"
)

func evaluateFocusMetrics(metrics map[fix.MetricID]fix.MetricValue, goal fix.ScoringGoal) (map[fix.MetricID]struct{}, []string, bool) {
	focused := make(map[fix.MetricID]struct{}, len(goal.Focus))
	diagnostics := make([]string, 0)
	met := true
	for _, focus := range goal.Focus {
		focused[focus.Metric] = struct{}{}
		if focus.Metric == fix.MetricScore {
			continue
		}
		value := metrics[focus.Metric]
		if !value.Complete || value.Value > focus.Maximum {
			met = false
			diagnostics = append(diagnostics, fmt.Sprintf("%s %.1f exceeds %.1f", value.Label, value.Value, focus.Maximum))
		}
	}
	return focused, diagnostics, met
}

func evaluateRegressions(baseline fix.TargetSnapshot, metrics map[fix.MetricID]fix.MetricValue, goal fix.ScoringGoal, focused map[fix.MetricID]struct{}) ([]string, bool) {
	metricIDs := make([]fix.MetricID, 0, len(baseline.Metrics))
	for metric := range baseline.Metrics {
		if _, isFocus := focused[metric]; !isFocus {
			metricIDs = append(metricIDs, metric)
		}
	}
	sort.Slice(metricIDs, func(i, j int) bool { return metricIDs[i] < metricIDs[j] })
	diagnostics := make([]string, 0)
	met := true
	for _, metric := range metricIDs {
		before := baseline.Metrics[metric]
		if !before.Complete {
			continue
		}
		after := metrics[metric]
		allowance := goal.AllowedRegression[metric]
		if !after.Complete || after.Value > before.Value+allowance {
			met = false
			diagnostics = append(diagnostics, fmt.Sprintf("%s regressed from %.1f to %.1f (allowance %.1f)", before.Label, before.Value, after.Value, allowance))
		}
	}
	return diagnostics, met
}
