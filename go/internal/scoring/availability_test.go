package scoring

import (
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestPartialShallowKeepsAggregateScoreAvailable(t *testing.T) {
	original := report.File{
		Complete: false,
		Components: map[string]report.Component{
			"cognitive_complexity": {Contribution: 10},
			"module_shallowness": {
				DepthVersion: "responsibility-burden-v4",
				DepthState:   "partial",
			},
		},
		Coverage: map[string]string{
			"cognitive_complexity": "complete",
			"module_shallowness":   "partial",
		},
	}
	projected := ProjectFile(original, NewPolicy(nil, nil))
	if projected.Score != 10 || !ScoreAvailable(projected) || projected.ValidZero {
		t.Fatalf("partial projection = score %v available %t valid zero %t", projected.Score, ScoreAvailable(projected), projected.ValidZero)
	}
}

func TestNumericPartialShallowRemainsAggregateAvailable(t *testing.T) {
	value := 70.0
	file := report.File{
		Complete: false,
		Components: map[string]report.Component{
			"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "partial", RawMaximum: &value, Contribution: 4},
		},
		Coverage: map[string]string{"module_shallowness": "failed"},
	}
	if !ScoreAvailable(file) {
		t.Fatal("finite partial SHALLOW value was hidden by failed coverage")
	}
}

func TestFailedComponentsDoNotCreateZeroAggregate(t *testing.T) {
	file := report.File{
		Complete: false,
		Components: map[string]report.Component{
			"cognitive_complexity": {},
			"module_shallowness": {
				DepthVersion: "responsibility-burden-v4",
				DepthState:   "unavailable",
			},
		},
		Coverage: map[string]string{
			"cognitive_complexity": "failed",
			"module_shallowness":   "unavailable",
		},
	}
	if ScoreAvailable(file) {
		t.Fatal("failed components were treated as an available aggregate")
	}
}

func TestCompleteNotApplicableShallowKeepsZeroAggregateNumeric(t *testing.T) {
	file := report.File{
		Complete: true,
		Components: map[string]report.Component{
			"module_shallowness": {
				DepthVersion: "responsibility-burden-v4",
				DepthState:   "not_applicable",
			},
		},
		Coverage: map[string]string{"module_shallowness": "complete"},
	}
	if !ScoreAvailable(file) {
		t.Fatal("complete not-applicable aggregate was marked unavailable")
	}
}
