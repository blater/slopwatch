package scoring

import (
	"math"
	"testing"
)

func TestContinuousLogSeverityUsesBaselineAndReference(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		want      float64
		baseline  float64
		reference float64
	}{
		{name: "below baseline", value: 0, baseline: 1, reference: 10, want: 0},
		{name: "baseline", value: 1, baseline: 1, reference: 10, want: 0},
		{name: "reference", value: 10, baseline: 1, reference: 10, want: 1},
		{name: "cognitive zero", value: 0, baseline: 0, reference: 15, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ScalarSeverity("continuous-log", test.value, test.baseline, test.reference)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(got-test.want) > 1e-12 {
				t.Fatalf("severity = %.15f, want %.15f", got, test.want)
			}
		})
	}
}

func TestContinuousLogSeverityIsMonotoneAndFinite(t *testing.T) {
	values := []float64{0, 1, 2, 9, 10, 11, 100, 1e300}
	previous := -1.0
	for _, value := range values {
		severity, err := ScalarSeverity("continuous-log", value, 1, 10)
		if err != nil {
			t.Fatalf("value %g: %v", value, err)
		}
		if math.IsNaN(severity) || math.IsInf(severity, 0) || severity < previous {
			t.Fatalf("value %g: severity %g after %g", value, severity, previous)
		}
		previous = severity
	}
	justBelow, err := ScalarSeverity("continuous-log", math.Nextafter(10, 0), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	atReference, err := ScalarSeverity("continuous-log", 10, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	justAbove, err := ScalarSeverity("continuous-log", math.Nextafter(10, math.Inf(1)), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if atReference < justBelow || justAbove < atReference || justAbove-justBelow > 1e-12 {
		t.Fatalf("reference discontinuity: below=%g at=%g above=%g", justBelow, atReference, justAbove)
	}
}

func TestContinuousLogSeverityRejectsInvalidParameters(t *testing.T) {
	cases := []struct {
		name      string
		value     float64
		baseline  float64
		reference float64
	}{
		{name: "equal reference", value: 2, baseline: 2, reference: 2},
		{name: "reversed reference", value: 2, baseline: 3, reference: 2},
		{name: "nan baseline", value: 2, baseline: math.NaN(), reference: 10},
		{name: "infinite reference", value: 2, baseline: 0, reference: math.Inf(1)},
		{name: "nan value", value: math.NaN(), baseline: 0, reference: 10},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ScalarSeverity("continuous-log", test.value, test.baseline, test.reference); err == nil {
				t.Fatal("expected invalid parameters to be rejected")
			}
		})
	}
}

func TestScalarSeverityPreservesExistingFormulas(t *testing.T) {
	if got, err := ScalarSeverity("log-ratio", 20, 0, 10); err != nil || math.Abs(got-2) > 1e-12 {
		t.Fatalf("log-ratio = %v, %v", got, err)
	}
	if got, err := ScalarSeverity("count", 3, 0, 0); err != nil || got != 3 {
		t.Fatalf("count = %v, %v", got, err)
	}
}
