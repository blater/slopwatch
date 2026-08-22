package native

import "github.com/slopslap/slopslap/internal/report"

func scoreFile(path, language string, descriptors []componentDescriptor, observations map[string]map[string][]observation, coverage map[string]map[string]string, passScore *float64) (report.File, error) {
	file := report.File{Path: path, Language: language, Complete: true, Components: map[string]report.Component{}, Coverage: map[string]string{}, Axes: map[string]float64{}, ObservedAxes: map[string]float64{}}
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
		raw := observations[path][descriptor.ID]
		component, err := scoreComponent(descriptor, state, raw)
		if err != nil {
			return report.File{}, err
		}
		file.Components[descriptor.ID] = component
		file.Axes[descriptor.Axis] = roundScore(file.Axes[descriptor.Axis] + component.Contribution)
		file.ObservedAxes[descriptor.Axis] = file.Axes[descriptor.Axis]
	}
	for _, value := range file.Axes {
		file.Score += value
	}
	file.Score = roundScore(file.Score)
	file.ObservedScore = file.Score
	file.ValidZero = file.Complete && file.Score == 0
	if passScore != nil {
		passed := file.Complete && file.Score <= *passScore
		file.Passed = &passed
	}
	return file, nil
}
