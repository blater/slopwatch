package native

import (
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/report"
)

type scoreInputs struct {
	replaceCompleted bool
	observations     map[string]map[string][]observation
	coverage         map[string]map[string]string
	languages        map[string]string
	diagnostics      []map[string]any
	plans            []map[string]any
	depth            map[string]report.DepthBoundary
	depthByPath      map[string][]string
	depthStates      map[string]string
}

func newScoreInputs() scoreInputs {
	return scoreInputs{
		observations: map[string]map[string][]observation{},
		coverage:     map[string]map[string]string{},
		languages:    map[string]string{},
		depth:        map[string]report.DepthBoundary{},
		depthByPath:  map[string][]string{},
		depthStates:  map[string]string{},
	}
}

func (inputs *scoreInputs) add(record protocolRecord) error {
	switch record.Type {
	case "measurement":
		if record.Path == nil {
			return fmt.Errorf("measurement has no path")
		}
		// A later component result replaces its previous completed snapshot.
		if _, completed := inputs.coverage[*record.Path][record.Component]; completed && inputs.replaceCompleted {
			delete(inputs.coverage[*record.Path], record.Component)
			delete(inputs.observations[*record.Path], record.Component)
			if record.Component == "module_shallowness" {
				replacePreviewDepthPath(inputs, *record.Path, nil)
				delete(inputs.depthStates, *record.Path)
			}
		}
		if isDepthV4(record) {
			return inputs.addDepth(record)
		}
		value, err := number(record.Value)
		if err != nil {
			return err
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
			return fmt.Errorf("coverage has no path")
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
	return nil
}

func (inputs *scoreInputs) merge(other scoreInputs) {
	for path, components := range other.observations {
		inputs.observations[path] = components
	}
	for path, components := range other.coverage {
		inputs.coverage[path] = components
	}
	for path, language := range other.languages {
		inputs.languages[path] = language
	}
	inputs.diagnostics = append(inputs.diagnostics, other.diagnostics...)
	inputs.plans = append(inputs.plans, other.plans...)
	for id, boundary := range other.depth {
		inputs.mergeDepth(id, boundary)
	}
	for path, ids := range other.depthByPath {
		inputs.depthByPath[path] = appendUnique(inputs.depthByPath[path], ids...)
	}
	for path, state := range other.depthStates {
		inputs.depthStates[path] = mergeDepthState(inputs.depthStates[path], state)
	}
}

func (inputs *scoreInputs) mergeDepth(id string, boundary report.DepthBoundary) {
	if id == "" {
		return
	}
	if inputs.depth == nil {
		inputs.depth = map[string]report.DepthBoundary{}
	}
	current, ok := inputs.depth[id]
	if !ok {
		inputs.depth[id] = boundary
		return
	}
	inputs.depth[id] = mergeDepthBoundary(current, boundary)
}

func mergeDepthBoundary(current, boundary report.DepthBoundary) report.DepthBoundary {
	current = mergeDepthStateAndScore(current, boundary)
	current.Files = appendUnique(current.Files, boundary.Files...)
	current.DeclarationFiles = appendUnique(current.DeclarationFiles, boundary.DeclarationFiles...)
	if boundary.Scope != "" {
		current.Scope = boundary.Scope
	}
	current.Dependencies = appendUnique(current.Dependencies, boundary.Dependencies...)
	current.Reasons = appendUniqueAny(current.Reasons, boundary.Reasons...)
	current.PreciseReasons = appendUniqueAny(current.PreciseReasons, boundary.PreciseReasons...)
	current.Estimated = current.Estimated || boundary.Estimated
	if current.Estimated && current.State == "measured" {
		current.State = "partial"
	}
	current.Evidence = appendUniqueAny(current.Evidence, boundary.Evidence...)
	return current
}

func mergeDepthStateAndScore(current, boundary report.DepthBoundary) report.DepthBoundary {
	current = mergeBoundaryState(current, boundary)
	current = mergeDepthShallow(current, boundary)
	if current.PolicyRevision == "" {
		current.PolicyRevision = boundary.PolicyRevision
	}
	current = mergeDepthFingerprint(current, boundary)
	current = mergeMeasuredDepthScore(current, boundary)
	if current.State == "unavailable" || current.State == "not_applicable" {
		current.Shallow = nil
	}
	return current
}

func mergeBoundaryState(current, boundary report.DepthBoundary) report.DepthBoundary {
	if depthStateRank(boundary.State) > depthStateRank(current.State) {
		current.State = boundary.State
	}
	return current
}

func mergeDepthShallow(current, boundary report.DepthBoundary) report.DepthBoundary {
	if current.Shallow == nil && boundary.Shallow != nil {
		current.Shallow = boundary.Shallow
	}
	return current
}

func mergeDepthFingerprint(current, boundary report.DepthBoundary) report.DepthBoundary {
	if current.InventoryFingerprint == "" {
		current.InventoryFingerprint = boundary.InventoryFingerprint
	} else if boundary.InventoryFingerprint != "" && current.InventoryFingerprint != boundary.InventoryFingerprint {
		current.State, current.Shallow = "unavailable", nil
		current.Reasons = append(current.Reasons, map[string]any{"code": "conflicting_depth_inventory", "message": "duplicate boundary reported different inventory fingerprints"})
	}
	return current
}

func mergeMeasuredDepthScore(current, boundary report.DepthBoundary) report.DepthBoundary {
	if current.State == "measured" && boundary.State == "measured" && current.Shallow != nil && boundary.Shallow != nil && *current.Shallow != *boundary.Shallow {
		current.State, current.Shallow = "unavailable", nil
		current.Reasons = append(current.Reasons, map[string]any{"code": "conflicting_depth_score", "message": "duplicate boundary reported different shallow values"})
	}
	return current
}

func collectScoreInputs(records []protocolRecord) (scoreInputs, error) {
	inputs := newScoreInputs()
	for _, record := range records {
		if err := inputs.add(record); err != nil {
			return scoreInputs{}, err
		}
	}
	return inputs, nil
}
