package native

import (
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

func scoreFile(path, language string, descriptors []componentDescriptor, observations map[string]map[string][]observation, coverage map[string]map[string]string, depths map[string]report.DepthBoundary, depthByPath map[string][]string, depthStates map[string]string, passScore *float64) (report.File, error) {
	file := report.File{Path: path, Language: language, Complete: true, Components: map[string]report.Component{}, Coverage: map[string]string{}, Axes: map[string]float64{}, ObservedAxes: map[string]float64{}}
	associated := associateScoringRoutines(observations[path])
	for _, descriptor := range descriptors {
		if !descriptor.Defaults.Enabled || !descriptor.supported(language) {
			continue
		}
		state := coverage[path][descriptor.ID]
		if state == "" {
			state = "not_requested"
		}
		file.Coverage[descriptor.ID] = state
		if state != "complete" {
			file.Complete = false
		}
		raw := associated[descriptor.ID]
		component, err := scoreComponent(descriptor, state, raw)
		if descriptor.ID == "module_shallowness" && (descriptor.Version == "responsibility-burden-v4" || len(depthByPath[path]) > 0 || depthStates[path] != "") {
			component, err = scoreDepthComponent(descriptor, state, depths, depthByPath[path], depthStates[path], path)
		}
		if err != nil {
			return report.File{}, err
		}
		component.Axis = descriptor.Axis
		file.Components[descriptor.ID] = component
		if descriptor.ID == "module_shallowness" && (component.DepthState == "partial" || component.DepthState == "unavailable" || component.DepthEstimated) {
			file.Complete = false
		}
	}
	associateTypeOwners(&file)
	// Capture the catalog-weighted component totals before overlap grouping.
	// Projection may change SCORE, but Observed* remains the immutable raw
	// baseline used by display and later re-projections.
	for id, component := range file.Components {
		axis := scoring.ComponentAxis(id)
		if component.Axis != "" {
			axis = component.Axis
		}
		file.ObservedAxes[axis] += component.Contribution
		file.ObservedScore += component.Contribution
	}
	weights := map[string]float64{}
	enabled := map[string]bool{}
	for _, descriptor := range descriptors {
		weight, err := descriptor.Defaults.weight()
		if err != nil {
			return report.File{}, err
		}
		weights[descriptor.ID] = weight
		enabled[descriptor.ID] = descriptor.Defaults.Enabled && descriptor.supported(language)
	}
	file = scoring.ProjectFile(file, scoring.NewPolicy(weights, enabled))
	file.ValidZero = file.Complete && file.Score == 0
	if passScore != nil {
		passed := file.Complete && file.Score <= *passScore
		file.Passed = &passed
	}
	return file, nil
}
