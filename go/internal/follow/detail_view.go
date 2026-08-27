package follow

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopmochi/internal/report"
	"github.com/blater/slopmochi/internal/style"
)

func detailView(model Model) string {
	file, ok := model.selectedFile()
	if !ok {
		return ""
	}
	outerWidth, outerHeight, titleHeight, bodyHeight, contentWidth := model.detailDimensions()
	innerWidth := max(1, outerWidth-2)
	lines := model.detailContent(file, contentWidth)
	maximumOffset := max(0, len(lines)-bodyHeight)
	offset := min(maximumOffset, max(0, model.detailOffset))

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(style.AccentPositive).Background(style.SurfaceDetailTitle)
	bodyStyle := lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceDetailBody)
	inner := make([]string, 0, outerHeight-2)
	for row := 0; row < titleHeight; row++ {
		text := ""
		if row == titleHeight/2 {
			text = "  FULL ANALYSIS"
			closeLabel := "ESC / Q CLOSE  "
			if lipgloss.Width(text)+lipgloss.Width(closeLabel) <= innerWidth {
				text += strings.Repeat(" ", innerWidth-lipgloss.Width(text)-lipgloss.Width(closeLabel)) + closeLabel
			}
		}
		inner = append(inner, titleStyle.Render(padANSI(truncateANSI(text, innerWidth), innerWidth)))
	}
	thumbStart, thumbSize := scrollbar(offset, len(lines), bodyHeight)
	for row := 0; row < bodyHeight; row++ {
		content := strings.Repeat(" ", contentWidth)
		if index := offset + row; index < len(lines) {
			content = lines[index]
		}
		track := " "
		trackColour := style.TextMuted
		if len(lines) > bodyHeight {
			track = "│"
			if row >= thumbStart && row < thumbStart+thumbSize {
				track = "█"
			}
		}
		inner = append(inner,
			bodyStyle.Render("  ")+
				content+
				lipgloss.NewStyle().Foreground(trackColour).Background(style.SurfaceDetailBody).Render(track)+
				bodyStyle.Render(" "),
		)
	}
	dialog := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(style.TextMuted).Render(strings.Join(inner, "\n"))
	return dialog
}

type detailLine struct {
	text   string
	colour lipgloss.Color
	bold   bool
}

func detailContent(model Model, file report.File, width int) []string {
	logical := detailHeaderLines(file)
	logical = append(logical, detailMetricLines(file)...)
	logical = append(logical, detailComponentLines(file)...)
	return renderDetailLines(logical, width)
}

func detailHeaderLines(file report.File) []detailLine {
	logical := []detailLine{
		{file.Path, style.TextPrimary, true},
		{fmt.Sprintf("%s  ·  rank %d  ·  score %.1f", file.Language, file.Rank, file.Score), style.TextMuted, false},
	}
	if file.Freshness != "" && file.Freshness != report.FreshnessCurrent {
		state := strings.ToUpper(strings.ReplaceAll(string(file.Freshness), "_", " "))
		if file.FreshnessNote != "" {
			state += "  ·  " + file.FreshnessNote
		}
		colour := style.AccentWarning
		if file.Freshness == report.FreshnessStaleError {
			colour = style.AccentCritical
		}
		logical = append(logical, detailLine{state, colour, true})
	}
	logical = append(logical,
		detailLine{"", style.TextPrimary, false},
		detailLine{"METRIC SUMMARY", style.AccentPositive, true},
	)
	return logical
}

func detailMetricLines(file report.File) []detailLine {
	logical := []detailLine{}
	labels := []struct{ id, label string }{
		{"cognitive_complexity", "Cognitive complexity"},
		{"npath_complexity", "NPath complexity"},
		{"cyclomatic_method_complexity", "Cyclomatic routine complexity"},
		{"cyclomatic_class_complexity", "Cyclomatic type complexity"},
		{"module_shallowness", "Module shallowness penalty"},
		{"god_class", "Responsibility concentration"},
	}
	for _, item := range labels {
		component, exists := file.Components[item.id]
		if !exists {
			logical = append(logical, detailLine{fmt.Sprintf("%-24s unavailable", item.label), style.TextMuted, false})
			continue
		}
		maximum := maxSubjectValue(component)
		if item.id == "god_class" {
			logical = append(logical, detailLine{fmt.Sprintf("%-24s %.1f across %d types", item.label, component.Contribution, component.Observations), style.TextMuted, false})
		} else if item.id == "module_shallowness" {
			logical = append(logical, detailLine{fmt.Sprintf("%-24s %s/100 penalty", item.label, report.DisplayNumber(maximum)), style.TextMuted, false})
		} else {
			unit := "routines"
			if item.id == "cyclomatic_class_complexity" {
				unit = "types"
			}
			logical = append(logical, detailLine{fmt.Sprintf("%-24s maximum %s across %d %s", item.label, report.DisplayNumber(maximum), component.Observations, unit), style.TextMuted, false})
		}
	}
	logical = append(logical, detailLine{"", style.TextPrimary, false}, detailLine{"COMPONENTS", style.AccentPositive, true})
	return logical
}

func maxSubjectValue(component report.Component) float64 {
	maximum := 0.0
	for _, subject := range component.Subjects {
		maximum = math.Max(maximum, subject.Value)
	}
	return maximum
}

func detailComponentLines(file report.File) []detailLine {
	logical := []detailLine{}
	componentIDs := make([]string, 0, len(file.Components))
	for id := range file.Components {
		componentIDs = append(componentIDs, id)
	}
	sort.Strings(componentIDs)
	for _, id := range componentIDs {
		component := file.Components[id]
		logical = append(logical, detailLine{fmt.Sprintf("%s  contribution %.1f  ·  observations %d", id, component.Contribution, component.Observations), style.TextPrimary, true})
		for _, subject := range component.Subjects {
			logical = append(logical, detailLine{fmt.Sprintf("  • %s = %s  (+%s)", subject.Subject, report.DisplayNumber(subject.Value), report.DisplayNumber(subject.Contribution)), style.TextMuted, false})
		}
	}
	return logical
}

func renderDetailLines(logical []detailLine, width int) []string {
	result := []string{}
	for _, line := range logical {
		wrapped := []string{""}
		if line.text != "" {
			wrapped = strings.Split(ansi.Hardwrap(line.text, max(1, width), false), "\n")
		}
		for _, part := range wrapped {
			style := lipgloss.NewStyle().Foreground(line.colour).Background(style.SurfaceDetailBody).Bold(line.bold)
			result = append(result, style.Render(padANSI(clip(part, width), width)))
		}
	}
	return result
}

func detailDimensions(model Model) (outerWidth, outerHeight, titleHeight, bodyHeight, contentWidth int) {
	outerWidth = min(model.width, max(4, int(math.Round(float64(model.width)*0.92))))
	outerHeight = min(model.height, max(4, int(math.Round(float64(model.height)*0.88))))
	innerWidth := max(1, outerWidth-2)
	innerHeight := max(2, outerHeight-2)
	titleHeight = min(3, max(1, innerHeight-1))
	bodyHeight = max(1, innerHeight-titleHeight)
	contentWidth = max(1, innerWidth-4)
	return
}

func detailBodyHeight(model Model) int {
	_, _, _, bodyHeight, _ := model.detailDimensions()
	return bodyHeight
}

func detailMaxOffset(model Model) int {
	file, ok := model.selectedFile()
	if !ok {
		return 0
	}
	_, _, _, bodyHeight, contentWidth := model.detailDimensions()
	return max(0, len(model.detailContent(file, contentWidth))-bodyHeight)
}

func (model *Model) clampDetailOffset() {
	model.detailOffset = min(max(0, model.detailOffset), model.detailMaxOffset())
}

func scrollbar(offset, total, viewport int) (start, size int) {
	if total <= viewport || viewport <= 0 {
		return 0, 0
	}
	size = max(1, viewport*viewport/total)
	travel := max(0, viewport-size)
	maximumOffset := max(1, total-viewport)
	start = int(math.Round(float64(offset) * float64(travel) / float64(maximumOffset)))
	return start, size
}
