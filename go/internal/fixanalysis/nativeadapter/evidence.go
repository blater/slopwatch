package nativeadapter

import (
	"fmt"
	"sort"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

func metricEvidence(file report.File, target fix.RepoPath, mapper pathMapper) ([]fix.MetricEvidence, error) {
	result := make([]fix.MetricEvidence, 0, len(scoring.Metrics()))
	componentIDs := make([]string, 0, len(file.Components))
	for componentID := range file.Components {
		componentIDs = append(componentIDs, componentID)
	}
	sort.Strings(componentIDs)
	for _, definition := range scoring.Metrics() {
		values := make([]float64, 0)
		paths := make([]fix.RepoPath, 0)
		for _, componentID := range componentIDs {
			component := file.Components[componentID]
			componentDefinition, known := scoring.ComponentByID(componentID)
			matches := definition.ComponentID == componentID ||
				(definition.Axis != "" && known && componentDefinition.Axis == definition.Axis)
			if !matches {
				continue
			}
			for _, item := range component.Evidence {
				values = append(values, item.Value)
				path := target
				if item.Location.Path != "" {
					parsed, err := mapper.repositoryPath(item.Location.Path)
					if err != nil {
						return nil, fmt.Errorf("metric %q has invalid evidence path %q", definition.ID, item.Location.Path)
					}
					path = parsed
				}
				paths = append(paths, path)
			}
		}
		result = append(result, fix.MetricEvidence{
			Metric: fix.MetricID(definition.ID), Summary: metricExpression(definition),
			Values: values, Paths: paths,
		})
	}
	return result, nil
}
