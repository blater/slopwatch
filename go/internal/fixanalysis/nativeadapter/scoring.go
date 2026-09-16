package nativeadapter

import (
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

func targetSnapshot(path fix.RepoPath, contentHash string, file report.File, mapper pathMapper) (fix.TargetSnapshot, error) {
	metrics := metricValues(file)
	evidence, err := metricEvidence(file, path, mapper)
	if err != nil {
		return fix.TargetSnapshot{}, fmt.Errorf("collect evidence for %q: %w", path, err)
	}
	return fix.TargetSnapshot{
		Path: path, ContentHash: contentHash, Language: file.Language,
		Score: file.Score, Metrics: metrics, Evidence: evidence, Complete: freshAndComplete(file),
	}, nil
}

func metricValues(file report.File) map[fix.MetricID]fix.MetricValue {
	result := make(map[fix.MetricID]fix.MetricValue)
	for _, definition := range scoring.Metrics() {
		value := scoring.Metric(file, string(definition.ID))
		result[fix.MetricID(definition.ID)] = fix.MetricValue{
			ID: fix.MetricID(definition.ID), Label: metricLabel(definition.ID),
			Value: value.Value, Complete: freshAndComplete(file) && value.Available,
		}
	}
	return result
}

func metricLabel(id scoring.MetricID) string {
	switch id {
	case scoring.MetricCognitive:
		return "COG"
	case scoring.MetricNPath:
		return "NPATH"
	case scoring.MetricCyclomatic:
		return "CYCLO"
	case scoring.MetricShallowness:
		return "SHALLOW"
	case scoring.MetricGodClass:
		return "GOD"
	case scoring.MetricCoupling:
		return "COUPLING"
	case scoring.MetricNesting:
		return "NESTING"
	case scoring.MetricTypeSafety:
		return "TYPE SAFETY"
	default:
		return strings.ToUpper(string(id))
	}
}

func freshAndComplete(file report.File) bool {
	return file.Complete && (file.Freshness == "" || file.Freshness == report.FreshnessCurrent)
}

func metricExpression(definition scoring.MetricDefinition) string {
	if definition.Axis != "" {
		return string(definition.Aggregation) + "(" + definition.Axis + ")"
	}
	return string(definition.Aggregation) + "(" + definition.ComponentID + ")"
}
