package native

import (
	"fmt"
	"strings"
)

type scoreInputs struct {
	observations map[string]map[string][]observation
	coverage     map[string]map[string]string
	languages    map[string]string
	diagnostics  []map[string]any
	plans        []map[string]any
}

func collectScoreInputs(records []protocolRecord) (scoreInputs, error) {
	inputs := scoreInputs{observations: map[string]map[string][]observation{}, coverage: map[string]map[string]string{}, languages: map[string]string{}}
	for _, record := range records {
		switch record.Type {
		case "measurement":
			if record.Path == nil {
				return scoreInputs{}, fmt.Errorf("measurement has no path")
			}
			value, err := number(record.Value)
			if err != nil {
				return scoreInputs{}, err
			}
			item := observation{
				component: record.Component, path: *record.Path, language: record.Language,
				scope: record.Scope, value: value, subject: record.Subject,
				attributes: record.Attributes, provenance: record.Provenance,
			}
			if inputs.observations[item.path] == nil {
				inputs.observations[item.path] = map[string][]observation{}
			}
			inputs.observations[item.path][item.component] = append(inputs.observations[item.path][item.component], item)
			inputs.languages[item.path] = item.language
		case "coverage":
			if record.Path == nil {
				return scoreInputs{}, fmt.Errorf("coverage has no path")
			}
			if inputs.coverage[*record.Path] == nil {
				inputs.coverage[*record.Path] = map[string]string{}
			}
			inputs.coverage[*record.Path][record.Component] = record.State
			if inputs.languages[*record.Path] == "" {
				inputs.languages[*record.Path] = strings.TrimSuffix(record.UnitID, "-unit")
			}
		case "diagnostic":
			inputs.diagnostics = append(inputs.diagnostics, record.Raw)
		case "execution_plan":
			inputs.plans = append(inputs.plans, record.Raw)
		}
	}
	return inputs, nil
}
