package main

import (
	"github.com/blater/slopwatch/internal/report"
	"testing"
)

func TestDepthTableKeepsAvailablePackageAndContractRatings(t *testing.T) {
	for _, test := range []struct {
		scope string
		value float64
		want  string
	}{{"package", 28, "28"}, {"file", 0, "0"}} {
		file := report.File{Coverage: map[string]string{"module_shallowness": "unavailable"}, Components: map[string]report.Component{"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "partial", DepthScope: test.scope, RawMaximum: &test.value}}}
		if got := depth(file); got != test.want {
			t.Fatalf("got %q want %q", got, test.want)
		}
	}
}
