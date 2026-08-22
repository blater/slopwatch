package follow

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (model Model) helpView() string {
	width := max(1, min(100, model.width-8))
	lines := []string{
		"HELP",
		"",
		"SCORE   Weighted sum of the COG, NPath, Cyclo, SHALLOW, and God metrics. Lower is better",
		"COG     Cognitive effort needed to understand nested decisions. Lower is better",
		"NPATH   Number of possible execution paths. Lower is better",
		"CYCLO   Cyclomatic complexity - independent control-flow paths. Lower is better",
		"SHALLOW Depth is functionality per unit of interface complexity. Shallowness is bad",
		"GOD     God classes -bloated modules with a low-cohesion class score. Keep this low",
		"",
		keyHint("V", "       Open the highlighted source file", lipgloss.Color("#0d1d29")),
		keyHint("f", "       Find file paths or source text (/ also works)", lipgloss.Color("#0d1d29")),
		keyHint("n / N", "   Next or previous find match", lipgloss.Color("#0d1d29")),
		"",
		keyHint("Esc", " to close help", lipgloss.Color("#0d1d29")),
	}
	return lipgloss.NewStyle().Width(width).Padding(1, 2).
		Border(lipgloss.RoundedBorder()).BorderForeground(colourBlue).
		Background(lipgloss.Color("#0d1d29")).Foreground(colourText).
		Render(strings.Join(lines, "\n"))
}
