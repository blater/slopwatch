package fixapp

import (
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestConfiguredFocusUsesIncompleteMeasurementsAndSkipsMissing(t *testing.T) {
	goals, err := configuredFocusGoals([]fix.MetricID{"score", "cog", "npath"}, []fix.TargetSnapshot{
		{Path: "missing.go"},
		{Path: "partial.go", Metrics: map[fix.MetricID]fix.MetricValue{"cog": {Value: 9, Complete: false}}},
		{Path: "complete.go", Metrics: map[fix.MetricID]fix.MetricValue{"cog": {Value: 7, Complete: true}}},
	}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(goals) != 2 || goals[0].Metric != "score" || goals[0].Maximum != 30 || goals[1].Metric != "cog" || goals[1].Maximum != 9 {
		t.Fatalf("goals = %#v", goals)
	}
}
