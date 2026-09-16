package follow

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/style"
)

func configSettingsRowsForState(state configSettingsState) int {
	switch state.kind {
	case configAgents:
		return len(agentProviderChoices)
	case configFix:
		return fixSettingsMetricStart + len(fixSettingsMetrics())
	case configConcurrency:
		return 7
	case configDelivery:
		return len(deliverySettingFields(state.working.Delivery))
	default:
		return 0
	}
}

func configSettingsPopup(state configSettingsState, catalog agent.ProfileCatalog, width, height int) string {
	terminalWidth := width
	popupWidth := min(72, max(38, terminalWidth-4))
	title := configSettingsTitleForState(state)
	if state.loading {
		return style.Popup(style.Heading(title), []string{"Loading configuration…"}, "Esc back", popupWidth)
	}
	lines, selectedStart, selectedEnd := configSettingsContentForState(state, catalog, popupWidth-4)
	if len(lines) == 0 {
		lines = []string{"No configured items."}
	}
	bodyWidth := popupWidth - 4
	lines, windowStart := scrollModalLineWindow(lines, selectedStart, selectedEnd, max(1, configModalBodyHeight(height)-2))
	if state.choiceOpen {
		lines = overlayConfigChoiceMenu(lines, state, bodyWidth, selectedStart-windowStart)
	}
	if state.kind != configAgents {
		lines = append(lines, style.DisabledOption(truncate(state.status, popupWidth), popupWidth))
	}
	footer := configSettingsFooterForState(state, catalog, terminalWidth, height)
	if state.editing {
		input := state.input
		style.ApplyTextInputStyle(&input, true)
		lines = append(lines, "Edit: "+style.InputField(input.View(), max(1, popupWidth-10)))
	}
	return style.Popup(style.Heading(title), lines, footer, popupWidth)
}

func configModalBodyHeight(height int) int {
	if height <= 0 {
		return 1 << 30
	}
	return max(1, height-6)
}

func configSettingsFullScreen(state configSettingsState, catalog agent.ProfileCatalog, width, height int) string {
	title := configSettingsTitleForState(state)
	footer := configSettingsFooterForState(state, catalog, width, height)
	lines := []string{fixSurfaceLine(title, width, style.SurfaceHeader, style.TextPrimary)}
	if state.loading {
		lines = append(lines, fixSurfaceLine("Loading configuration…", width, style.SurfaceModal, style.TextMuted))
	} else {
		body, selectedStart, selectedEnd := configSettingsContentForState(state, catalog, width)
		reserved := 3 // header, status, footer
		if state.editing {
			reserved++ // active editor row
		}
		body, windowStart := scrollModalLineWindow(body, selectedStart, selectedEnd, max(1, height-reserved))
		if state.choiceOpen {
			body = overlayConfigChoiceMenu(body, state, width, selectedStart-windowStart)
		}
		for _, line := range body {
			lines = append(lines, fixSurfaceLineANSI(line, width, style.SurfaceModal))
		}
		if state.kind != configAgents {
			lines = append(lines, fixSurfaceLine(state.status, width, style.SurfaceModal, style.TextMuted))
		}
	}
	if state.editing {
		input := state.input
		style.ApplyTextInputStyle(&input, true)
		lines = append(lines, fixSurfaceLineANSI("Edit: "+style.InputField(input.View(), max(1, width-6)), width, style.SurfaceModal))
	}
	for len(lines) < height-1 {
		lines = append(lines, fixSurfaceLine("", width, style.SurfaceModal, style.TextPrimary))
	}
	lines = append(lines, fixSurfaceLine(footer, width, style.SurfaceFooter, style.TextMuted))
	return joinScreenLines(lines[:height])
}

func configSettingsTitleForState(state configSettingsState) string {
	if state.kind == configAgents && state.profileEditing {
		for _, choice := range agentProviderChoices {
			if choice.Runtime == state.providerRuntime {
				return strings.ToUpper(choice.Label)
			}
		}
	}
	return map[configSettingsKind]string{
		configAgents: "AGENTS", configFix: "FIX DEFAULTS", configConcurrency: "CONCURRENCY & RETENTION",
		configDelivery: "GIT & PULL REQUESTS",
	}[state.kind]
}

func configSettingsFooterForState(state configSettingsState, catalog agent.ProfileCatalog, width, height int) string {
	compact := responsiveTier(width, height) == ResponsiveCompact
	if state.choiceOpen {
		return "Enter select"
	}
	if state.editing {
		return "Enter apply · Esc cancel"
	}
	if state.kind == configAgents {
		if state.profileEditing {
			if state.profileFieldCount(catalog) > 0 {
				return "Enter edit"
			}
			return ""
		}
		return "Enter select"
	}
	readonly := false
	edit := false
	metrics := state.kind == configFix && state.cursor >= fixSettingsMetricStart
	switch state.kind {
	case configFix:
		edit = state.cursor == fixSettingsPromptRow
	case configDelivery:
		edit = state.textEditable()
	}
	if compact {
		switch {
		case readonly:
			return "read-only · Esc"
		case edit:
			return "Enter edit · Esc"
		case metrics:
			return "Space select · Esc"
		default:
			return "Esc"
		}
	}
	switch {
	case readonly:
		return "Read-only in this release · Esc back"
	case edit:
		return "Enter edit · Esc back"
	case metrics:
		return "Space select · Esc back"
	default:
		return "Esc back"
	}
}

func firstPromptLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	return value
}

func agentProbeReadiness(result agent.ProbeResult) string {
	if result.State == agent.ProbeReady && result.Capabilities.Isolation.EligibleForMutation() {
		return "RUNNABLE · ready"
	}
	state := string(result.State)
	if result.State == agent.ProbeReady {
		state = "confinement failed"
	}
	if state == "" {
		state = "unknown"
	}
	return "NOT RUNNABLE · " + state
}

func configSettingsContentForState(state configSettingsState, catalog agent.ProfileCatalog, width int) ([]string, int, int) {
	if state.kind == configAgents {
		return agentSettingsLinesForState(state, catalog, width)
	}
	lines := configSettingsLinesForState(state, catalog, width)
	selected := state.cursor
	if state.kind == configFix && selected >= fixSettingsMetricStart {
		// Six ordinary rows, one heading, then three metric cells per line.
		selected = 7 + (selected-fixSettingsMetricStart)/3
	}
	selected = min(max(0, selected), max(0, len(lines)-1))
	return lines, selected, selected
}

func scrollModalLineRange(lines []string, selectedStart, selectedEnd, limit int) []string {
	window, _ := scrollModalLineWindow(lines, selectedStart, selectedEnd, limit)
	return window
}

func scrollModalLineWindow(lines []string, selectedStart, selectedEnd, limit int) ([]string, int) {
	if len(lines) <= limit {
		return lines, 0
	}
	limit = max(1, limit)
	selectedStart = min(len(lines)-1, max(0, selectedStart))
	selectedEnd = min(len(lines)-1, max(selectedStart, selectedEnd))
	if selectedEnd-selectedStart+1 >= limit {
		return lines[selectedStart : selectedStart+limit], selectedStart
	}
	start := max(0, selectedEnd-limit+1)
	if start > selectedStart {
		start = selectedStart
	}
	if start+limit > len(lines) {
		start = max(0, len(lines)-limit)
	}
	return lines[start : start+limit], start
}

func overlayConfigChoiceMenu(lines []string, state configSettingsState, width, anchorRow int) []string {
	if len(lines) == 0 || anchorRow < 0 || anchorRow >= len(lines) {
		return lines
	}
	const fieldStart = 24
	if width <= 0 {
		return lines
	}
	menu := formChoiceMenu(state.choices(state.cursor), state.choiceCursor, false, width, len(lines), 1)
	if len(menu) == 0 {
		return lines
	}
	menuTop := min(anchorRow, max(0, len(lines)-len(menu)))
	menuLeft := min(fieldStart, max(0, width-lipgloss.Width(menu[0])))
	return overlayFixLines(lines, menu, menuLeft, menuTop, width, len(lines))
}

func wrappedDisabledConfigLines(value string, width int) []string {
	value = cleanAgentText(value)
	wrapped := wrapText(value, max(1, width), "", "")
	lines := make([]string, 0, len(wrapped))
	for _, logicalLine := range wrapped {
		for _, line := range strings.Split(ansi.Hardwrap(logicalLine, max(1, width), false), "\n") {
			lines = append(lines, style.DisabledOption(line, width))
		}
	}
	return lines
}

func agentSettingsLinesForState(state configSettingsState, catalog agent.ProfileCatalog, width int) ([]string, int, int) {
	if state.profileEditing {
		return agentConnectionLinesForState(state, catalog, width)
	}

	activeRuntime := runtimeForProfile(state.working.Profiles, state.working.Fix.Profile)
	labelWidth := 0
	for _, choice := range agentProviderChoices {
		labelWidth = max(labelWidth, len(choice.Label))
	}
	lines := make([]string, 0, len(agentProviderChoices))
	for index, choice := range agentProviderChoices {
		available, checking := agentProviderAvailabilityForState(state, catalog, choice)
		status := "available"
		switch {
		case choice.Runtime == activeRuntime && available:
			status = "[ACTIVE]"
		case choice.Runtime == activeRuntime:
			status = "[ACTIVE] · unavailable"
		case checking:
			status = "checking…"
		case !available:
			status = "not available"
		}
		row := fmt.Sprintf("%-*s  %s", labelWidth, choice.Label, status)
		background := style.SurfaceScreen
		if index == state.cursor {
			background = style.SurfaceSelected
		}
		foreground := style.TextPrimary
		if !available {
			foreground = style.TextMuted
		}
		lines = append(lines, lipgloss.NewStyle().Width(width).Background(background).Foreground(foreground).
			Bold(choice.Runtime == activeRuntime).Render(truncate(row, width)))
	}
	selected := min(max(0, state.cursor), len(lines)-1)
	return lines, selected, selected
}

func agentProviderAvailabilityForState(state configSettingsState, catalog agent.ProfileCatalog, choice agentProviderChoice) (available, checking bool) {
	if catalog == nil {
		return false, false
	}
	if _, err := catalog.Descriptor(choice.Runtime); err != nil {
		return false, false
	}
	if profileCountForRuntime(state.working.Profiles, choice.Runtime) != 1 {
		return false, false
	}
	index := profileIndexForRuntime(state.working.Profiles, choice.Runtime, state.working.Fix.Profile)
	profile := state.working.Profiles[index]
	if state.probing[profile.ID] {
		return true, true
	}
	if probe, ok := state.probes[profile.ID]; ok && probe.State == agent.ProbeUnavailable {
		return false, false
	}
	return true, false
}

func agentConnectionLinesForState(state configSettingsState, catalog agent.ProfileCatalog, width int) ([]string, int, int) {
	choice := agentProviderChoice{Runtime: state.providerRuntime, Label: string(state.providerRuntime)}
	for _, candidate := range agentProviderChoices {
		if candidate.Runtime == state.providerRuntime {
			choice = candidate
			break
		}
	}
	descriptor, descriptorErr := profileDescriptor(catalog, agent.Profile{Runtime: choice.Runtime})
	if descriptorErr != nil {
		message := nonemptySetting(choice.Unavailable, "This agent adapter is not available in this Slopwatch build.")
		lines := wrappedDisabledConfigLines(message, width)
		return lines, 0, max(0, len(lines)-1)
	}

	count := profileCountForRuntime(state.working.Profiles, choice.Runtime)
	if count == 0 {
		lines := agentConnectionErrorLines("No connection configured. Add exactly one profile in the preferences file, then reopen Settings.", width)
		return lines, 0, max(0, len(lines)-1)
	}
	if count > 1 {
		lines := agentConnectionErrorLines("Multiple connections configured. This release supports one account per provider. In the preferences file, keep one profile and reopen Settings.", width)
		return lines, 0, max(0, len(lines)-1)
	}
	lines := make([]string, 0, 12)
	if descriptor.ConnectionInstructions != "" {
		lines = append(lines, wrappedDisabledConfigLines(descriptor.ConnectionInstructions, width)...)
	}
	if descriptor.DocumentationURL != "" {
		lines = append(lines, wrappedDisabledConfigLines(descriptor.DocumentationURL, width)...)
	}
	index := state.selectedProfileIndex()
	profile := state.working.Profiles[index]
	fields := state.profileEditorFields(catalog, profile)
	for fieldIndex, field := range fields {
		value := profileFieldValue(profile, field)
		if value == "" {
			value = field.Default
		}
		lines = append(lines, style.FormFieldRow("  "+field.Label, value, width, 24, fieldIndex == state.profileCursor, field.Kind == agent.ProfileFieldChoice))
		if fieldIndex == state.profileCursor {
			if field.Description != "" {
				lines = append(lines, wrappedDisabledConfigLines(field.Description, width)...)
			}
		}
	}
	statusStart := len(lines)
	lines = append(lines, "")
	if state.saving {
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentInfo).Render("SAVING ACTIVE CONNECTION…"))
		return lines, statusStart, len(lines) - 1
	}
	if state.connectionError != "" {
		title := nonemptySetting(state.connectionTitle, "CONNECTION FAILED")
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentCritical).Render(title))
		lines = append(lines, agentConnectionErrorLines(state.connectionError, width)...)
		return lines, statusStart, len(lines) - 1
	}
	if state.probing[profile.ID] {
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentInfo).Render("CHECKING CONNECTION…"))
		return lines, statusStart, len(lines) - 1
	}
	probe, probed := state.probes[profile.ID]
	if !probed {
		lines = append(lines, lipgloss.NewStyle().Width(width).Foreground(style.TextMuted).Render("Connection has not been checked."))
		return lines, statusStart, len(lines) - 1
	}
	if probe.State == agent.ProbeReady && probe.Capabilities.Isolation.EligibleForMutation() {
		detail := nonemptySetting(cleanAgentText(probe.Authentication.Label), "Connection ready")
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentPositive).Render("CONNECTED"))
		lines = append(lines, wrappedDisabledConfigLines(detail, width)...)
		return lines, statusStart, len(lines) - 1
	}
	lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentCritical).Render("CONNECTION FAILED"))
	diagnostic := nonemptySetting(cleanAgentText(probe.Diagnostic), agentProbeReadiness(probe))
	lines = append(lines, agentConnectionErrorLines(diagnostic, width)...)
	return lines, statusStart, len(lines) - 1
}

func agentConnectionErrorLines(value string, width int) []string {
	wrapped := wrapText(cleanAgentText(value), max(1, width), "", "")
	lines := make([]string, 0, len(wrapped))
	for _, logicalLine := range wrapped {
		for _, line := range strings.Split(ansi.Hardwrap(logicalLine, max(1, width), false), "\n") {
			lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentCritical).Render(line))
		}
	}
	return lines
}

func configSettingsLinesForState(state configSettingsState, catalog agent.ProfileCatalog, width int) []string {
	option := func(index int, label, value string) string {
		prefix := "  "
		if index == state.cursor {
			prefix = "› "
		}
		return style.FormFieldRow(prefix+label, value, width, 24, index == state.cursor, len(state.choices(index)) > 1)
	}
	switch state.kind {
	case configAgents:
		lines, _, _ := agentSettingsLinesForState(state, catalog, width)
		return lines
	case configFix:
		profile := nonemptySetting(string(state.working.Fix.Profile), "not selected")
		for _, configured := range state.working.Profiles {
			if configured.ID == state.working.Fix.Profile {
				profile = agentProfileChoiceLabel(configured) + " [" + string(configured.ID) + "]"
				break
			}
		}
		modelName := nonemptySetting(string(state.working.Fix.Model), "runtime default")
		effort := nonemptySetting(string(state.working.Fix.Effort), "runtime default")
		lines := []string{
			option(0, "Target score", fmt.Sprintf("%.0f", state.working.Fix.TargetScore)),
			option(1, "May edit", changeScopeLabel(state.working.Fix.ChangeScope)), option(2, "Agent profile", profile),
			option(3, "Model", modelName), option(4, "Effort", effort),
			option(fixSettingsPromptRow, "Agent prompt", firstPromptLine(state.working.Fix.PromptTemplate)),
		}
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Background(style.SurfaceScreen).Foreground(style.TextPrimary).Render("Focus metrics"))
		lines = append(lines, fixSettingsMetricGrid(state, width)...)
		return lines
	case configConcurrency:
		lines := []string{
			option(0, "Running agents", fmt.Sprint(state.working.Concurrency.MaxAgents)),
			option(1, "Running verifiers", fmt.Sprint(state.working.Concurrency.MaxVerifiers)),
			option(2, "Actors per job", fmt.Sprint(state.working.Concurrency.MaxActorsPerJob)),
			option(3, "Candidate preview bytes", fmt.Sprint(state.working.Concurrency.MaxCandidatePreviewBytes)),
			option(4, "Candidate preview lines", fmt.Sprint(state.working.Concurrency.MaxCandidatePreviewLines)),
		}
		return lines
	case configDelivery:
		branchTemplate := strings.TrimSpace(state.working.Delivery.BranchTemplate)
		branchValue := branchTemplate + " → " + appconfig.PreviewBranchTemplate(branchTemplate)
		values := map[int]struct{ label, value string }{
			deliverySettingWorkspace:   {"Work in", workspaceModeLabel(state.working.Delivery.DefaultPlan.Workspace)},
			deliverySettingGit:         {"Git", gitModeLabel(state.working.Delivery.DefaultPlan.Git)},
			deliverySettingPublish:     {"Publish", publishModeLabel(state.working.Delivery.DefaultPlan.Publish)},
			deliverySettingRemote:      {"Remote", state.working.Delivery.Remote},
			deliverySettingBase:        {"Base branch", nonemptySetting(state.working.Delivery.BaseBranch, "not set")},
			deliverySettingBranch:      {"Branch template", branchValue},
			deliverySettingPRState:     {"Initial PR state", map[bool]string{true: "Draft", false: "Ready for review"}[state.working.Delivery.DraftPullRequests]},
			deliverySettingCommitTitle: {"Commit title", nonemptySetting(state.working.Delivery.CommitTitleTemplate, "default")},
			deliverySettingCommitBody:  {"Commit body", nonemptySetting(state.working.Delivery.CommitBodyTemplate, "default")},
			deliverySettingPRTitle:     {"PR title", nonemptySetting(state.working.Delivery.PullRequestTitleTemplate, "commit title")},
			deliverySettingPRBody:      {"PR body", nonemptySetting(state.working.Delivery.PullRequestBodyTemplate, "commit body")},
		}
		fields := deliverySettingFields(state.working.Delivery)
		lines := make([]string, 0, len(fields))
		for row, field := range fields {
			item := values[field]
			lines = append(lines, option(row, item.label, item.value))
		}
		return lines
	}
	return nil
}

func fixSettingsMetricGrid(state configSettingsState, width int) []string {
	type metricChoice struct {
		label    string
		selected bool
		cursor   int
	}
	choices := make([]metricChoice, 0, len(fixSettingsMetrics()))
	for index, metric := range fixSettingsMetrics() {
		choices = append(choices, metricChoice{
			label: strings.ToUpper(string(metric)), selected: hasMetric(state.working.Fix.Focus, metric),
			cursor: fixSettingsMetricStart + index,
		})
	}
	const columns = 3
	cellWidth := max(1, width/columns)
	lines := make([]string, 0, (len(choices)+columns-1)/columns)
	for start := 0; start < len(choices); start += columns {
		line := ""
		for column := 0; column < columns; column++ {
			index := start + column
			currentWidth := cellWidth
			if column == columns-1 {
				currentWidth = max(1, width-cellWidth*(columns-1))
			}
			text := ""
			selected := false
			if index < len(choices) {
				mark := " "
				if choices[index].selected {
					mark = "x"
				}
				text = fmt.Sprintf("[%s]", mark)
				selected = choices[index].cursor == state.cursor
				line += style.ToggleOption(text, choices[index].label, selected, false, currentWidth)
				continue
			}
			line += lipgloss.NewStyle().Width(currentWidth).Background(style.SurfaceScreen).Render(text)
		}
		lines = append(lines, line)
	}
	return lines
}

func fixSettingsMetrics() []fix.MetricID {
	metrics := make([]fix.MetricID, 1, len(scoring.Metrics())+1)
	metrics[0] = fix.MetricScore
	for _, metric := range scoring.Metrics() {
		metrics = append(metrics, fix.MetricID(metric.ID))
	}
	return metrics
}

func agentProfileChoiceLabel(profile agent.Profile) string {
	switch profile.Runtime {
	case "codex-cli":
		return "Codex"
	case "openai-responses":
		return "OpenAI API"
	default:
		return fmt.Sprintf("%s · %s", profile.Label, profile.Runtime)
	}
}
