package follow

import "github.com/blater/slopwatch/internal/style"

func helpView(model Model) string {
	width := max(1, min(100, model.width-8))
	body := make([]string, 0, len(metricInformation))
	for index, info := range metricInformation {
		body = append(body, infoSummary(info, index == model.helpCursor, max(1, width-2)))
	}
	return style.Popup(style.Heading("HELP"), scrollModalLines(body, model.helpCursor, max(1, model.modalBodyHeight()-1)), hintRow(style.SurfaceModal, hintItem{"i", "info"}), width)
}
