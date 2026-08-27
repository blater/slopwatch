package scoring

import (
	"testing"

	"github.com/blater/slopmochi/internal/report"
)

func TestMetricAggregationsPreserveDashboardSemantics(t *testing.T) {
	file := report.File{
		Axes: map[string]float64{"typescript_type_safety": 12},
		Components: map[string]report.Component{
			"cognitive_complexity":         metricComponent(11, 3, 7),
			"npath_complexity":             metricComponent(8, 80),
			"cyclomatic_method_complexity": metricComponent(5, 2, 4),
			"module_shallowness":           metricComponent(5, 20, 30),
			"god_class":                    metricComponent(42, 1),
			"coupling_between_objects":     metricComponent(10, 2, 7),
			"deeply_nested_if":             metricComponent(6, 2),
		},
	}
	tests := []struct {
		key          string
		value        float64
		contribution float64
	}{
		{"cog", 7, 11},
		{"npath", 80, 8},
		{"cyclo", 4, 5},
		{"deep", 50, 5},
		{"god", 42, 42},
		{"coupling", 7, 10},
		{"nesting", 6, 6},
		{"typesafety", 12, 12},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			metric := Metric(file, test.key)
			if !metric.Available || metric.Value != test.value || metric.Contribution != test.contribution {
				t.Fatalf("metric = %#v, want value %v contribution %v", metric, test.value, test.contribution)
			}
		})
	}

	if metric := Metric(file, "missing"); metric.Available || metric.Value != 0 || metric.Contribution != 0 {
		t.Fatalf("missing metric = %#v", metric)
	}
}

func TestMetricCatalogDefinesEveryStableVerifierExpression(t *testing.T) {
	definitions := Metrics()
	if len(definitions) != 8 {
		t.Fatalf("metric count = %d, want 8", len(definitions))
	}
	want := map[MetricID]Aggregation{
		MetricCognitive: AggregationMaximum, MetricNPath: AggregationMaximum,
		MetricCyclomatic: AggregationMaximum, MetricShallowness: AggregationSum,
		MetricGodClass: AggregationContribution, MetricCoupling: AggregationMaximum,
		MetricNesting: AggregationContribution, MetricTypeSafety: AggregationAxis,
	}
	for _, definition := range definitions {
		if want[definition.ID] != definition.Aggregation {
			t.Errorf("metric %s aggregation = %s, want %s", definition.ID, definition.Aggregation, want[definition.ID])
		}
		delete(want, definition.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing metric definitions: %#v", want)
	}

	definitions[0].Aggregation = AggregationAxis
	definition, _ := MetricDefinitionByID(MetricCognitive)
	if definition.Aggregation != AggregationMaximum {
		t.Fatal("caller mutated metric catalog")
	}
}

func metricComponent(contribution float64, values ...float64) report.Component {
	subjects := make([]report.SubjectContribution, 0, len(values))
	for _, value := range values {
		subjects = append(subjects, report.SubjectContribution{Value: value})
	}
	return report.Component{Contribution: contribution, Subjects: subjects}
}
