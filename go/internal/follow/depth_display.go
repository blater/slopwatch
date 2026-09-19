package follow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

func depthBoundaries(document report.Document, file report.File) []report.DepthBoundary {
	component, ok := file.Components["module_shallowness"]
	if !ok || component.DepthVersion != "responsibility-burden-v4" {
		return nil
	}
	ids := append([]string(nil), component.DepthBoundaryIDs...)
	sort.Strings(ids)
	result := make([]report.DepthBoundary, 0, len(ids))
	for _, id := range ids {
		if boundary, ok := document.Depth[id]; ok {
			result = append(result, boundary)
		}
	}
	if len(result) > 0 {
		return result
	}
	// Older native projections retain the typed depth payload in component
	// evidence even when the document ledger is absent.
	for _, evidence := range component.Evidence {
		boundary := report.DepthBoundary{ID: evidence.Name, State: stringAttribute(evidence.Attributes, "knowledge_state")}
		if raw, ok := evidence.Attributes["depth"].(map[string]any); ok {
			boundary.Raw = raw
			boundary.Reasons, boundary.Evidence = anySlice(raw["reasons"]), anySlice(raw["evidence"])
			boundary.PreciseReasons = anySlice(raw["precise_reasons"])
			boundary.Estimated, _ = raw["estimated"].(bool)
			if boundary.State == "" {
				boundary.State, _ = raw["state"].(string)
			}
		}
		result = append(result, boundary)
	}
	return result
}

func stringAttribute(attributes map[string]any, key string) string {
	value, _ := attributes[key].(string)
	return value
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func depthRecordText(kind string, value any) string {
	if item, ok := value.(map[string]any); ok {
		for _, key := range []string{"message", "summary", "description", "code", "name", "symbol"} {
			if text, ok := item[key].(string); ok && text != "" {
				return kind + ": " + text
			}
		}
	}
	if text, ok := value.(string); ok && text != "" {
		return kind + ": " + text
	}
	return ""
}

func depthSummaryLines(document report.Document, file report.File) []string {
	boundaries := depthBoundaries(document, file)
	if len(boundaries) == 0 {
		return nil
	}
	lines := []string{}
	contract := file.Components["module_shallowness"].DepthRole == "declaration-only-contract"
	if contract {
		lines = append(lines, "Declaration-only contract: file SHALLOW 0, no SCORE penalty. No behavioral implementation is declared here.")
	}
	for boundaryIndex, boundary := range boundaries {
		if boundaryIndex >= 4 {
			lines = append(lines, "SHALLOW v4: additional boundaries omitted")
			break
		}
		mode := "Analysis mode unavailable"
		if boundary.State == "measured" {
			mode = "Measured"
		}
		if boundary.Estimated || rawBool(boundary.Raw, "estimated") {
			mode = "Estimated"
		} else if boundary.State == "not_applicable" {
			mode = "Not applicable"
		}
		lines = append(lines, fmt.Sprintf("SHALLOW v4 %s: %s", boundary.ID, mode))
		if boundary.Shallow != nil {
			lines = append(lines, fmt.Sprintf("SHALLOW: %.0f", *boundary.Shallow))
		}
		lines = append(lines, depthGradedLines(boundary.Raw)...)
		lines = append(lines, depthEstimateLines(boundary.Raw)...)
		lines = append(lines, depthResponsibilityLines(boundary.Raw)...)
		for evidenceIndex, item := range boundary.Evidence {
			if evidenceIndex >= 3 {
				lines = append(lines, "evidence: additional evidence omitted")
				break
			}
			if text := depthEvidenceText(boundary, item); text != "" {
				lines = append(lines, text)
			}
		}
	}
	return lines
}

func depthEstimateLines(raw map[string]any) []string {
	estimate, _ := raw["estimate"].(map[string]any)
	if len(estimate) == 0 {
		return nil
	}
	lines := []string{}
	if stringAttribute(estimate, "penalty_support") == "no-supported-finding" {
		lines = append(lines, "No adverse SHALLOW finding established; this is not a claim of proven depth.")
	}
	if ratio, ok := depthNumber(estimate["descriptive_ratio"]); ok {
		lines = append(lines, fmt.Sprintf("Descriptive burden/depth ratio: %.0f (not defect severity).", ratio))
	}
	for _, value := range anySlice(estimate["findings"]) {
		if finding, ok := value.(map[string]any); ok {
			support := "support not recorded"
			switch stringAttribute(finding, "support") {
			case "observed-callers":
				support = "observed caller burden"
			case "source-inferred":
				support = "inferred from source"
			}
			text := fmt.Sprintf("Finding %s (%s): input %s in %s is unused.", stringAttribute(finding, "kind"), support, stringAttribute(finding, "parameter"), stringAttribute(finding, "operation"))
			if callers := depthStrings(finding["caller_files"]); len(callers) > 0 {
				text += " Callers: " + strings.Join(callers, ", ") + "."
			}
			lines = append(lines, text)
		}
	}

	for _, role := range depthStrings(estimate["roles"]) {
		switch role {
		case "supporting-error-representation", "supporting-java-error-type":
			lines = append(lines, "Supporting error representation excluded; this rating assesses the file's behavioral entry points.")
		case "passive-value-object-v1", "supporting-data-representation", "supporting-java-data-type", "supporting-rust-value-type":
			lines = append(lines, "Passive data representation excluded; its accessors do not replace the file's behavioral assessment.")
		case "supporting-rust-trait-contract":
			lines = append(lines, "Declaration-only trait contract excluded from behavioral burden.")
		}
	}
	for index, value := range anySlice(estimate["abstractions"]) {
		if index >= 12 {
			lines = append(lines, "Additional abstraction details omitted")
			break
		}
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		burden, bok := depthNumber(item["burden"])
		hidden, hok := depthNumber(item["hidden"])
		if bok && hok && burden+2*hidden > 0 {
			text := fmt.Sprintf("Abstraction %s (%s audience): B=%g H=%g", stringAttribute(item, "name"), stringAttribute(item, "audience"), burden, hidden)
			if published, ok := depthNumber(item["published_penalty"]); ok {
				text += fmt.Sprintf("; SHALLOW %.0f", published)
			}
			lines = append(lines, text)
			lines = append(lines, depthMaterialLimitations(stringAttribute(item, "name"), depthStrings(item["limitations"]))...)
		}
	}
	if model, _ := estimate["model"].(string); model != "" {
		lines = append(lines, "estimate model: "+model)
	}
	if burden, ok := depthNumber(estimate["burden"]); ok {
		lines = append(lines, fmt.Sprintf("estimated burden: %g", burden))
	}
	if hidden, ok := depthNumber(estimate["hidden"]); ok {
		lines = append(lines, fmt.Sprintf("recognized hidden responsibility: %g", hidden))
	}
	if known, ok := depthNumber(estimate["known_hidden"]); ok && known > 0 {
		lines = append(lines, fmt.Sprintf("recognized responsibility lower bound: %g", known))
	}
	if categories := depthCategories(estimate["categories"]); len(categories) > 0 {
		keys := make([]string, 0, len(categories))
		for key := range categories {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			if key == "unknown_call" || key == "unknown_outcome" {
				continue
			}
			if value, ok := depthNumber(categories[key]); ok {
				parts = append(parts, fmt.Sprintf("%s=%.0f", key, value))
			}
		}
		if len(parts) > 0 {
			lines = append(lines, "recognized categories: "+strings.Join(parts, ", "))
		}
	}
	// Raw semantic-completeness reasons remain in the exported diagnostics.
	// Only specific gaps in the assessed behavior belong in ordinary details.
	if len(anySlice(estimate["abstractions"])) == 0 {
		lines = append(lines, depthMaterialLimitations("", depthStrings(estimate["limitations"]))...)
	}
	return lines
}

// Limitations describe affected behavior, not whether the analyzer achieved a
// complete semantic proof. Keep unclassified diagnostics in the raw ledger.
func depthMaterialLimitations(operation string, limitations []string) []string {
	lines := []string{}
	seen := map[string]bool{}
	for _, limitation := range limitations {
		text := ""
		if strings.HasPrefix(limitation, "unresolved_call_range_0_2:") {
			call := strings.TrimPrefix(limitation, "unresolved_call_range_0_2:")
			call, _, _ = strings.Cut(call, "/")
			text = "Delegated behavior unresolved: " + call
		} else if limitation == "generated_constructor_unavailable" {
			text = "Generated constructor unavailable"
		}
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		if operation != "" {
			text = operation + ": " + text
		}
		lines = append(lines, "Limitation: "+text)
		if len(lines) == 3 {
			break
		}
	}
	return lines
}

func depthCategories(value any) map[string]any {
	if categories, ok := value.(map[string]any); ok {
		return categories
	}
	result := map[string]any{}
	if categories, ok := value.(map[string]float64); ok {
		for key, number := range categories {
			result[key] = number
		}
	}
	return result
}

func depthStrings(value any) []string {
	if values, ok := value.([]string); ok {
		return values
	}
	values, _ := value.([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func depthNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint64:
		return float64(number), true
	default:
		return 0, false
	}
}

// Explanations are presentation, constructed only for the selected file's popup.
// The ledger stores the evidence kind and facts, not repeated prose.
func depthEvidenceText(boundary report.DepthBoundary, value any) string {
	item, ok := value.(map[string]any)
	if !ok {
		return depthRecordText("evidence", value)
	}
	details, _ := item["details"].(map[string]any)
	switch stringAttribute(item, "kind") {
	case "supporting-error-representation", "supporting-java-error-type":
		return "Supporting error representation: a private error data type and its pure data construction/accessors. It adds no independent SHALLOW penalty."
	case "passive-value-object-v1", "supporting-data-representation", "supporting-java-data-type", "supporting-rust-value-type":
		return "Passive value object: SHALLOW 0 means no penalty for representing and exposing data; it does not mean exceptional functional depth."
	case "passive-enum-v1":
		return "Passive enum: SHALLOW 0 means no penalty for named values and their fixed metadata; it does not mean exceptional functional depth."
	case "passive-result-carrier-v1":
		return "Passive result carrier: SHALLOW 0 means no penalty for storing, returning, copying or resetting data; it does not mean exceptional functional depth."
	case "bounded-static-v1":
		if stringAttribute(item, "status") == "not_applicable" {
			return "evidence: no implemented service behavior in this boundary"
		}
		return "evidence: bounded source analysis"
	case "source-delegation":
		return fmt.Sprintf("evidence: expanded %d resolved source helpers", len(boundary.Dependencies))
	case "supporting-contract-v1":
		if member := stringAttribute(details, "member"); member != "" {
			return "evidence: " + member + " supports " + stringAttribute(details, "contract_member") + " through production binding " + stringAttribute(details, "consumer") + "/" + stringAttribute(details, "slot")
		}
		return "evidence: internal implementation supports a proven production contract binding"
	case "passive-creation-v1":
		if readOnly, _ := details["read_only_accessors"].(bool); readOnly {
			return "evidence: data-only creation family " + stringAttribute(details, "family") + " has only proven read-only accessors and no independent behavior or writable accessors"
		}
		return "evidence: data-only creation family " + stringAttribute(details, "family") + " has no independent behavior or writable accessors"
	}
	if kind := stringAttribute(item, "kind"); kind != "" {
		return "evidence: " + kind + " (" + stringAttribute(item, "status") + ")"
	}
	return depthRecordText("evidence", value)
}

func rawBool(raw map[string]any, key string) bool {
	value, _ := raw[key].(bool)
	return value
}

func depthDetailLines(document report.Document, file report.File) []detailLine {
	lines := depthSummaryLines(document, file)
	if len(lines) == 0 {
		return nil
	}
	result := []detailLine{{"", style.TextPrimary, false}, {"SHALLOW V4 EVIDENCE", style.AccentPositive, true}}
	for _, line := range lines {
		result = append(result, detailLine{"  " + strings.TrimSpace(line), style.TextMuted, false})
	}
	return result
}

func depthResponsibilityLines(raw map[string]any) []string {
	items := anySlice(raw["obligations"])
	lines := []string{}
	if stringAttribute(raw, "penalty_basis") == "no-supported-finding" {
		lines = append(lines, "No adverse SHALLOW finding established.")
		if ratio, ok := depthNumber(raw["responsibility_ratio"]); ok {
			lines = append(lines, fmt.Sprintf("Descriptive burden/depth ratio: %.0f (not defect severity).", ratio))
		}
	}
	for index, item := range items {
		if index >= 3 {
			lines = append(lines, "responsibility: additional witnesses omitted")
			break
		}
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		category, rule := stringAttribute(record, "category"), stringAttribute(record, "rule")
		if category != "" && rule != "" {
			lines = append(lines, "responsibility "+category+": "+strings.ReplaceAll(rule, "_", " "))
		}
	}
	return lines
}

func depthGradedLines(raw map[string]any) []string {
	lines := []string{}
	if grade, ok := raw["graded"].(map[string]any); ok {
		burden, bok := depthNumber(grade["residual_burden"])
		hidden, hok := depthNumber(grade["hidden_responsibility"])
		if bok && hok {
			lines = append(lines, fmt.Sprintf("Graded assessment: caller obligations %g; hidden responsibility %g.", burden, hidden))
		}
		if estimated, ok := depthNumber(grade["estimated_hidden_responsibility"]); ok && estimated > 0 {
			lines = append(lines, fmt.Sprintf("Estimated responsibility allowance: %g units for unresolved duties supported by the call site; separate from observed work.", estimated))
		}
	}
	switch stringAttribute(raw, "zero_reason") {
	case "recognized_role":
		lines = append(lines, "Zero: positively recognized role with no applicable SHALLOW penalty.")
	case "lowest_range_supported":
		lines = append(lines, "Zero: supported assessment in the lowest SHALLOW range.")
	case "conservative_uncertainty":
		lines = append(lines, "Zero: conservative estimate under material uncertainty; not complete semantic coverage.")
	}
	return lines
}
