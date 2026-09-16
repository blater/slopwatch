package follow

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/style"
)

func helpView(model Model) string {
	width := max(1, min(100, model.width-8))
	if model.helpTopic == "" {
		return helpTopicsView(model, width)
	}
	return helpTopicView(model, width)
}

func helpTopicsView(model Model, width int) string {
	body := make([]string, 0, len(helpTopics))
	for index, topic := range helpTopics {
		body = append(body, style.ModalOption(topic.label, index == model.helpCursor, max(1, width-2)))
	}
	return style.Popup(
		style.Heading("HELP"), body,
		hintRow(style.SurfaceModal, hintItem{"ENTER", "open"}), width,
	)
}

func helpTopicView(model Model, width int) string {
	topic, _ := helpTopicFor(model.helpTopic)
	bodyWidth := max(1, width-4)
	lines := topicLines(model.helpTopic, bodyWidth, model.helpCursor)
	body := scrollModalLines(lines, model.helpCursor, max(1, model.modalBodyHeight()-1))
	footer := hintRow(style.SurfaceModal, hintItem{"ESC", "topics"})
	if model.helpTopic == helpScoring {
		footer = hintRow(style.SurfaceModal, hintItem{"i", "info"}, hintItem{"ESC", "topics"})
	}
	return style.Popup(style.Heading(fmt.Sprintf("HELP · %s", topic.label)), body, footer, width)
}

func topicLines(topic string, width, selected int) []string {
	if topic == helpScoring {
		body := make([]string, 0, len(metricInformation))
		for index, info := range metricInformation {
			body = append(body, infoSummary(info, index == selected, max(1, width)))
		}
		return body
	}
	entries := commandLineHelp
	if topic == helpMainScreen {
		entries = mainScreenHelp
	}
	return helpEntryLines(entries, width)
}

func helpEntryLines(entries []helpEntry, width int) []string {
	lines := make([]string, 0, len(entries)*2)
	for _, entry := range entries {
		prefix := entry.label + " — "
		indent := strings.Repeat(" ", lipgloss.Width(prefix))
		lines = append(lines, wrapText(entry.description, width, prefix, indent)...)
	}
	return lines
}
