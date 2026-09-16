package follow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func agentFileMetricsMatching(file fix.FilePresentation, include func(fix.MetricValue) bool) string {
	baselineMetrics := agentBaselineMetrics(file)
	if supporting := agentSupportingFileText(file, len(baselineMetrics)); supporting != "" {
		return supporting
	}
	parts := []string{agentScoreTransitionText(file)}
	verifiedMetrics := make(map[fix.MetricID]fix.MetricValue, len(file.VerifiedMetrics))
	for _, metric := range file.VerifiedMetrics {
		verifiedMetrics[metric.ID] = metric
	}
	for _, metric := range baselineMetrics {
		if !include(metric) {
			continue
		}
		parts = append(parts, agentMetricTransitionText(metric, verifiedMetrics))
	}
	return strings.Join(parts, " · ")
}

func agentBaselineMetrics(file fix.FilePresentation) []fix.MetricValue {
	metrics := file.BaselineMetrics
	if len(metrics) == 0 {
		metrics = file.Metrics
	}
	metrics = append([]fix.MetricValue(nil), metrics...)
	sort.SliceStable(metrics, func(left, right int) bool {
		leftRank, rightRank := agentMetricColumnRank(metrics[left].ID), agentMetricColumnRank(metrics[right].ID)
		if leftRank == rightRank {
			return metrics[left].ID < metrics[right].ID
		}
		return leftRank < rightRank
	})
	return metrics
}

func agentSupportingFileText(file fix.FilePresentation, metricCount int) string {
	if agentFileClass(file) != "S" || file.VerifiedScore != nil || metricCount != 0 {
		return ""
	}
	if !file.Changed {
		return "supporting file · -"
	}
	status := strings.TrimSpace(file.ChangeStatus)
	if status == "" {
		status = "modified"
	}
	return "supporting file · " + cleanAgentText(status)
}

func agentScoreTransitionText(file fix.FilePresentation) string {
	verified := "…"
	if file.VerifiedScore != nil {
		verified = roundedIntegerText(*file.VerifiedScore) + agentVerificationGlyph(file.Verification)
	}
	return fmt.Sprintf("SCORE %s→%s", roundedIntegerText(file.BaselineScore), verified)
}

func agentMetricTransitionText(metric fix.MetricValue, verified map[fix.MetricID]fix.MetricValue) string {
	value := "-"
	if metric.Complete {
		value = roundedIntegerText(metric.Value)
	}
	if after, ok := verified[metric.ID]; ok && after.Complete {
		value += "→" + roundedIntegerText(after.Value)
	}
	return agentMetricColumnTitle(metric.ID) + " " + value
}
