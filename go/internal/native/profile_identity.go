package native

import (
	"encoding/json"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/sourceestimate"
)

// StructuralScoringPolicyRevision identifies grouped and continuous-curve
// projection semantics.
// It is independent of the SHALLOW policy revision so either depth profile
// invalidates stale score projections consistently.
const StructuralScoringPolicyRevision = "grouping-curves-v1"

type reportComponentIdentity struct {
	ID               string            `json:"id"`
	Version          string            `json:"version"`
	Axis             string            `json:"axis"`
	Kind             string            `json:"kind"`
	Aggregator       string            `json:"aggregator"`
	DeduplicationKey []string          `json:"deduplication_key"`
	Support          map[string]string `json:"support"`
	Defaults         componentDefaults `json:"defaults"`
}

func reportIdentity(catalog catalogDocument) (int, string, string, string, error) {
	return reportIdentityWithCalibration(catalog, sourceestimate.DefaultCalibrationIdentity())
}

func reportIdentityWithCalibration(catalog catalogDocument, calibration string) (int, string, string, string, error) {
	if !catalogHasDepthV4(catalog) {
		identity := struct {
			Languages  []string                  `json:"languages"`
			Analyzers  []analyzerDescriptor      `json:"analyzers"`
			Components []reportComponentIdentity `json:"components"`
			Policy     string                    `json:"structural_scoring_policy"`
		}{Languages: catalog.Languages, Analyzers: catalog.Analyzers, Policy: StructuralScoringPolicyRevision}
		for _, component := range catalog.Components {
			identity.Components = append(identity.Components, reportComponentIdentity{
				ID: component.ID, Version: component.Version, Axis: component.Axis, Kind: component.Kind,
				Aggregator: component.Aggregator, DeduplicationKey: component.DeduplicationKey,
				Support: component.Support, Defaults: component.Defaults,
			})
		}
		encoded, err := json.Marshal(identity)
		if err != nil {
			return 0, "", "", "", err
		}
		return 3, string(analysiscache.DigestBytes(encoded)), "", "", nil
	}
	identity := struct {
		Languages   []string                  `json:"languages"`
		Analyzers   []analyzerDescriptor      `json:"analyzers"`
		Components  []reportComponentIdentity `json:"components"`
		Profile     string                    `json:"profile"`
		Calibration string                    `json:"calibration"`
		Policy      string                    `json:"policy_revision"`
		Structural  string                    `json:"structural_scoring_policy"`
	}{Languages: catalog.Languages, Analyzers: catalog.Analyzers, Profile: ShallowProfileResponsibilityV4, Policy: ShallowPolicyRevisionV4, Structural: StructuralScoringPolicyRevision, Calibration: calibration}
	for _, component := range catalog.Components {
		identity.Components = append(identity.Components, reportComponentIdentity{
			ID: component.ID, Version: component.Version, Axis: component.Axis, Kind: component.Kind,
			Aggregator: component.Aggregator, DeduplicationKey: component.DeduplicationKey,
			Support: component.Support, Defaults: component.Defaults,
		})
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return 0, "", "", "", err
	}
	return 4, string(analysiscache.DigestBytes(encoded)), ShallowProfileResponsibilityV4, ShallowPolicyRevisionV4, nil
}

func catalogHasDepthV4(catalog catalogDocument) bool {
	for _, component := range catalog.Components {
		if component.ID == "module_shallowness" && component.Version == ShallowDefinitionV4 {
			return true
		}
	}
	return false
}
