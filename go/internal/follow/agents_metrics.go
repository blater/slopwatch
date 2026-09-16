package follow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func agentFileMetricsMatching(file fix.FilePresentation, include func(fix.MetricValue) bool) string {
	baselineMetrics := file.BaselineMetrics
	if len(baselineMetrics) == 0 {
		baselineMetrics = file.Metrics
	}
	baselineMetrics = append([]fix.MetricValue(nil), baselineMetrics...)
	sort.SliceStable(baselineMetrics, func(left, right int) bool {
		leftRank, rightRank := agentMetricColumnRank(baselineMetrics[left].ID), agentMetricColumnRank(baselineMetrics[right].ID)
		if leftRank == rightRank {
			return baselineMetrics[left].ID < baselineMetrics[right].ID
		}
		return leftRank < rightRank
	})
	if agentFileClass(file) == "S" && file.VerifiedScore == nil && len(baselineMetrics) == 0 {
		if file.Changed {
			status := strings.TrimSpace(file.ChangeStatus)
			if status == "" {
				status = "modified"
			}
			return "supporting file · " + cleanAgentText(status)
		}
		return "supporting file · -"
	}
	verified := "…"
	if file.VerifiedScore != nil {
		verified = roundedIntegerText(*file.VerifiedScore) + agentVerificationGlyph(file.Verification)
	}
	parts := []string{fmt.Sprintf("SCORE %s→%s", roundedIntegerText(file.BaselineScore), verified)}
	verifiedMetrics := make(map[fix.MetricID]fix.MetricValue, len(file.VerifiedMetrics))
	for _, metric := range file.VerifiedMetrics {
		verifiedMetrics[metric.ID] = metric
	}
	for _, metric := range baselineMetrics {
		if !include(metric) {
			continue
		}
		label := agentMetricColumnTitle(metric.ID)
		value := "-"
		if metric.Complete {
			value = roundedIntegerText(metric.Value)
		}
		if after, ok := verifiedMetrics[metric.ID]; ok && after.Complete {
			value += "→" + roundedIntegerText(after.Value)
		}
		parts = append(parts, label+" "+value)
	}
	return strings.Join(parts, " · ")
}
