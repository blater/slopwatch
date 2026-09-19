package scoring

import (
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestV4ShallowUsesRawMaximumAndKeepsNullableStates(t *testing.T) {
	rawMaximum := 70.0
	file := report.File{Components: map[string]report.Component{
		"module_shallowness": {
			Contribution: 9,
			DepthVersion: "responsibility-burden-v4",
			DepthState:   "measured",
			RawMaximum:   &rawMaximum,
			Subjects: []report.SubjectContribution{
				{Subject: "first", Value: 70}, {Subject: "second", Value: 30},
			},
		},
	}}
	value := Metric(file, "deep")
	if !value.Available || value.Value != 70 || value.Contribution != 9 || value.State != "measured" {
		t.Fatalf("v4 metric = %#v, want raw maximum 70 and contribution 9", value)
	}

	legacy := report.File{Components: map[string]report.Component{
		"module_shallowness": {Contribution: 9, Subjects: file.Components["module_shallowness"].Subjects},
	}}
	if value := Metric(legacy, "deep"); !value.Available || value.Value != 100 || value.Contribution != 9 || value.State != "" {
		t.Fatalf("legacy metric = %#v, want sum 100 and contribution 9", value)
	}

	for _, state := range []string{"partial", "unavailable", "not_applicable"} {
		component := file.Components["module_shallowness"]
		component.DepthState = state
		component.RawMaximum = nil
		file.Components["module_shallowness"] = component
		value := Metric(file, "deep")
		if value.Available || value.Value != 0 || value.State != state {
			t.Fatalf("%s metric = %#v, want unavailable nullable state", state, value)
		}
	}
	partialMaximum := 70.0
	partial := file.Components["module_shallowness"]
	partial.DepthState, partial.RawMaximum = "partial", &partialMaximum
	file.Components["module_shallowness"] = partial
	if value := Metric(file, "deep"); !value.Available || value.Value != 70 || value.State != "partial" || value.Contribution != 9 {
		t.Fatalf("partial numeric v4 metric = %#v, want available provisional value", value)
	}
}
