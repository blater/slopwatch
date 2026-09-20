package follow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

func updateScanProgress(model *Model, document report.Document) {
	if model.runtime.scanProgress == nil {
		model.runtime.scanProgress = map[string]report.ScanProgress{}
	}
	if model.runtime.scanFiles == nil {
		model.runtime.scanFiles = map[string]bool{}
	}
	for language, progress := range document.Progress {
		if progress.Files == 0 {
			progress.Files = model.runtime.scanProgress[language].Files
		}
		model.runtime.scanProgress[language] = progress
	}
	for _, file := range document.Files {
		done := len(file.PendingComponents) == 0
		previous, exists := model.runtime.scanFiles[file.Path]
		if exists && previous {
			model.runtime.scanFinished--
		}
		if done {
			model.runtime.scanFinished++
		}
		model.runtime.scanFiles[file.Path] = done
	}
	total := 0
	for _, progress := range model.runtime.scanProgress {
		total += progress.Files
	}
	model.runtime.scanTotal = max(total, len(model.runtime.scanFiles))
}

func scanStatus(model Model) (string, string) {
	languages := make([]string, 0, len(model.runtime.scanProgress))
	for language := range model.runtime.scanProgress {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	stages := make([]string, 0, len(languages))
	for _, language := range languages {
		progress := model.runtime.scanProgress[language]
		text := language + ": " + strings.ReplaceAll(progress.Stage, "_", " ")
		if progress.Total > 0 {
			text += fmt.Sprintf(" %d/%d", progress.Completed, progress.Total)
		} else if progress.Completed > 0 {
			text += fmt.Sprintf(" %d", progress.Completed)
		}
		stages = append(stages, text)
	}
	return scanningFrame(model.animationFrame) + " " + strings.Join(stages, " · "), fmt.Sprintf("%d/%d rated · %d pending", model.runtime.scanFinished, model.runtime.scanTotal, max(0, model.runtime.scanTotal-model.runtime.scanFinished))
}

func metricPending(file report.File, key string) bool {
	if len(file.PendingComponents) == 0 {
		return false
	}
	if key == "score" {
		return true
	}
	definition, known := scoring.MetricDefinitionByID(scoring.MetricID(key))
	if !known {
		return false
	}
	for _, id := range file.PendingComponents {
		if id == definition.ComponentID {
			return true
		}
		if definition.Aggregation == scoring.AggregationAxis {
			for _, component := range scoring.Components() {
				if component.ID == id && component.Axis == definition.Axis {
					return true
				}
			}
		}
	}
	return false
}
