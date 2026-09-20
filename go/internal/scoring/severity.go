package scoring

import (
	"fmt"
	"math"
)

// ScalarSeverity evaluates a catalog scalar formula at unit weight. The
// returned value is the canonical, unrounded severity used by native scoring
// and policy projection. Keeping this value unweighted and unrounded makes
// repeated re-projection independent of the original catalog weight.
//
// continuous-log is the Plan 3 curve:
//
//	log2(1 + max(0, value-baseline)/(reference-baseline))
//
// The other formulas retain their pre-Plan 3 semantics. Baseline is ignored
// by those formulas so existing type, nesting, GOD and SHALLOW components do
// not change behavior.
func ScalarSeverity(formula string, value, baseline, reference float64) (float64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("formula %s requires a finite value", formula)
	}

	var result float64
	switch formula {
	case "binary":
		if value > 0 {
			result = 1
		}
	case "count":
		if value < 0 {
			return 0, fmt.Errorf("formula %s requires a nonnegative value", formula)
		}
		result = value
	case "log-count":
		if value < 0 {
			return 0, fmt.Errorf("formula %s requires a nonnegative value", formula)
		}
		result = math.Log2(1 + value)
	case "linear-over-threshold", "log-ratio":
		if !finitePositive(reference) {
			return 0, fmt.Errorf("formula %s requires a positive finite reference", formula)
		}
		if value >= reference {
			if formula == "linear-over-threshold" {
				result = value / reference
			} else {
				result = 1 + math.Log2(value/reference)
			}
		}
	case "continuous-log":
		if !finite(baseline) || !finite(reference) || reference <= baseline {
			return 0, fmt.Errorf("formula %s requires finite reference greater than baseline", formula)
		}
		excess := math.Max(0, value-baseline)
		// Checking the denominator separately keeps custom catalog parameters
		// from turning a valid input into an infinity through division.
		ratio := excess / (reference - baseline)
		if !finite(ratio) {
			return 0, fmt.Errorf("formula %s produced a non-finite ratio", formula)
		}
		result = math.Log2(1 + ratio)
	default:
		return 0, fmt.Errorf("unknown formula %s", formula)
	}
	if !finite(result) || result < 0 {
		return 0, fmt.Errorf("formula %s produced invalid severity", formula)
	}
	return result, nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finitePositive(value float64) bool {
	return finite(value) && value > 0
}
