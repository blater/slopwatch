package sourceestimate

import "math"

const UncertaintyPolicy = "graded-caller-responsibility-v1"

// Finding records demonstrated avoidable caller cost. Neither unknown behavior
// nor an unfavorable responsibility ratio can manufacture a finding.
type Finding struct {
	Kind              string   `json:"kind"`
	Operation         string   `json:"operation"`
	Parameter         string   `json:"parameter"`
	CallerFiles       []string `json:"caller_files"`
	UnnecessaryBurden float64  `json:"unnecessary_burden"`
}

type PenaltyAssessment struct {
	RecognizedHidden float64
	UncertainBurden  float64
	Ratio            float64
	Value            float64
}

func FindingPenalty(burden float64, findings []Finding) float64 {
	if burden <= 0 {
		return 0
	}
	adverse := 0.0
	seen := map[string]bool{}
	for _, f := range findings {
		key := f.Kind + "|" + f.Operation + "|" + f.Parameter
		if !seen[key] && f.UnnecessaryBurden > 0 {
			adverse += f.UnnecessaryBurden
			seen[key] = true
		}
	}
	return math.Round(100 * math.Min(burden, adverse) / burden)
}

func ResponsibilityRatio(burden, hidden float64) float64 {
	if burden <= 0 {
		return 0
	}
	return math.Round(100 * burden / (burden + 2*math.Max(0, hidden)))
}

func (r Result) RecognizedHidden() float64 {
	return math.Max(0, r.Hidden-r.Categories["unknown_call"]-r.Categories["unknown_outcome"])
}

func (r Result) Assessment() PenaltyAssessment {
	uncertain := r.UncertainBurden
	if uncertain == 0 && len(r.Limitations) > 0 {
		uncertain = r.Burden
	}
	value := 0.0
	if r.Grade != nil {
		value = r.Grade.Value()
	} else {
		residual := math.Max(0, r.Burden-1.25)
		value = math.Round(100 * residual / (1 + residual + 2*r.RecognizedHidden()))
	}
	return PenaltyAssessment{RecognizedHidden: r.RecognizedHidden(), UncertainBurden: uncertain, Ratio: ResponsibilityRatio(r.Burden, r.RecognizedHidden()), Value: value}
}

func (r Result) PublishedPenalty() float64 {
	if !r.Applicable || r.RoleOnly {
		return 0
	}
	return r.Assessment().Value
}
