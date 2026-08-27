// Package analysiscache persists verified analyzer inputs and results.
//
// The package deliberately separates the small, eagerly loaded display
// projection from lossless per-unit reports. Stored values are immutable and
// are addressed by SHA-256 digests; a workspace manifest is the only mutable
// pointer.
package analysiscache

import (
	"fmt"
	"time"

	"github.com/blater/slopmochi/internal/report"
)

const (
	storeSchemaVersion      = 1
	projectionSchemaVersion = 1
	unitSchemaVersion       = 1
	manifestSchemaVersion   = 1
)

// Key is a lowercase hexadecimal SHA-256 digest used as a stable cache key.
type Key string

// ViewKey identifies one workspace plus its file-membership-affecting scope.
// It is an alias of Key so unit and view identities share validation helpers.
type ViewKey = Key

// Digest identifies immutable content in the store.
type Digest string

// ArtifactRef points at an immutable, checksummed artifact envelope.
type ArtifactRef struct {
	Digest Digest `json:"digest"`
}

// Freshness is report.Freshness, re-exported so cache clients do not need a
// conversion when publishing a projection.
type Freshness = report.Freshness

const (
	FreshnessProvisional = report.FreshnessProvisional
	FreshnessVerifying   = report.FreshnessVerifying
	FreshnessRefreshing  = report.FreshnessRefreshing
	FreshnessCurrent     = report.FreshnessCurrent
	FreshnessStaleError  = report.FreshnessStaleError
	FreshnessRemoved     = report.Freshness("removed")
)

// DisplayProjection is the compact, eager startup representation of a report.
// Detailed evidence, waivers, diagnostics, and execution plans remain in the
// corresponding UnitArtifact values and can be loaded lazily.
type DisplayProjection struct {
	ViewKey     ViewKey       `json:"view_key"`
	GeneratedAt time.Time     `json:"generated_at"`
	Files       []DisplayFile `json:"files"`
}

// DisplayFile contains the fields needed to score, sort, and render a report
// row. Components retain aggregate subjects but omit evidence and waivers.
type DisplayFile struct {
	Axes          map[string]float64          `json:"axes"`
	Complete      bool                        `json:"complete"`
	Components    map[string]DisplayComponent `json:"components"`
	Coverage      map[string]string           `json:"coverage"`
	Language      string                      `json:"language"`
	ObservedAxes  map[string]float64          `json:"observed_axes"`
	ObservedScore float64                     `json:"observed_score"`
	Passed        *bool                       `json:"passed,omitempty"`
	Path          string                      `json:"path"`
	Rank          int                         `json:"rank"`
	Score         float64                     `json:"score"`
	ValidZero     bool                        `json:"valid_zero_score"`
	Freshness     Freshness                   `json:"freshness"`
	FreshnessNote string                      `json:"freshness_note,omitempty"`
}

// DisplayComponent is the evidence-free representation of a report component.
type DisplayComponent struct {
	Axis                     string                       `json:"axis,omitempty"`
	Contribution             float64                      `json:"contribution"`
	DeduplicatedObservations int                          `json:"deduplicated_observations"`
	Observations             int                          `json:"observations"`
	ObservedContribution     float64                      `json:"observed_contribution"`
	Subjects                 []report.SubjectContribution `json:"subjects"`
}

// UnitArtifact is the lossless result for one analyzer-owned unit. Report
// contains raw component subjects and evidence, coverage, diagnostics, and
// execution plans in the application's existing wire-compatible schema.
type UnitArtifact struct {
	UnitID      string          `json:"unit_id"`
	UnitKey     Key             `json:"unit_key"`
	Language    string          `json:"language"`
	SnapshotKey Key             `json:"snapshot_key"`
	Report      report.Document `json:"report"`
}

// Generation is a complete workspace view. Commits replace the current view
// atomically; individual referenced artifacts remain immutable.
type Generation struct {
	ViewKey    ViewKey             `json:"view_key"`
	Number     uint64              `json:"number"`
	CreatedAt  time.Time           `json:"created_at"`
	Projection ArtifactRef         `json:"projection"`
	Units      map[Key]ArtifactRef `json:"units"`
}

// ProjectionFromReport strips heavyweight detail while preserving the values
// used by current report scoring and table rendering.
func ProjectionFromReport(viewKey ViewKey, document report.Document, freshness Freshness) DisplayProjection {
	files := make([]DisplayFile, len(document.Files))
	for index, file := range document.Files {
		components := make(map[string]DisplayComponent, len(file.Components))
		for id, component := range file.Components {
			components[id] = DisplayComponent{
				Axis:                     component.Axis,
				Contribution:             component.Contribution,
				DeduplicatedObservations: component.DeduplicatedObservations,
				Observations:             component.Observations,
				ObservedContribution:     component.ObservedContribution,
				Subjects:                 append([]report.SubjectContribution(nil), component.Subjects...),
			}
		}
		files[index] = DisplayFile{
			Axes: cloneFloatMap(file.Axes), Complete: file.Complete,
			Components: components, Coverage: cloneStringMap(file.Coverage),
			Language: file.Language, ObservedAxes: cloneFloatMap(file.ObservedAxes),
			ObservedScore: file.ObservedScore, Passed: cloneBool(file.Passed),
			Path: file.Path, Rank: file.Rank, Score: file.Score,
			ValidZero: file.ValidZero, Freshness: freshness, FreshnessNote: file.FreshnessNote,
		}
	}
	return DisplayProjection{ViewKey: viewKey, GeneratedAt: time.Now().UTC(), Files: files}
}

// ReportFiles reconstructs report-compatible files from a projection. Evidence
// and waivers are intentionally empty and should be fetched from UnitArtifact
// for a detail view.
func (projection DisplayProjection) ReportFiles() []report.File {
	files := make([]report.File, len(projection.Files))
	for index, file := range projection.Files {
		components := make(map[string]report.Component, len(file.Components))
		for id, component := range file.Components {
			components[id] = report.Component{
				Axis:                     component.Axis,
				Contribution:             component.Contribution,
				DeduplicatedObservations: component.DeduplicatedObservations,
				Observations:             component.Observations,
				ObservedContribution:     component.ObservedContribution,
				Subjects:                 append([]report.SubjectContribution(nil), component.Subjects...),
			}
		}
		files[index] = report.File{
			Axes: cloneFloatMap(file.Axes), Complete: file.Complete,
			Components: components, Coverage: cloneStringMap(file.Coverage),
			Language: file.Language, ObservedAxes: cloneFloatMap(file.ObservedAxes),
			ObservedScore: file.ObservedScore, Passed: cloneBool(file.Passed),
			Path: file.Path, Rank: file.Rank, Score: file.Score,
			ValidZero: file.ValidZero, Freshness: file.Freshness, FreshnessNote: file.FreshnessNote,
		}
	}
	return files
}

func validateFreshness(value Freshness) error {
	switch value {
	case FreshnessProvisional, FreshnessVerifying, FreshnessRefreshing, FreshnessCurrent, FreshnessStaleError, FreshnessRemoved:
		return nil
	default:
		return fmt.Errorf("unknown freshness %q", value)
	}
}

func cloneFloatMap(source map[string]float64) map[string]float64 {
	if source == nil {
		return nil
	}
	result := make(map[string]float64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
