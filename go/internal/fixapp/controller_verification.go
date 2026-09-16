package fixapp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
)

func (record *jobRecord) applyVerification(result workerResult) {
	record.applyDiffInventory(result.diff)
	applyVerifiedFiles(record, result.verify)
	if result.verify.Complete && result.verify.TargetMet && result.verify.Stable() {
		record.presentation.TargetStatus = fix.TargetMet
		record.nextAttemptNotes = ""
		return
	}
	record.presentation.TargetStatus = fix.TargetNotMet
	record.presentation.Attention = fix.AttentionError
	record.nextAttemptNotes = nextAttemptNotes(record, result)
}

func applyVerifiedFiles(record *jobRecord, verification fixanalysis.VerificationResult) {
	for index := range record.presentation.Targets {
		verified, ok := verifiedFile(verification, record.presentation.Targets[index].Path)
		if !ok {
			continue
		}
		score := verified.Score
		record.presentation.Targets[index].VerifiedScore = &score
		record.presentation.Targets[index].Verification = verified.Diagnostic
		record.presentation.Targets[index].VerifiedMetrics = metricValues(verified.Metrics)
	}
}

func verifiedFile(verification fixanalysis.VerificationResult, path fix.RepoPath) (fixanalysis.FileResult, bool) {
	for _, verified := range verification.Files {
		if verified.Path == path {
			return verified, true
		}
	}
	return fixanalysis.FileResult{}, false
}

func nextAttemptNotes(record *jobRecord, result workerResult) string {
	parts := []string{fmt.Sprintf("attempt %d", record.presentation.AttemptOrdinal)}
	var overallDiagnostic string
	if record.presentation.TargetStatus == fix.TargetNotMet {
		parts = append(parts, "target score not met")
		overallDiagnostic = boundedRetryDiagnostic(result.verify.Diagnostic)
	}
	for _, verified := range result.verify.Files {
		parts = append(parts, verifiedRetryLine(record, verified))
	}
	if overallDiagnostic != "" {
		parts = append(parts, overallDiagnostic)
	}
	return sanitizeSummary(strings.Join(parts, "; "))
}

func verifiedRetryLine(record *jobRecord, verified fixanalysis.FileResult) string {
	measurements := []string{fmt.Sprintf("SCORE %.1f/%.1f", verified.Score, record.input.TargetScore)}
	seen := focusMeasurements(record, verified, &measurements)
	regressionMeasurements(record, verified, seen, &measurements)
	line := fmt.Sprintf("%s: %s", verified.Path, strings.Join(measurements, ", "))
	if diagnostic := boundedRetryDiagnostic(verified.Diagnostic); diagnostic != "" {
		line += " (" + diagnostic + ")"
	}
	return line
}

func focusMeasurements(record *jobRecord, verified fixanalysis.FileResult, measurements *[]string) map[fix.MetricID]bool {
	seen := map[fix.MetricID]bool{}
	for _, focus := range record.input.Focus {
		metric, ok := verified.Metrics[focus.Metric]
		if !ok {
			continue
		}
		*measurements = append(*measurements, fmt.Sprintf("%s %.1f/%.1f", metricLabel(metric, focus.Metric), metric.Value, focus.Maximum))
		seen[focus.Metric] = true
	}
	return seen
}

func regressionMeasurements(record *jobRecord, verified fixanalysis.FileResult, seen map[fix.MetricID]bool, measurements *[]string) {
	ids := make([]fix.MetricID, 0, len(record.input.Baseline.Contract.Goal.AllowedRegression))
	for metric := range record.input.Baseline.Contract.Goal.AllowedRegression {
		if !seen[metric] {
			ids = append(ids, metric)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		metric, ok := verified.Metrics[id]
		if !ok {
			continue
		}
		limit := regressionLimit(record.input.Baseline.Contract, verified.Path, id)
		*measurements = append(*measurements, fmt.Sprintf("%s %.1f/%.1f", metricLabel(metric, id), metric.Value, limit))
	}
}

func metricLabel(metric fix.MetricValue, id fix.MetricID) string {
	if strings.TrimSpace(metric.Label) != "" {
		return metric.Label
	}
	return string(id)
}

func regressionLimit(contract fix.ScoringContract, path fix.RepoPath, metric fix.MetricID) float64 {
	for _, target := range contract.Targets {
		if target.Path == path {
			return target.Metrics[metric].Value + contract.Goal.AllowedRegression[metric]
		}
	}
	return contract.Goal.AllowedRegression[metric]
}

func boundedRetryDiagnostic(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 160 {
		value = value[:160] + "…"
	}
	return sanitizeSummary(value)
}
