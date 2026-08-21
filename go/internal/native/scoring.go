package native

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/slopslap/slopslap/internal/report"
)

type observation struct {
	component  string
	path       string
	language   string
	value      float64
	subject    protocolSubject
	attributes map[string]any
}

func number(value any) (float64, error) {
	switch value := value.(type) {
	case json.Number:
		return strconv.ParseFloat(value.String(), 64)
	case float64:
		return value, nil
	case string:
		return strconv.ParseFloat(value, 64)
	case map[string]any:
		tagged, ok := value["$integer"]
		if !ok {
			return 0, fmt.Errorf("invalid tagged number")
		}
		return strconv.ParseFloat(fmt.Sprint(tagged), 64)
	default:
		return 0, fmt.Errorf("unsupported number %T", value)
	}
}

func attributeNumber(attributes map[string]any, key string) (float64, error) {
	value, ok := attributes[key]
	if !ok {
		return 0, fmt.Errorf("compound measurement requires numeric attribute %q", key)
	}
	return number(value)
}

func roundScore(value float64) float64 {
	if value == 0 {
		return 0
	}
	return math.Round(value*1e12) / 1e12
}

func scalarContribution(formula string, value, threshold, weight float64, hasThreshold bool) (float64, error) {
	result := 0.0
	switch formula {
	case "binary":
		if value > 0 {
			result = weight
		}
	case "count":
		result = weight * value
	case "log-count":
		result = weight * math.Log2(1+value)
	case "linear-over-threshold", "log-ratio":
		if !hasThreshold || threshold <= 0 {
			return 0, fmt.Errorf("formula %s requires a threshold", formula)
		}
		if value >= threshold {
			if formula == "linear-over-threshold" {
				result = weight * value / threshold
			} else {
				result = weight * (1 + math.Log2(value/threshold))
			}
		}
	default:
		return 0, fmt.Errorf("unknown formula %s", formula)
	}
	return roundScore(result), nil
}

func godContribution(item observation, weight float64) (float64, error) {
	wmc, err := attributeNumber(item.attributes, "wmc")
	if err != nil {
		return 0, err
	}
	atfd, err := attributeNumber(item.attributes, "atfd")
	if err != nil {
		return 0, err
	}
	tcc, err := attributeNumber(item.attributes, "tcc_percent")
	if err != nil {
		tcc, err = attributeNumber(item.attributes, "tcc")
		tcc *= 100
	}
	if err != nil {
		return 0, err
	}
	threshold := 100.0 / 3.0
	if wmc < 47 || atfd <= 5 || tcc >= threshold {
		return 0, nil
	}
	atfdSeverity := 1 + math.Log2(math.Max(atfd, 5)/5)
	boundedTCC := math.Max(1, math.Min(tcc, threshold))
	tccSeverity := 1 + math.Log2(threshold/boundedTCC)
	return roundScore(weight * (30 + 8*atfdSeverity + 8*tccSeverity)), nil
}

func subjectKey(subject protocolSubject) string {
	symbol := subject.Symbol
	if symbol == "" {
		symbol = subject.Name
	}
	return fmt.Sprintf("%s@%d:%d-%d:%d", symbol, subject.Line, subject.Column, subject.EndLine, subject.EndColumn)
}

func scoreRecords(catalog catalogDocument, selected []string, records []protocolRecord, passScore *float64) (report.Document, error) {
	descriptors := map[string]componentDescriptor{}
	for _, descriptor := range catalog.Components {
		descriptors[descriptor.ID] = descriptor
	}
	observations := map[string]map[string][]observation{}
	coverage := map[string]map[string]string{}
	languages := map[string]string{}
	diagnostics := []map[string]any{}
	plans := []map[string]any{}
	for _, record := range records {
		switch record.Type {
		case "measurement":
			if record.Path == nil {
				return report.Document{}, fmt.Errorf("measurement has no path")
			}
			value, err := number(record.Value)
			if err != nil {
				return report.Document{}, err
			}
			item := observation{record.Component, *record.Path, record.Language, value, record.Subject, record.Attributes}
			if observations[item.path] == nil {
				observations[item.path] = map[string][]observation{}
			}
			observations[item.path][item.component] = append(observations[item.path][item.component], item)
			languages[item.path] = item.language
		case "coverage":
			if record.Path == nil {
				return report.Document{}, fmt.Errorf("coverage has no path")
			}
			if coverage[*record.Path] == nil {
				coverage[*record.Path] = map[string]string{}
			}
			coverage[*record.Path][record.Component] = record.State
			if languages[*record.Path] == "" {
				languages[*record.Path] = strings.TrimSuffix(record.UnitID, "-unit")
			}
		case "diagnostic":
			diagnostics = append(diagnostics, record.Raw)
		case "execution_plan":
			plans = append(plans, record.Raw)
		}
	}
	paths := make([]string, 0, len(coverage))
	for path := range coverage {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	files := make([]report.File, 0, len(paths))
	selectedSet := map[string]bool{}
	for _, language := range selected {
		selectedSet[language] = true
	}
	for _, path := range paths {
		language := languages[path]
		if !selectedSet[language] {
			continue
		}
		file := report.File{Path: path, Language: language, Complete: true, Components: map[string]report.Component{}, Coverage: map[string]string{}, Axes: map[string]float64{}, ObservedAxes: map[string]float64{}}
		for _, descriptor := range catalog.Components {
			if !descriptor.Defaults.Enabled || !descriptor.supported(language) {
				continue
			}
			state, ok := coverage[path][descriptor.ID]
			if !ok {
				state = "not_requested"
			}
			file.Coverage[descriptor.ID] = state
			if state != "complete" {
				file.Complete = false
			}
			weight, err := descriptor.Defaults.weight()
			if err != nil {
				return report.Document{}, err
			}
			threshold, hasThreshold, err := descriptor.Defaults.threshold()
			if err != nil {
				return report.Document{}, err
			}
			raw := observations[path][descriptor.ID]
			component := report.Component{Observations: len(raw), DeduplicatedObservations: len(raw), Subjects: []report.SubjectContribution{}, Waivers: []map[string]any{}}
			if state == "complete" {
				if descriptor.Kind == "count" {
					value := float64(len(raw))
					contribution, err := scalarContribution(descriptor.Defaults.Formula, value, threshold, weight, hasThreshold)
					if err != nil {
						return report.Document{}, err
					}
					component.Contribution = contribution
					component.ObservedContribution = contribution
					if len(raw) > 0 {
						component.Subjects = append(component.Subjects, report.SubjectContribution{Subject: "deduplicated_count", Value: value, Contribution: contribution})
					}
				} else {
					values := make([]float64, 0, len(raw))
					for _, item := range raw {
						contribution := 0.0
						if descriptor.Kind == "compound" {
							contribution, err = godContribution(item, weight)
						} else {
							contribution, err = scalarContribution(descriptor.Defaults.Formula, item.value, threshold, weight, hasThreshold)
						}
						if err != nil {
							return report.Document{}, err
						}
						values = append(values, contribution)
						component.Subjects = append(component.Subjects, report.SubjectContribution{Subject: subjectKey(item.subject), Value: item.value, Contribution: contribution})
					}
					if descriptor.Aggregator == "max" {
						for _, value := range values {
							component.Contribution = math.Max(component.Contribution, value)
						}
					} else {
						for _, value := range values {
							component.Contribution += value
						}
					}
					component.Contribution = roundScore(component.Contribution)
					component.ObservedContribution = component.Contribution
				}
			}
			file.Components[descriptor.ID] = component
			file.Axes[descriptor.Axis] = roundScore(file.Axes[descriptor.Axis] + component.Contribution)
			file.ObservedAxes[descriptor.Axis] = file.Axes[descriptor.Axis]
		}
		for _, value := range file.Axes {
			file.Score += value
		}
		file.Score = roundScore(file.Score)
		file.ObservedScore = file.Score
		file.ValidZero = file.Complete && file.Score == 0
		if passScore != nil {
			passed := file.Complete && file.Score <= *passScore
			file.Passed = &passed
		}
		files = append(files, file)
	}
	document := report.Document{Calibrated: true, Configuration: nil, Diagnostics: diagnostics, ExecutionPlans: plans, Files: files, ProfileSetHash: "native-balanced-v1", SchemaVersion: 3, Summary: map[string]any{}}
	document.SortAndRank()
	complete := 0
	passed := true
	failed := 0
	for _, file := range document.Files {
		if file.Complete {
			complete++
		}
		if file.Passed != nil && !*file.Passed {
			passed = false
			failed++
		}
	}
	document.Summary["complete_files"] = complete
	if passScore != nil {
		document.Summary["pass_score"] = *passScore
		document.Summary["passed"] = passed
		document.Summary["failed_files"] = failed
	}
	return document, nil
}
