package follow

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

func metricColour(key string, value float64, exists bool) lipgloss.Color {
	if !exists {
		return style.TextMuted
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
	case "typesafety":
		hot, warm = value >= 50, value >= 20
	case "nesting":
		hot, warm = value >= 20, value >= 5
	case "coupling":
		hot, warm = value >= 30, value >= 15
	}
	if hot {
		return style.AccentCritical
	}
	if warm {
		return style.AccentWarning
	}
	return style.AccentPositive
}

func renderPath(path string, width, offset int, background lipgloss.Color) string {
	separator := strings.LastIndex(path, "/")
	if separator < 0 {
		return ansi.Cut(styleCell(path, style.TextPrimary, background), offset, offset+width)
	}
	parent := styleCell(path[:separator+1], style.TextMuted, background)
	name := styleCell(path[separator+1:], style.TextPrimary, background)
	return ansi.Cut(parent+name, offset, offset+width)
}

func metric(file report.File, key string) (float64, bool, float64) {
	if key == "typesafety" {
		value, exists := file.Axes["typescript_type_safety"]
		return value, exists, value
	}
	if key == "nesting" {
		component, exists := file.Components["deeply_nested_if"]
		return component.Contribution, exists, component.Contribution
	}
	if key == "coupling" {
		component, exists := file.Components["coupling_between_objects"]
		value, _ := report.Max(file, "coupling_between_objects")
		return value, exists, component.Contribution
	}
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
		return style.ScoreCritical
	}
	if value >= 50 {
		return style.ScoreWarning
	}
	return style.AccentPositive
}

func decimalWithin(value float64, width int) string {
	result := roundedIntegerText(value)
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

func roundedIntegerText(value float64) string {
	return strconv.FormatInt(int64(math.Round(value)), 10)
}

func directionColour(delta int) lipgloss.Color {
	if delta > 0 {
		return style.AccentCritical
	}
	if delta < 0 {
		return style.AccentPositive
	}
	return style.TextMuted
}

func editBackground(state rowState, now time.Time, window time.Duration) lipgloss.Color {
	age := now.Sub(state.editedAt)
	if age < 0 || age > window {
		return ""
	}
	base := colourComponents(style.AccentInfo)
	if state.direction < 0 {
		base = colourComponents(style.AccentPositive)
	}
	if state.direction > 0 {
		base = colourComponents(style.AccentCritical)
	}
	fast := min(1.0, age.Seconds()/5.0)
	slow := max(0.0, 1-age.Seconds()/window.Seconds())
	strength := (0.22 - (0.12 * fast)) * slow
	background := colourComponents(style.SurfaceScreen)
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
		int(background[0]+(base[0]-background[0])*strength),
		int(background[1]+(base[1]-background[1])*strength),
		int(background[2]+(base[2]-background[2])*strength)))
}

func colourComponents(colour lipgloss.Color) [3]float64 {
	hex := strings.TrimPrefix(string(colour), "#")
	if len(hex) != 6 {
		return [3]float64{}
	}
	components := [3]float64{}
	for index := range components {
		value, _ := strconv.ParseUint(hex[index*2:index*2+2], 16, 8)
		components[index] = float64(value)
	}
	return components
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
