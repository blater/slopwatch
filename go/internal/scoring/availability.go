package scoring

import "github.com/blater/slopwatch/internal/report"

// ScoreAvailable reports whether SCORE has an eligible measured component, or
// a complete successful not-applicable SHALLOW result. File.Complete also
// describes failed or partial work, so it is only consulted for metadata-free
// legacy reports and the successful not-applicable case.
func ScoreAvailable(file report.File) bool {
	if len(file.Components) == 0 && len(file.Coverage) == 0 {
		// Pre-v4 in-memory and legacy reports sometimes persist only the
		// already aggregated score. Preserve that representation while still
		// rejecting an explicitly incomplete report.
		return file.Complete
	}
	notApplicable := false
	for _, definition := range metricDefinitions {
		if metricAvailable(file, definition) {
			return true
		}
		if definition.ID == MetricShallowness && metricNotApplicable(file, definition) {
			notApplicable = true
		}
	}
	return notApplicable && file.Complete
}

func metricAvailable(file report.File, definition MetricDefinition) bool {
	if definition.Aggregation == AggregationAxis {
		return axisAvailable(file, definition.Axis)
	}
	value := Metric(file, string(definition.ID))
	if definition.ID == MetricShallowness {
		if component, exists := file.Components[definition.ComponentID]; exists && component.DepthVersion == "responsibility-burden-v4" {
			// A finite measured or estimated SHALLOW value remains reportable
			// when coverage is partial/failed; File.Complete and diagnostics retain
			// the semantic failure state.
			return value.Available
		}
	}
	return value.Available && componentAvailable(file, definition.ComponentID)
}

func axisAvailable(file report.File, axis string) bool {
	if _, exists := file.Axes[axis]; !exists {
		return false
	}
	known := false
	for _, component := range components {
		if component.Axis != axis {
			continue
		}
		known = true
		if componentAvailable(file, component.ID) {
			return true
		}
	}
	// Older in-memory reports may carry axes without their component map.
	return !known && len(file.Coverage) == 0
}

func componentAvailable(file report.File, id string) bool {
	if _, exists := file.Components[id]; !exists {
		return false
	}
	if len(file.Coverage) == 0 {
		return true
	}
	state, recorded := file.Coverage[id]
	return !recorded || state == "" || state == "complete"
}

func metricNotApplicable(file report.File, definition MetricDefinition) bool {
	if definition.ID != MetricShallowness {
		return false
	}
	return Metric(file, string(definition.ID)).State == "not_applicable" &&
		componentAvailable(file, definition.ComponentID)
}
