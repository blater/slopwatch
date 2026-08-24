package report

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type Document struct {
	Calibrated     bool             `json:"calibrated"`
	Configuration  any              `json:"configuration"`
	Diagnostics    []map[string]any `json:"diagnostics"`
	ExecutionPlans []map[string]any `json:"execution_plans"`
	Files          []File           `json:"files"`
	ProfileSetHash string           `json:"profile_set_hash"`
	ReturnedFiles  int              `json:"returned_files"`
	SchemaVersion  int              `json:"schema_version"`
	Summary        map[string]any   `json:"summary"`
	Truncated      bool             `json:"truncated"`
}

type File struct {
	Axes          map[string]float64   `json:"axes"`
	Complete      bool                 `json:"complete"`
	Components    map[string]Component `json:"components"`
	Coverage      map[string]string    `json:"coverage"`
	Language      string               `json:"language"`
	ObservedAxes  map[string]float64   `json:"observed_axes"`
	ObservedScore float64              `json:"observed_score"`
	Passed        *bool                `json:"passed,omitempty"`
	Path          string               `json:"path"`
	Rank          int                  `json:"rank"`
	Score         float64              `json:"score"`
	ValidZero     bool                 `json:"valid_zero_score"`
	Freshness     Freshness            `json:"freshness,omitempty"`
	FreshnessNote string               `json:"freshness_note,omitempty"`
}

// Freshness describes how closely a displayed result is known to match the
// current workspace. Empty is equivalent to Current for uncached analyses.
type Freshness string

const (
	FreshnessProvisional Freshness = "provisional"
	FreshnessVerifying   Freshness = "verifying"
	FreshnessRefreshing  Freshness = "refreshing"
	FreshnessCurrent     Freshness = "current"
	FreshnessStaleError  Freshness = "stale_error"
)

type Component struct {
	Axis                     string                `json:"axis,omitempty"`
	Contribution             float64               `json:"contribution"`
	DeduplicatedObservations int                   `json:"deduplicated_observations"`
	Evidence                 []MeasurementEvidence `json:"evidence,omitempty"`
	Observations             int                   `json:"observations"`
	ObservedContribution     float64               `json:"observed_contribution"`
	Subjects                 []SubjectContribution `json:"subjects"`
	Waivers                  []map[string]any      `json:"waivers"`
}

type SourcePosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset,omitempty"`
}

type SourceRange struct {
	Path  string         `json:"path"`
	Start SourcePosition `json:"start"`
	End   SourcePosition `json:"end"`
}

type MeasurementEvidence struct {
	Name       string         `json:"name"`
	Symbol     string         `json:"symbol"`
	Routine    string         `json:"routine,omitempty"`
	Scope      string         `json:"scope"`
	Value      float64        `json:"value"`
	Location   SourceRange    `json:"location"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Provenance map[string]any `json:"provenance,omitempty"`
}

type SubjectContribution struct {
	Contribution float64 `json:"contribution"`
	Subject      string  `json:"subject"`
	Value        float64 `json:"value"`
}

func Decode(data []byte) (Document, error) {
	var document Document
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode analysis report: %w", err)
	}
	return document, nil
}

func (document *Document) SortAndRank() {
	sort.SliceStable(document.Files, func(i, j int) bool {
		if document.Files[i].Score != document.Files[j].Score {
			return document.Files[i].Score > document.Files[j].Score
		}
		return document.Files[i].Path < document.Files[j].Path
	})
	for index := range document.Files {
		document.Files[index].Rank = index + 1
	}
	document.ReturnedFiles = len(document.Files)
	if document.Summary == nil {
		document.Summary = map[string]any{}
	}
	document.Summary["files_analyzed"] = len(document.Files)
}

func DisplayNumber(value float64) string {
	if math.Trunc(value) == value {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'g', 6, 64)
}

func OneDecimal(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}

func Max(file File, componentID string) (float64, bool) {
	component, ok := file.Components[componentID]
	if !ok {
		return 0, false
	}
	maximum := 0.0
	for _, subject := range component.Subjects {
		maximum = math.Max(maximum, subject.Value)
	}
	return maximum, true
}

func Sum(file File, componentID string) (float64, bool) {
	component, ok := file.Components[componentID]
	if !ok {
		return 0, false
	}
	total := 0.0
	for _, subject := range component.Subjects {
		total += subject.Value
	}
	return total, true
}

func Contribution(file File, componentID string) (float64, bool) {
	component, ok := file.Components[componentID]
	return component.Contribution, ok
}
