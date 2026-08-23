package follow

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/style"
)

type metricInfo struct {
	key         string
	label       string
	short       string
	description string
}

type dialogPolicy struct {
	hasInteractiveOptions bool
}

func (model Model) activeDialogPolicy() dialogPolicy {
	if model.infoOpen {
		return dialogPolicy{}
	}
	if model.help || model.detail || model.sourceView || model.columns || model.sortOpen || model.weightsOpen || model.settings {
		return dialogPolicy{hasInteractiveOptions: true}
	}
	return dialogPolicy{}
}

func (model *Model) handleDialogKey(name string) bool {
	policy := model.activeDialogPolicy()
	if name == "enter" && !policy.hasInteractiveOptions && model.infoOpen {
		model.infoOpen = false
		return true
	}
	return false
}

var metricInformation = []metricInfo{
	{key: "score", label: "SCORE", short: "overall score", description: "The weighted sum of the enabled component contributions for a file. SCORE does not add raw metric values. Lower is better."},
	{key: "cog", label: "COG", short: "cognitive complexity", description: "Cognitive complexity estimates the mental effort needed to understand a routine. Decisions cost more when they are nested. Boolean sequences, conditional expressions, recursion, and labeled jumps add further cost. Lower is better."},
	{key: "npath", label: "NPATH", short: "execution path complexity", description: "NPath complexity counts distinct acyclic execution routes through a routine. Branches combine their possible outcomes, boolean expressions include short-circuit outcomes, loops retain their zero-iteration route, and switches account for their cases. Lower is better."},
	{key: "cyclo", label: "CYCLO", short: "cyclomatic complexity", description: "Cyclomatic complexity counts independent control-flow decisions. It starts at one and increases for branches, loops, non-default switch cases, boolean decisions, conditional expressions, and other exposed decision points. Lower is better."},
	{key: "deep", label: "SHALLOW", short: "module depth", description: "SHALLOW estimates how much useful functionality a module provides through its caller-visible interface. A module is shallower when its interface is large relative to the functionality it delivers. Higher is worse."},
	{key: "god", label: "GOD", short: "responsibility concentration", description: "GOD identifies types that concentrate too much responsibility. It uses weighted routine complexity, access to foreign data, and type cohesion. A non-zero value means the type meets the combined responsibility-concentration conditions. Lower is better."},
	{key: "coupling", label: "CPL", short: "dependency entanglement", description: "CPL measures how many other types a type depends on. High coupling makes changes more likely to cross type boundaries and increases the cost of change. Lower is better."},
	{key: "nesting", label: "NEST", short: "deep nesting", description: "NEST counts control-flow branches nested beyond the configured depth. It isolates the cost of excessive nesting, which COG already reflects, so this measure is disabled by default. Enable it when that additional signal is useful. Lower is better."},
	{key: "typesafety", label: "TYPE", short: "type safety", description: "TYPE sums TypeScript type-safety findings, including explicit any, unsafe assertions and boundaries, unsafe propagation and use, ambiguous booleans, non-exhaustive unions, and complex types. It is disabled by default. Lower is better."},
	{key: "path", label: "PATH", short: "source filename", description: "The source file path. PATH identifies the file; it is not a quality measurement."},
}

func metricInfoFor(key string) (metricInfo, bool) {
	for _, info := range metricInformation {
		if info.key == key {
			return info, true
		}
	}
	return metricInfo{}, false
}

func weightInfoKey(id string) string {
	switch id {
	case "cognitive_complexity":
		return "cog"
	case "npath_complexity":
		return "npath"
	case "cyclomatic_method_complexity", "cyclomatic_class_complexity":
		return "cyclo"
	case "module_shallowness":
		return "deep"
	case "god_class":
		return "god"
	case "coupling_between_objects":
		return "coupling"
	case "deeply_nested_if":
		return "nesting"
	case "ambiguous_boolean_expression", "explicit_any", "non_exhaustive_union", "type_complexity", "unsafe_type_assertion", "unsafe_type_boundary", "unsafe_type_propagation", "unsafe_type_use":
		return "typesafety"
	default:
		return "score"
	}
}

func (model *Model) openInfo(key string) {
	if _, ok := metricInfoFor(key); ok {
		model.infoKey = key
		model.infoOpen = true
	}
}

func (model *Model) handleInfoKey(name string) (tea.Model, tea.Cmd) {
	if model.handleDialogKey(name) {
		return model, nil
	}
	if name == "esc" || name == "escape" || name == "q" || name == "i" {
		model.infoOpen = false
	}
	return model, nil
}

func (model *Model) handleHelpKey(name string) (tea.Model, tea.Cmd) {
	switch name {
	case "esc", "escape", "q", "h":
		model.help = false
	case "up", "k":
		model.helpCursor = max(0, model.helpCursor-1)
	case "down", "j":
		model.helpCursor = min(len(metricInformation)-1, model.helpCursor+1)
	case "home", "g":
		model.helpCursor = 0
	case "end", "G":
		model.helpCursor = len(metricInformation) - 1
	case "i", "enter":
		model.openInfo(metricInformation[model.helpCursor].key)
	}
	return model, nil
}

func (model Model) infoView() string {
	info, ok := metricInfoFor(model.infoKey)
	if !ok {
		return ""
	}
	width := max(1, min(90, model.width-8))
	content := wrapText(info.description, max(1, width-4), "", "")
	return style.Popup(style.Heading(fmt.Sprintf("%s  %s", info.label, info.short)), content, "", width)
}

func infoSummary(info metricInfo, selected bool, width int) string {
	return style.ModalOption(fmt.Sprintf("%-7s %s", info.label, info.short), selected, width)
}
