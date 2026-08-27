package scoring

import "github.com/blater/slopmochi/internal/report"

// MetricID is a stable verifier and dashboard metric identifier.
type MetricID string

const (
	MetricCognitive   MetricID = "cog"
	MetricNPath       MetricID = "npath"
	MetricCyclomatic  MetricID = "cyclo"
	MetricShallowness MetricID = "deep"
	MetricGodClass    MetricID = "god"
	MetricCoupling    MetricID = "coupling"
	MetricNesting     MetricID = "nesting"
	MetricTypeSafety  MetricID = "typesafety"
)

// Aggregation states exactly how a displayed metric value is obtained.
type Aggregation string

const (
	AggregationAxis         Aggregation = "axis"
	AggregationContribution Aggregation = "contribution"
	AggregationMaximum      Aggregation = "maximum_subject"
	AggregationSum          Aggregation = "sum_subjects"
)

// MetricDefinition is the stable contract shared by presentation and goal
// verification. Axis is used only by axis aggregation; ComponentID is used by
// component aggregations.
type MetricDefinition struct {
	ID          MetricID
	Axis        string
	ComponentID string
	Aggregation Aggregation
}

var metricDefinitions = []MetricDefinition{
	{ID: MetricCognitive, ComponentID: "cognitive_complexity", Aggregation: AggregationMaximum},
	{ID: MetricNPath, ComponentID: "npath_complexity", Aggregation: AggregationMaximum},
	{ID: MetricCyclomatic, ComponentID: "cyclomatic_method_complexity", Aggregation: AggregationMaximum},
	{ID: MetricShallowness, ComponentID: "module_shallowness", Aggregation: AggregationSum},
	{ID: MetricGodClass, ComponentID: "god_class", Aggregation: AggregationContribution},
	{ID: MetricCoupling, ComponentID: "coupling_between_objects", Aggregation: AggregationMaximum},
	{ID: MetricNesting, ComponentID: "deeply_nested_if", Aggregation: AggregationContribution},
	{ID: MetricTypeSafety, Axis: "typescript_type_safety", Aggregation: AggregationAxis},
}

var metricsByID = indexMetrics(metricDefinitions)

func indexMetrics(values []MetricDefinition) map[MetricID]MetricDefinition {
	result := make(map[MetricID]MetricDefinition, len(values))
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}

// Metrics returns metric definitions in stable dashboard order.
func Metrics() []MetricDefinition {
	return append([]MetricDefinition(nil), metricDefinitions...)
}

// MetricDefinitionByID returns the verifier expression for id.
func MetricDefinitionByID(id MetricID) (MetricDefinition, bool) {
	definition, ok := metricsByID[id]
	return definition, ok
}

// MetricValue is the display and verification projection of one metric.
// Contribution remains separate because several metrics display a raw
// aggregate while contributing a differently scaled value to SCORE.
type MetricValue struct {
	Value        float64
	Available    bool
	Contribution float64
}

// Metric evaluates a stable dashboard metric key for one file.
func Metric(file report.File, key string) MetricValue {
	definition, known := MetricDefinitionByID(MetricID(key))
	if !known {
		return MetricValue{}
	}
	if definition.Aggregation == AggregationAxis {
		value, exists := file.Axes[definition.Axis]
		return MetricValue{Value: value, Available: exists, Contribution: value}
	}
	component, exists := file.Components[definition.ComponentID]
	if !exists {
		return MetricValue{}
	}
	value := component.Contribution
	switch definition.Aggregation {
	case AggregationMaximum:
		value, _ = report.Max(file, definition.ComponentID)
	case AggregationSum:
		value, _ = report.Sum(file, definition.ComponentID)
	}
	return MetricValue{Value: value, Available: true, Contribution: component.Contribution}
}
