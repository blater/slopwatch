package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

func renderTable(document report.Document, compact, includePass bool) string {
	headers := []string{"RANK", "SCORE", "COG MAX/#", "NPATH MAX/#", "CYCLO TOT/MAX", "SHALLOW", "GOD", "PATH"}
	if compact {
		headers = []string{"SCORE", "PATH"}
	} else if includePass {
		headers = append([]string{"PASS"}, headers...)
	}
	rows := make([][]string, 0, len(document.Files))
	for _, file := range document.Files {
		if compact {
			rows = append(rows, []string{scoreDisplay(file), file.Path})
			continue
		}
		row := []string{strconv.Itoa(file.Rank), scoreDisplay(file), maxCount(file, "cognitive_complexity"),
			maxCount(file, "npath_complexity"), cyclomatic(file), depth(file), contribution(file, "god_class"), file.Path}
		if includePass {
			passed := "NO"
			if file.Passed != nil && *file.Passed {
				passed = "yes"
			}
			row = append([]string{passed}, row...)
		}
		rows = append(rows, row)
	}
	widths := make([]int, len(headers))
	for i, value := range headers {
		widths[i] = len(value)
	}
	for _, row := range rows {
		for i, value := range row {
			widths[i] = max(widths[i], len(value))
		}
	}
	format := func(row []string) string {
		parts := make([]string, len(row))
		for i, value := range row {
			if i == len(row)-1 {
				parts[i] = fmt.Sprintf("%-*s", widths[i], value)
			} else {
				parts[i] = fmt.Sprintf("%*s", widths[i], value)
			}
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	lines := []string{format(headers)}
	for _, row := range rows {
		lines = append(lines, format(row))
	}
	return strings.Join(lines, "\n")
}

func renderDiagnostics(document report.Document) string {
	lines := make([]string, 0, len(document.Diagnostics))
	for _, diagnostic := range document.Diagnostics {
		severity, _ := diagnostic["severity"].(string)
		if severity != "error" && severity != "warning" {
			continue
		}
		code, _ := diagnostic["code"].(string)
		message, _ := diagnostic["message"].(string)
		if message == "" {
			continue
		}
		path, _ := diagnostic["path"].(string)
		location := ""
		if path != "" && !strings.HasPrefix(message, path+":") {
			location = path + ": "
		}
		prefix := code
		if severity != "" {
			prefix = severity + " " + prefix
		}
		lines = append(lines, fmt.Sprintf("%s: %s%s", prefix, location, message))
	}
	return strings.Join(lines, "\n")
}

func scoreDisplay(file report.File) string {
	if !scoring.ScoreAvailable(file) {
		return "X"
	}
	return report.DisplayNumber(file.Score)
}

func maxCount(file report.File, id string) string {
	if componentUnavailable(file, id) {
		return "X"
	}
	component, ok := file.Components[id]
	if !ok {
		return "-"
	}
	value, _ := report.Max(file, id)
	return fmt.Sprintf("%s/%d", report.DisplayNumber(value), len(component.Subjects))
}

func cyclomatic(file report.File) string {
	typeValue, typeOK := report.Max(file, "cyclomatic_class_complexity")
	methodValue, methodOK := report.Max(file, "cyclomatic_method_complexity")
	if !typeOK && !methodOK {
		return "-"
	}
	left, right := "-", "-"
	if componentUnavailable(file, "cyclomatic_class_complexity") {
		left = "X"
	} else if typeOK {
		left = report.DisplayNumber(typeValue)
	}
	if componentUnavailable(file, "cyclomatic_method_complexity") {
		right = "X"
	} else if methodOK {
		right = report.DisplayNumber(methodValue)
	}
	return left + "/" + right
}

func depth(file report.File) string {
	value := scoring.Metric(file, "deep")
	if value.State == "not_applicable" {
		return "N/A"
	}
	if value.State != "" && !value.Available {
		return "X"
	}
	if componentUnavailable(file, "module_shallowness") && !value.Available {
		return "X"
	}
	if !value.Available {
		return "-"
	}
	text := report.DisplayNumber(value.Value)
	return text
}

func contribution(file report.File, id string) string {
	if componentUnavailable(file, id) {
		return "X"
	}
	value, ok := report.Contribution(file, id)
	if !ok {
		return "-"
	}
	return report.DisplayNumber(value)
}

func componentUnavailable(file report.File, id string) bool {
	state := file.Coverage[id]
	return state == "failed" || state == "unavailable"
}
