package sourceestimate

import "math"

// GradedEvidence separates the caller's observable contract from knowledge of
// its implementation. A simple operation with one input establishes the scale;
// it is not a fictitious unit of hidden responsibility.
type GradedEvidence struct {
	DenominatorReference     float64            `json:"denominator_reference,omitempty"`
	ResponsibilityMultiplier float64            `json:"responsibility_multiplier,omitempty"`
	Surface                  CallerSurface      `json:"surface"`
	ResidualBurden           float64            `json:"residual_burden"`
	SupportedBurden          float64            `json:"supported_burden"`
	Hidden                   float64            `json:"hidden_responsibility"`
	EstimatedHidden          float64            `json:"estimated_hidden_responsibility,omitempty"`
	Responsibilities         map[string]float64 `json:"responsibilities"`
	MaterialLimitations      []string           `json:"material_limitations,omitempty"`
	ZeroReason               string             `json:"zero_reason,omitempty"`
}

func (g *GradedEvidence) Value() float64 {
	if g == nil || g.ResidualBurden <= 0 {
		return 0
	}
	reference, multiplier := g.DenominatorReference, g.ResponsibilityMultiplier
	if reference == 0 || multiplier == 0 {
		p := DefaultCalibration()
		reference, multiplier = p.DenominatorReference, p.ResponsibilityMultiplier
	}
	return math.Round(100 * g.SupportedBurden / (reference + g.ResidualBurden + multiplier*(g.Hidden+g.EstimatedHidden)))
}
