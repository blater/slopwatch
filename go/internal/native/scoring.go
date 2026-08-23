package native

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/blater/slopwatch/internal/report"
)

type observation struct {
	component  string
	path       string
	language   string
	scope      string
	value      float64
	subject    protocolSubject
	attributes map[string]any
	provenance map[string]any
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
	start, end := subjectPositions(subject)
	return fmt.Sprintf("%s@%d:%d-%d:%d", symbol, start.Line, start.Column, end.Line, end.Column)
}

func subjectPositions(subject protocolSubject) (protocolPosition, protocolPosition) {
	start, end := subject.Start, subject.End
	if start.Line == 0 {
		start.Line, start.Column = subject.Line, subject.Column
	}
	if end.Line == 0 {
		end.Line, end.Column = subject.EndLine, subject.EndColumn
	}
	return start, end
}

func scoreRecords(catalog catalogDocument, selected []string, records []protocolRecord, passScore *float64) (report.Document, error) {
	descriptors := map[string]componentDescriptor{}
	for _, descriptor := range catalog.Components {
		descriptors[descriptor.ID] = descriptor
	}
	inputs, err := collectScoreInputs(records)
	if err != nil {
		return report.Document{}, err
	}
	observations, coverage, languages := inputs.observations, inputs.coverage, inputs.languages
	diagnostics, plans := inputs.diagnostics, inputs.plans
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
		file, err := scoreFile(path, language, catalog.Components, observations, coverage, passScore)
		if err != nil {
			return report.Document{}, err
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
