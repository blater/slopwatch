package nativeadapter

import (
	"errors"
	"fmt"
	"math"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/scoring"
)

func validateGoal(goal fix.ScoringGoal) error {
	if math.IsNaN(goal.MaximumScore) || math.IsInf(goal.MaximumScore, 0) || goal.MaximumScore < 0 {
		return errors.New("maximum score must be finite and non-negative")
	}
	seen := make(map[fix.MetricID]struct{}, len(goal.Focus))
	for _, item := range goal.Focus {
		if _, exists := scoring.MetricDefinitionByID(scoring.MetricID(item.Metric)); !exists && item.Metric != fix.MetricScore {
			return fmt.Errorf("unknown focus metric %q", item.Metric)
		}
		if math.IsNaN(item.Maximum) || math.IsInf(item.Maximum, 0) || item.Maximum < 0 {
			return fmt.Errorf("focus metric %q maximum must be finite and non-negative", item.Metric)
		}
		if _, duplicate := seen[item.Metric]; duplicate {
			return fmt.Errorf("duplicate focus metric %q", item.Metric)
		}
		seen[item.Metric] = struct{}{}
	}
	for metric, allowance := range goal.AllowedRegression {
		if _, exists := scoring.MetricDefinitionByID(scoring.MetricID(metric)); !exists {
			return fmt.Errorf("unknown regression metric %q", metric)
		}
		if math.IsNaN(allowance) || math.IsInf(allowance, 0) || allowance < 0 {
			return fmt.Errorf("regression allowance for %q must be finite and non-negative", metric)
		}
	}
	return nil
}
