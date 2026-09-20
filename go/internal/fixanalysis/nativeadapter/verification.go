package nativeadapter

import (
	"fmt"
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

func verifyFile(baseline fix.TargetSnapshot, file report.File, depths map[string]report.DepthBoundary, goal fix.ScoringGoal, requireComplete bool) (fixanalysis.FileResult, error) {
	metrics := metricValues(file)
	complete := freshAndComplete(file)
	depthEstimate := hasEstimatedDepth(file, depths)
	if depthEstimate {
		complete = false
	}
	if err := requiredMetricsComplete(metrics, goal); err != nil {
		complete = false
	}
	targetMet := (!requireComplete || complete) && file.Score <= goal.MaximumScore
	diagnostics := make([]string, 0)
	if depthEstimate {
		diagnostics = append(diagnostics, "SHALLOW evidence is estimated")
	}
	if file.Score > goal.MaximumScore {
		diagnostics = append(diagnostics, fmt.Sprintf("score %.1f exceeds %.1f", file.Score, goal.MaximumScore))
	}
	focused, focusDiagnostics, focusMet := evaluateFocusMetrics(metrics, goal)
	targetMet = targetMet && focusMet
	diagnostics = append(diagnostics, focusDiagnostics...)
	regressionDiagnostics, regressionsMet := evaluateRegressions(baseline, metrics, goal, focused)
	targetMet = targetMet && regressionsMet
	diagnostics = append(diagnostics, regressionDiagnostics...)
	if !complete {
		diagnostics = append(diagnostics, "analysis result is incomplete")
	}
	return fixanalysis.FileResult{
		Path: baseline.Path, Score: file.Score, Metrics: metrics,
		Complete: complete, TargetMet: targetMet, Diagnostic: strings.Join(diagnostics, "; "),
	}, nil
}

func hasEstimatedDepth(file report.File, depths map[string]report.DepthBoundary) bool {
	component, ok := file.Components["module_shallowness"]
	if !ok || component.DepthVersion != "responsibility-burden-v4" {
		return false
	}
	if component.DepthEstimated {
		return true
	}
	for _, id := range component.DepthBoundaryIDs {
		if depths[id].Estimated {
			return true
		}
	}
	return false
}
