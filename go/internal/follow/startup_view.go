package follow

import (
	_ "embed"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

//go:embed startup_logo.ansi
var embeddedStartupLogo string

func cleanStartupLogo(value string) string {
	value = strings.ReplaceAll(value, "\x1b[?25l", "")
	value = strings.ReplaceAll(value, "\x1b[?25h", "")
	return strings.Trim(value, "\r\n")
}

func fitStartupLogo(value string, width, height int) string {
	lines := strings.Split(cleanStartupLogo(value), "\n")
	if len(lines) > height {
		first := (len(lines) - height) / 2
		lines = lines[first : first+height]
	}
	for index, line := range lines {
		lineWidth := lipgloss.Width(line)
		if lineWidth > width {
			first := (lineWidth - width) / 2
			lines[index] = ansi.Cut(line, first, first+width)
		}
	}
	return strings.Join(lines, "\n")
}

func startupOverlay(model Model, base, logo string) string {
	return model.overlay(base, fitStartupLogo(logo, model.width, model.height))
}

func startupView(model Model, base string) string {
	return model.startupOverlay(base, embeddedStartupLogo)
}
