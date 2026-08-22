package follow

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/slopslap/slopslap/internal/report"
)

func metricColour(key string, value float64, exists bool) lipgloss.Color {
	if !exists {
		return colourMuted
	}
	hot, warm := false, false
	switch key {
	case "cog":
		hot, warm = value >= 30, value >= 15
	case "npath":
		hot, warm = value >= 200, value >= 80
	case "cyclo":
		hot, warm = value >= 20, value >= 10
	case "deep":
		hot, warm = value >= 60, value >= 30
	case "god":
		hot, warm = value >= 20, value > 0
	}
	if hot {
		return colourRed
	}
	if warm {
		return colourAmber
	}
	return colourGreen
}

func renderPath(path string, width int, background lipgloss.Color) string {
	displayed := clip(path, width)
	separator := strings.LastIndex(displayed, "/")
	if separator < 0 {
		return styleCell(displayed, colourText, background)
	}
	parent := styleCell(displayed[:separator+1], lipgloss.Color("#607b91"), background)
	name := styleCell(displayed[separator+1:], colourText, background)
	return parent + name
}

func metric(file report.File, key string) (float64, bool, float64) {
	componentID := map[string]string{"cog": "cognitive_complexity", "npath": "npath_complexity", "cyclo": "cyclomatic_method_complexity", "deep": "module_shallowness", "god": "god_class"}[key]
	contribution, exists := report.Contribution(file, componentID)
	if !exists {
		return 0, false, 0
	}
	if key == "deep" {
		value, _ := report.Sum(file, componentID)
		return value, true, contribution
	}
	if key == "god" {
		return contribution, true, contribution
	}
	value, _ := report.Max(file, componentID)
	return value, true, contribution
}

func scoreColour(value float64) lipgloss.Color {
	if value >= 100 {
		return scoreRed
	}
	if value >= 50 {
		return scoreAmber
	}
	return colourGreen
}

func decimalWithin(value float64, width int) string {
	result := report.OneDecimal(value)
	if len(result) <= width {
		return result
	}
	mantissa, exponent, found := strings.Cut(fmt.Sprintf("%.1e", value), "e")
	if found {
		if number, err := strconv.Atoi(exponent); err == nil {
			result = fmt.Sprintf("%se%d", mantissa, number)
		}
	}
	return truncate(result, width)
}

func directionColour(delta int) lipgloss.Color {
	if delta > 0 {
		return scoreRed
	}
	if delta < 0 {
		return colourGreen
	}
	return colourMuted
}

func editBackground(state rowState, now time.Time, window time.Duration) lipgloss.Color {
	age := now.Sub(state.editedAt)
	if age < 0 || age > window {
		return ""
	}
	base := [3]float64{72, 181, 235}
	if state.direction < 0 {
		base = [3]float64{48, 220, 157}
	}
	if state.direction > 0 {
		base = [3]float64{255, 82, 105}
	}
	fast := min(1.0, age.Seconds()/5.0)
	slow := max(0.0, 1-age.Seconds()/window.Seconds())
	strength := (0.22 - (0.12 * fast)) * slow
	background := [3]float64{7, 16, 25}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
		int(background[0]+(base[0]-background[0])*strength),
		int(background[1]+(base[1]-background[1])*strength),
		int(background[2]+(base[2]-background[2])*strength)))
}

func styleValue(value string, colour lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(colour).Render(value)
}

func styleCell(value string, foreground, background lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(foreground).Background(background).Render(value)
}

func pad(value string, width int, right bool) string {
	if lipgloss.Width(value) >= width {
		return truncate(value, width)
	}
	spaces := strings.Repeat(" ", width-lipgloss.Width(value))
	if right {
		return spaces + value
	}
	return value + spaces
}

func padANSI(value string, width int) string {
	if lipgloss.Width(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-lipgloss.Width(value))
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func clip(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func truncateANSI(value string, width int) string {
	return ansi.Truncate(value, width, "")
}

func truncateLeft(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width("…"+string(runes)) > width {
		runes = runes[1:]
	}
	return "…" + string(runes)
}
