package native

import (
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestMergeDepthBoundaryRetainsShallowWhenPartialWinsEitherOrder(t *testing.T) {
	measured := report.DepthBoundary{ID: "shared", State: "measured", Shallow: floatPtr(31)}
	partial := report.DepthBoundary{ID: "shared", State: "partial", Shallow: floatPtr(31)}
	for _, test := range []struct {
		name   string
		first  report.DepthBoundary
		second report.DepthBoundary
	}{
		{"measured then partial", measured, partial},
		{"partial then measured", partial, measured},
	} {
		t.Run(test.name, func(t *testing.T) {
			merged := mergeDepthBoundary(test.first, test.second)
			if merged.State != "partial" || merged.Shallow == nil || *merged.Shallow != 31 {
				t.Fatalf("merged partial state = %#v", merged)
			}
		})
	}
}

func TestMergeDepthBoundaryClearsStaleShallowWhenUnavailableWins(t *testing.T) {
	merged := mergeDepthBoundary(
		report.DepthBoundary{ID: "shared", State: "unavailable", Shallow: floatPtr(31)},
		report.DepthBoundary{ID: "shared", State: "measured", Shallow: floatPtr(31)},
	)
	if merged.State != "unavailable" || merged.Shallow != nil {
		t.Fatalf("merged unavailable state = %#v", merged)
	}
}

func TestMergeDepthBoundaryConflictingMeasuredScoresRemainUnavailable(t *testing.T) {
	merged := mergeDepthBoundary(
		report.DepthBoundary{ID: "shared", State: "measured", Shallow: floatPtr(31)},
		report.DepthBoundary{ID: "shared", State: "measured", Shallow: floatPtr(47)},
	)
	if merged.State != "unavailable" || merged.Shallow != nil {
		t.Fatalf("conflicting measured state = %#v", merged)
	}
}

func floatPtr(value float64) *float64 { return &value }
