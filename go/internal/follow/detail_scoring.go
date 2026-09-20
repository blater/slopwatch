package follow

import (
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/style"
)

// The scorer owns attribution; presentation never recomputes the winning signal.
func detailScoringLines(file report.File) []detailLine {
	if len(file.ScoringAttributions) == 0 && len(file.ScoringLimitations) == 0 {
		return nil
	}
	lines := []detailLine{{"SCORE ATTRIBUTION", style.AccentPositive, true}}
	for _, group := range file.ScoringAttributions {
		lines = append(lines, detailLine{
			fmt.Sprintf("%s: +%s from %s", group.Group, report.DisplayNumber(group.Contribution), scoringSignalLabel(group.Component)),
			style.TextPrimary, false,
		})
		signals := make([]string, 0, len(group.Signals))
		for _, signal := range group.Signals {
			signals = append(signals, fmt.Sprintf("%s %s", scoringSignalLabel(signal.Component), report.DisplayNumber(signal.Contribution)))
		}
		if len(signals) > 0 {
			lines = append(lines, detailLine{"  Supporting severities (not added): " + strings.Join(signals, ", "), style.TextMuted, false})
		}
	}
	for _, limitation := range file.ScoringLimitations {
		lines = append(lines, detailLine{"Scoring limitation: " + limitation, style.AccentWarning, false})
	}
	return append(lines, detailLine{"", style.TextPrimary, false})
}

func scoringSignalLabel(id string) string {
	if id == "cyclomatic_method_complexity" {
		return "Cyclomatic complexity"
	}
	if definition, ok := scoring.ComponentByID(id); ok {
		return definition.Label
	}
	return id
}

func detailSubjectCharge(id string, component report.Component, subject report.SubjectContribution) string {
	amount := report.DisplayNumber(subject.Contribution)
	switch id {
	case "cognitive_complexity", "cyclomatic_method_complexity", "npath_complexity", "deeply_nested_if":
		return "+" + amount
	}
	if component.ScoringDefinition != nil && component.ScoringDefinition.Aggregation == "max" {
		return "severity " + amount + "; largest applies"
	}
	return "+" + amount
}
