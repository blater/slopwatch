package follow

import (
	"github.com/blater/slopwatch/internal/fix"
	"github.com/charmbracelet/lipgloss"
)

func (model Model) agentMetricPolicy() agentMetricPolicy {
	return agentMetricPolicy{Compact: model.options.Compact, Visible: model.files.Visible, Weights: model.weights, Enabled: model.weightEnabled}
}

func maximumAgentHorizontalOffset(rows []agentLogicalRow, tier ResponsiveTier, width int, visibleMetric func(fix.MetricID) bool) int {
	maximum := 0
	for _, row := range rows {
		if row.File == nil {
			continue
		}
		path := agentFileDisplayPath(*row.File)
		metrics := visibleAgentFileMetrics(*row.File, visibleMetric)
		viewport := agentFilePathViewport(*row.File, tier, width, metrics)
		maximum = max(maximum, lipgloss.Width(path)-viewport)
	}
	return maximum
}
