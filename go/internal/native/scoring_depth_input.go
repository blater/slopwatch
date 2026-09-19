package native

import (
	"fmt"
	"math"
	"sort"

	"github.com/blater/slopwatch/internal/report"
)

func isDepthV4(record protocolRecord) bool {
	return record.Type == "measurement" && record.Component == "module_shallowness" && record.Definition == "responsibility-burden-v4"
}

func (inputs *scoreInputs) addDepth(record protocolRecord) error {
	depth, err := decodeDepthBoundary(record)
	if err != nil {
		return err
	}
	if depth.ID == "" {
		if record.Path != nil {
			if inputs.depthStates == nil {
				inputs.depthStates = map[string]string{}
			}
			inputs.depthStates[*record.Path] = mergeDepthState(inputs.depthStates[*record.Path], depth.State)
			if record.Language != "" && inputs.languages[*record.Path] == "" {
				inputs.languages[*record.Path] = record.Language
			}
		}
		return nil
	}
	if inputs.depth == nil {
		inputs.depth = map[string]report.DepthBoundary{}
	}
	if record.Path != nil {
		depth.Files = []string{*record.Path}
		if inputs.depthStates == nil {
			inputs.depthStates = map[string]string{}
		}
		inputs.depthStates[*record.Path] = mergeDepthState(inputs.depthStates[*record.Path], depth.State)
		if record.Language != "" && inputs.languages[*record.Path] == "" {
			inputs.languages[*record.Path] = record.Language
		}
	}
	inputs.mergeDepth(depth.ID, depth)
	for _, path := range depth.Files {
		inputs.depthByPath[path] = appendUnique(inputs.depthByPath[path], depth.ID)
	}
	return nil
}

func decodeDepthBoundary(record protocolRecord) (report.DepthBoundary, error) {
	depth := report.DepthBoundary{ID: stringAttribute(record.Attributes, "boundary_id"), State: stringAttribute(record.Attributes, "knowledge_state"), PolicyRevision: stringAttribute(record.Attributes, "policy_revision")}
	if raw, ok := record.Attributes["depth"].(map[string]any); ok {
		if err := decodeRawDepth(&depth, raw); err != nil {
			return report.DepthBoundary{}, err
		}
	}
	if depth.InventoryFingerprint == "" {
		depth.InventoryFingerprint = stringAttribute(record.Attributes, "inventory_fingerprint")
	}
	if estimated, ok := record.Attributes["estimated"].(bool); ok {
		depth.Estimated = depth.Estimated || estimated
	}
	if len(depth.PreciseReasons) == 0 {
		depth.PreciseReasons = anySlice(record.Attributes["precise_reasons"])
	}
	if depth.ID == "" && depth.State == "measured" {
		return report.DepthBoundary{}, fmt.Errorf("measured v4 depth has no boundary id")
	}
	normalizeDepthState(&depth)
	if reason := record.Attributes["reason"]; reason != nil {
		depth.Reasons = append(depth.Reasons, reason)
	}
	if depth.State == "measured" {
		if err := validateMeasuredDepth(record, depth); err != nil {
			return report.DepthBoundary{}, err
		}
		if depth.Estimated {
			depth.State = "partial"
		}
	}
	depth = report.CompactDepthBoundary(depth)
	return depth, nil
}

func normalizeDepthState(depth *report.DepthBoundary) {
	if depth.State == "" {
		depth.State = "unavailable"
	}
	if !validDepthState(depth.State) {
		depth.State = "unavailable"
		depth.Reasons = append(depth.Reasons, map[string]any{"code": "unknown_depth_state", "message": "unsupported v4 knowledge state"})
	}
}

func decodeRawDepth(depth *report.DepthBoundary, raw map[string]any) error {
	depth.Raw = raw
	decodeRawIdentity(depth, raw)
	if err := decodeRawScore(depth, raw); err != nil {
		return err
	}
	depth.Dependencies = stringSlice(raw["dependencies"])
	depth.Reasons = anySlice(raw["reasons"])
	if estimated, ok := raw["estimated"].(bool); ok {
		depth.Estimated = estimated
	}
	depth.PreciseReasons = anySlice(raw["precise_reasons"])
	depth.Evidence = anySlice(raw["evidence"])
	return nil
}

func decodeRawIdentity(depth *report.DepthBoundary, raw map[string]any) {
	if value, ok := raw["boundary"].(map[string]any); ok {
		depth.Boundary = value
	}
	if depth.ID == "" {
		depth.ID = stringValue(raw["boundary_id"])
	}
	if depth.State == "" {
		depth.State = stringValue(raw["state"])
	}
	if value, ok := raw["policy_revision"].(string); ok && depth.PolicyRevision == "" {
		depth.PolicyRevision = value
	}
	if value, ok := raw["inventory_fingerprint"].(string); ok && depth.InventoryFingerprint == "" {
		depth.InventoryFingerprint = value
	}
}

func decodeRawScore(depth *report.DepthBoundary, raw map[string]any) error {
	if value, ok := raw["shallow"]; ok && value != nil {
		shallow, err := number(value)
		if err != nil || math.IsNaN(shallow) || math.IsInf(shallow, 0) || shallow < 0 || shallow > 100 {
			return fmt.Errorf("invalid v4 shallow value")
		}
		depth.Shallow = &shallow
	}
	return nil
}

func validateMeasuredDepth(record protocolRecord, depth report.DepthBoundary) error {
	if depth.Shallow == nil {
		return fmt.Errorf("measured v4 depth has no shallow value")
	}
	value, err := number(record.Value)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value != *depth.Shallow {
		return fmt.Errorf("v4 depth value disagrees with shallow")
	}
	return nil
}

func validDepthState(state string) bool {
	switch state {
	case "measured", "partial", "unavailable", "not_applicable":
		return true
	default:
		return false
	}
}

func depthStateRank(state string) int {
	switch state {
	case "unavailable":
		return 3
	case "partial":
		return 2
	case "measured":
		return 1
	default:
		return 0
	}
}

func mergeDepthState(current, incoming string) string {
	if current == "" || depthStateRank(incoming) > depthStateRank(current) {
		return incoming
	}
	return current
}

func stringAttribute(attributes map[string]any, key string) string {
	return stringValue(attributes[key])
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func stringSlice(value any) []string {
	result := []string{}
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
	case []string:
		result = append(result, items...)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	if len(items) == 0 {
		return nil
	}
	return append([]any(nil), items...)
}

func appendUnique(current []string, values ...string) []string {
	seen := make(map[string]bool, len(current)+len(values))
	for _, item := range current {
		seen[item] = true
	}
	for _, item := range values {
		if item != "" && !seen[item] {
			current, seen[item] = append(current, item), true
		}
	}
	sort.Strings(current)
	return current
}
