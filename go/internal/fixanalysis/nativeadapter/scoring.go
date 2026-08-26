package nativeadapter

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

func validateGoal(goal fix.ScoringGoal) error {
	if math.IsNaN(goal.MaximumScore) || math.IsInf(goal.MaximumScore, 0) || goal.MaximumScore < 0 {
		return errors.New("maximum score must be finite and non-negative")
	}
	seen := make(map[fix.MetricID]struct{}, len(goal.Focus))
	for _, item := range goal.Focus {
		if _, exists := scoring.MetricDefinitionByID(scoring.MetricID(item.Metric)); !exists {
			return fmt.Errorf("unknown focus metric %q", item.Metric)
		}
		if math.IsNaN(item.Maximum) || math.IsInf(item.Maximum, 0) || item.Maximum < 0 {
			return fmt.Errorf("focus metric %q maximum must be finite and non-negative", item.Metric)
		}
		if _, duplicate := seen[item.Metric]; duplicate {
			return fmt.Errorf("duplicate focus metric %q", item.Metric)
		}
		seen[item.Metric] = struct{}{}
	}
	for metric, allowance := range goal.AllowedRegression {
		if _, exists := scoring.MetricDefinitionByID(scoring.MetricID(metric)); !exists {
			return fmt.Errorf("unknown regression metric %q", metric)
		}
		if math.IsNaN(allowance) || math.IsInf(allowance, 0) || allowance < 0 {
			return fmt.Errorf("regression allowance for %q must be finite and non-negative", metric)
		}
	}
	return nil
}

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

func requiredMetricsComplete(values map[fix.MetricID]fix.MetricValue, goal fix.ScoringGoal) error {
	for _, focus := range goal.Focus {
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

func freshAndComplete(file report.File) bool {
	return file.Complete && (file.Freshness == "" || file.Freshness == report.FreshnessCurrent)
}

func metricExpression(definition scoring.MetricDefinition) string {
	if definition.Axis != "" {
		return string(definition.Aggregation) + "(" + definition.Axis + ")"
	}
	return string(definition.Aggregation) + "(" + definition.ComponentID + ")"
}
