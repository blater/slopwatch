package follow

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/slopslap/slopslap/internal/report"
)

type Options struct {
	Workspace    string
	Targets      []string
	Languages    []string
	IncludeTests bool
	Limit        int
	TrendWindow  time.Duration
	Compact      bool
}

type Analyzer interface {
	Analyze(context.Context, []string, []string) (report.Document, error)
}

type analysisResult struct {
	document report.Document
	replace  []string
	full     bool
	err      error
}

type animationTick time.Time

type rowState struct {
	editedAt  time.Time
	direction int
	rankDelta int
	ranks     []rankPoint
}

type rankPoint struct {
	at   time.Time
	rank int
}

type Model struct {
	analyzer     Analyzer
	watcher      *sourceWatcher
	options      Options
	document     report.Document
	rows         map[string]rowState
	width        int
	height       int
	cursor       int
	offset       int
	selected     string
	analyzing    bool
	queued       map[string]bool
	status       string
	detail       bool
	detailOffset int
	columns      bool
	columnCursor int
	sortOpen     bool
	sortCursor   int
	sortKey      string
	sortReverse  bool
	pendingSort  bool
	visible      map[string]bool
}

func New(document report.Document, analyzer Analyzer, options Options) (*Model, error) {
	watcher, err := newSourceWatcher(
		options.Workspace, options.Targets, options.IncludeTests, options.Languages,
	)
	if err != nil {
		return nil, err
	}
	document.SortAndRank()
	rows := make(map[string]rowState, len(document.Files))
	now := time.Now()
	for _, file := range document.Files {
		rows[file.Path] = rowState{ranks: []rankPoint{{at: now, rank: file.Rank}}}
	}
	if options.TrendWindow <= 0 {
		options.TrendWindow = 15 * time.Minute
	}
	model := &Model{
		analyzer: analyzer, watcher: watcher, options: options, document: document,
		rows: rows, queued: map[string]bool{},
		sortKey: "score", sortReverse: true,
		visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
	}
	if len(document.Files) > 0 {
		model.selected = document.Files[0].Path
	}
	return model, nil
}

func (model *Model) Close() { model.watcher.close() }

func (model Model) Init() tea.Cmd {
	return tea.Batch(model.waitForChange(), tickAnimation())
}

func tickAnimation() tea.Cmd {
	return tea.Tick(time.Second, func(at time.Time) tea.Msg { return animationTick(at) })
}

func (model Model) waitForChange() tea.Cmd {
	return func() tea.Msg { return <-model.watcher.changes }
}

func (model Model) analyze(paths []string, full bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		targets := paths
		if full {
			targets = model.options.Targets
		}
		languages := languagesForPaths(paths)
		if full {
			languages = nil
		}
		document, err := model.analyzer.Analyze(ctx, targets, languages)
		return analysisResult{document: document, replace: paths, full: full, err: err}
	}
}

func languagesForPaths(paths []string) []string {
	seen := map[string]bool{}
	for _, path := range paths {
		language := ""
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go":
			language = "go"
		case ".java":
			language = "java"
		case ".rs":
			language = "rust"
		case ".ts", ".tsx", ".mts", ".cts":
			language = "typescript"
		}
		if language != "" {
			seen[language] = true
		}
	}
	result := make([]string, 0, len(seen))
	for language := range seen {
		result = append(result, language)
	}
	sort.Strings(result)
	return result
}

func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
		model.ensureVisible()
		model.clampDetailOffset()
		return model, nil
	case sourceChange:
		command := model.waitForChange()
		if message.Err != nil {
			model.status = message.Err.Error()
			return model, command
		}
		for _, path := range message.Paths {
			model.queued[path] = true
			if state, exists := model.rows[path]; exists {
				state.editedAt = time.Now()
				state.direction = 0
				model.rows[path] = state
			}
		}
		if model.analyzing {
			return model, command
		}
		paths := model.takeQueue()
		model.analyzing = true
		return model, tea.Batch(command, model.analyzeExisting(paths))
	case analysisResult:
		model.analyzing = false
		if message.err != nil {
			model.status = message.err.Error()
		} else {
			model.status = ""
			model.merge(message)
		}
		if len(model.queued) > 0 {
			paths := model.takeQueue()
			model.analyzing = true
			return model, model.analyzeExisting(paths)
		}
		return model, nil
	case animationTick:
		model.expireTrends(time.Time(message))
		return model, tickAnimation()
	case tea.KeyMsg:
		return model.handleKey(message)
	}
	return model, nil
}

func (model *Model) expireTrends(now time.Time) {
	cutoff := now.Add(-model.options.TrendWindow)
	for path, state := range model.rows {
		first := 0
		for first+1 < len(state.ranks) && state.ranks[first].at.Before(cutoff) {
			first++
		}
		state.ranks = state.ranks[first:]
		if len(state.ranks) > 1 {
			state.rankDelta = state.ranks[0].rank - state.ranks[len(state.ranks)-1].rank
		} else {
			state.rankDelta = 0
		}
		model.rows[path] = state
	}
}

func (model Model) analyzeExisting(paths []string) tea.Cmd {
	existing := make([]string, 0, len(paths))
	for _, path := range paths {
		if info, err := os.Stat(filepath.Join(model.options.Workspace, filepath.FromSlash(path))); err == nil && !info.IsDir() {
			existing = append(existing, path)
		}
	}
	if len(existing) == 0 {
		return func() tea.Msg { return analysisResult{replace: paths, document: report.Document{}} }
	}
	command := model.analyze(existing, false)
	return func() tea.Msg {
		result := command()
		analysis := result.(analysisResult)
		analysis.replace = paths
		return analysis
	}
}

func (model *Model) takeQueue() []string {
	paths := make([]string, 0, len(model.queued))
	for path := range model.queued {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	model.queued = map[string]bool{}
	return paths
}

func (model *Model) merge(result analysisResult) {
	oldScores := map[string]float64{}
	oldRanks := map[string]int{}
	for _, file := range model.document.Files {
		oldScores[file.Path] = file.Score
		oldRanks[file.Path] = file.Rank
	}
	if result.full {
		model.document = result.document
	} else {
		replace := map[string]bool{}
		for _, path := range result.replace {
			replace[filepath.ToSlash(path)] = true
		}
		kept := make([]report.File, 0, len(model.document.Files)+len(result.document.Files))
		for _, file := range model.document.Files {
			if !replace[file.Path] {
				kept = append(kept, file)
			}
		}
		kept = append(kept, result.document.Files...)
		model.document.Files = kept
	}
	model.document.SortAndRank()
	now := time.Now()
	newRows := make(map[string]rowState, len(model.document.Files))
	for _, file := range model.document.Files {
		state := model.rows[file.Path]
		if _, changed := oldScores[file.Path]; changed && contains(result.replace, file.Path) {
			state.editedAt = now
			switch {
			case file.Score < oldScores[file.Path]:
				state.direction = -1
			case file.Score > oldScores[file.Path]:
				state.direction = 1
			default:
				state.direction = 0
			}
		}
		if len(state.ranks) == 0 {
			if oldRank, ok := oldRanks[file.Path]; ok {
				state.ranks = append(state.ranks, rankPoint{at: now, rank: oldRank})
			}
		}
		if len(state.ranks) == 0 || state.ranks[len(state.ranks)-1].rank != file.Rank {
			state.ranks = append(state.ranks, rankPoint{at: now, rank: file.Rank})
		}
		cutoff := now.Add(-model.options.TrendWindow)
		first := 0
		for first+1 < len(state.ranks) && state.ranks[first].at.Before(cutoff) {
			first++
		}
		state.ranks = state.ranks[first:]
		if len(state.ranks) > 1 {
			state.rankDelta = state.ranks[0].rank - file.Rank
		} else {
			state.rankDelta = 0
		}
		newRows[file.Path] = state
	}
	model.rows = newRows
	model.restoreSelection()
}

func contains(paths []string, path string) bool {
	for _, candidate := range paths {
		if filepath.ToSlash(candidate) == path {
			return true
		}
	}
	return false
}

func (model *Model) restoreSelection() {
	for index, file := range model.displayFiles() {
		if file.Path == model.selected {
			model.cursor = index
			model.ensureVisible()
			return
		}
	}
	if model.cursor >= len(model.displayFiles()) {
		model.cursor = max(0, len(model.displayFiles())-1)
	}
	if files := model.displayFiles(); len(files) > 0 {
		model.selected = files[model.cursor].Path
	} else {
		model.selected = ""
	}
	model.ensureVisible()
}

func (model Model) displayFiles() []report.File {
	files := append([]report.File(nil), model.document.Files...)
	sort.SliceStable(files, func(left, right int) bool {
		return model.less(files[left], files[right])
	})
	if model.options.Limit > 0 && len(files) > model.options.Limit {
		return files[:model.options.Limit]
	}
	return files
}

type sortField struct {
	key   string
	label string
}

func sortFields() []sortField {
	return []sortField{
		{"score", "Score"}, {"cog", "COG"},
		{"npath", "NPath"}, {"cyclo", "Cyclomatic"}, {"deep", "Deep"},
		{"god", "God"}, {"filename", "Filename"},
	}
}

func (model Model) less(left, right report.File) bool {
	if model.sortKey == "filename" {
		leftName := strings.ToLower(filepath.Base(left.Path))
		rightName := strings.ToLower(filepath.Base(right.Path))
		comparison := strings.Compare(leftName, rightName)
		if comparison == 0 {
			comparison = strings.Compare(strings.ToLower(left.Path), strings.ToLower(right.Path))
		}
		if model.sortReverse {
			return comparison > 0
		}
		return comparison < 0
	}
	leftValue, leftExists := model.sortValue(left)
	rightValue, rightExists := model.sortValue(right)
	if leftExists != rightExists {
		return leftExists // unavailable measurements always sort last
	}
	if leftValue == rightValue {
		return left.Path < right.Path
	}
	if model.sortReverse {
		return leftValue > rightValue
	}
	return leftValue < rightValue
}

func (model Model) sortValue(file report.File) (float64, bool) {
	switch model.sortKey {
	case "score":
		return file.Score, true
	case "cog", "npath", "cyclo", "deep", "god":
		value, exists, _ := metric(file, model.sortKey)
		return value, exists
	default:
		return float64(file.Rank), true
	}
}

func (model *Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := key.String()
	if model.detail {
		switch name {
		case "esc", "escape", "q":
			model.detail = false
			model.detailOffset = 0
		case "up", "k":
			model.detailOffset = max(0, model.detailOffset-1)
		case "down", "j":
			model.detailOffset = min(model.detailMaxOffset(), model.detailOffset+1)
		case "ctrl+f", "pgdown":
			model.detailOffset = min(
				model.detailMaxOffset(), model.detailOffset+max(1, model.detailBodyHeight()-1),
			)
		case "ctrl+b", "pgup":
			model.detailOffset = max(0, model.detailOffset-max(1, model.detailBodyHeight()-1))
		case "home", "g":
			model.detailOffset = 0
		case "end", "G":
			model.detailOffset = model.detailMaxOffset()
		}
		return model, nil
	}
	if model.columns {
		items := columnNames()
		switch name {
		case "esc", "escape", "q", "enter":
			model.columns = false
		case "up", "k":
			model.columnCursor = max(0, model.columnCursor-1)
		case "down", "j":
			model.columnCursor = min(len(items)-1, model.columnCursor+1)
		case " ":
			key := items[model.columnCursor].key
			model.visible[key] = !model.visible[key]
		}
		return model, nil
	}
	if model.sortOpen {
		items := sortFields()
		switch name {
		case "esc", "escape", "q":
			model.sortOpen = false
		case "enter":
			model.sortKey = items[model.sortCursor].key
			model.sortReverse = model.pendingSort
			if _, exists := model.visible[model.sortKey]; exists && !model.options.Compact {
				model.visible[model.sortKey] = true
			}
			model.sortOpen = false
			model.restoreSelection()
		case "up", "k":
			model.sortCursor = max(0, model.sortCursor-1)
		case "down", "j":
			model.sortCursor = min(len(items)-1, model.sortCursor+1)
		case "left", "h":
			model.pendingSort = false
		case "right", "l":
			model.pendingSort = true
		case " ":
			model.pendingSort = !model.pendingSort
		}
		return model, nil
	}
	switch name {
	case "ctrl+c", "q":
		return model, tea.Quit
	case "up", "k":
		model.move(-1)
	case "down", "j":
		model.move(1)
	case "ctrl+f", "pgdown":
		model.move(max(1, model.bodyHeight()))
	case "ctrl+b", "pgup":
		model.move(-max(1, model.bodyHeight()))
	case "home":
		model.cursor = 0
		model.selectCursor()
		model.ensureVisible()
	case "end":
		model.cursor = max(0, len(model.displayFiles())-1)
		model.selectCursor()
		model.ensureVisible()
	case "enter":
		if len(model.displayFiles()) > 0 {
			model.detail = true
		}
	case "c":
		model.columns = true
	case "s":
		model.sortOpen = true
		model.pendingSort = model.sortReverse
		for index, item := range sortFields() {
			if item.key == model.sortKey {
				model.sortCursor = index
				break
			}
		}
	case "r":
		if !model.analyzing {
			model.analyzing = true
			return model, model.analyze(nil, true)
		}
	}
	return model, nil
}

func (model *Model) move(delta int) {
	files := model.displayFiles()
	if len(files) == 0 {
		return
	}
	model.cursor = min(len(files)-1, max(0, model.cursor+delta))
	model.selectCursor()
	model.ensureVisible()
}

func (model *Model) selectCursor() {
	files := model.displayFiles()
	if model.cursor >= 0 && model.cursor < len(files) {
		model.selected = files[model.cursor].Path
	}
}

func (model Model) bodyHeight() int { return max(1, model.height-3) }

func (model *Model) ensureVisible() {
	page := model.bodyHeight()
	if model.cursor < model.offset {
		model.offset = model.cursor
	}
	if model.cursor >= model.offset+page {
		model.offset = model.cursor - page + 1
	}
	maxOffset := max(0, len(model.displayFiles())-page)
	model.offset = min(model.offset, maxOffset)
}

func (model Model) View() string {
	if model.width <= 0 || model.height <= 0 {
		return ""
	}
	base := model.tableView()
	if model.detail {
		return model.overlay(base, model.detailView())
	}
	if model.columns {
		return model.overlay(base, model.columnsView())
	}
	if model.sortOpen {
		return model.overlay(base, model.sortView())
	}
	return base
}

var (
	colourMuted        = lipgloss.Color("#668298")
	colourText         = lipgloss.Color("#d5e2eb")
	colourGreen        = lipgloss.Color("#58e7ad")
	colourAmber        = lipgloss.Color("#f0c765")
	colourRed          = lipgloss.Color("#ff8291")
	scoreAmber         = lipgloss.Color("#f5c451")
	scoreRed           = lipgloss.Color("#ff6174")
	colourBlue         = lipgloss.Color("#6fb9e8")
	selectedBackground = lipgloss.Color("#183b52")
	screenBackground   = lipgloss.Color("#071019")
	topBackground      = lipgloss.Color("#0a1622")
	headerBackground   = lipgloss.Color("#0b1e2d")
	footerBackground   = lipgloss.Color("#061019")
)

func ConfigureTerminalColours() {
	// This interface has a deliberate application palette. Textual renders that
	// palette even when NO_COLOR is inherited from the caller, so doing anything
	// different here makes the Go and Python dashboards visibly disagree.
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
}

func (model Model) tableView() string {
	lines := make([]string, 0, model.height)
	top := model.options.Workspace
	status := ""
	if model.analyzing {
		status = "SCANNING"
	} else if model.status != "" {
		status = model.status
	}
	if status != "" {
		top += "  " + status
	}
	topText := " " + truncate(top, max(0, model.width-2))
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#9fb4c6")).Background(topBackground).Render(padANSI(topText, model.width)))
	lines = append(lines, model.header())
	files := model.displayFiles()
	page := model.bodyHeight()
	for row := 0; row < page; row++ {
		index := model.offset + row
		if index >= len(files) {
			lines = append(lines, lipgloss.NewStyle().Background(screenBackground).Render(strings.Repeat(" ", model.width)))
			continue
		}
		lines = append(lines, model.renderRow(files[index], index == model.cursor))
	}
	lines = append(lines, model.footer())
	return strings.Join(lines, "\n")
}

func (model Model) footer() string {
	background := lipgloss.NewStyle().Background(footerBackground)
	result := background.Render(" ")
	for _, item := range [][2]string{{"c", "columns"}, {"s", "sort"}, {"r", "rescan"}, {"q", "quit"}} {
		result += lipgloss.NewStyle().Foreground(colourBlue).Background(footerBackground).Render(item[0])
		result += lipgloss.NewStyle().Foreground(colourMuted).Background(footerBackground).Render(" " + item[1] + "  ")
	}
	result = truncateANSI(result, model.width)
	if remaining := model.width - lipgloss.Width(result); remaining > 0 {
		result += background.Render(strings.Repeat(" ", remaining))
	}
	return result
}

type column struct {
	key, title string
	width      int
	right      bool
}

func columnNames() []column {
	return []column{{"cog", "COG", 7, false}, {"npath", "NPATH", 8, false}, {"cyclo", "CYCLO", 7, false}, {"deep", "DEEP", 4, false}, {"god", "GOD", 6, true}}
}

func (model Model) activeColumns() []column {
	if model.options.Compact {
		return nil
	}
	result := []column{}
	for _, column := range columnNames() {
		if model.visible[column.key] {
			result = append(result, column)
		}
	}
	return result
}

func (model Model) header() string {
	columns := []column{{key: "score", title: "SCORE", width: 7, right: true}}
	if !model.options.Compact {
		columns = append(columns, model.activeColumns()...)
	}
	heading := ""
	for index, column := range columns {
		title := column.title
		separator := ""
		if index > 0 {
			separator = " "
			if columns[index-1].key == "score" {
				separator = "  "
			}
		}
		if model.sortKey == column.key {
			marked := model.sortIndicator() + title
			if lipgloss.Width(marked) <= column.width {
				title = marked
			} else if index > 0 {
				separator = model.sortIndicator()
			}
		}
		heading += separator + pad(title, column.width, column.right)
	}
	heading = truncate(heading, model.width)
	if model.sortKey == "filename" && lipgloss.Width(heading) < model.width {
		heading += " " + model.sortIndicator()
	} else if !model.sortColumnVisible() && lipgloss.Width(heading) < model.width {
		heading += " " + model.sortIndicator() + model.sortTitle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#6f8ca2")).Background(headerBackground).Bold(true).Render(padANSI(heading, model.width))
}

func (model Model) sortColumnVisible() bool {
	if model.sortKey == "score" || model.sortKey == "filename" {
		return true
	}
	return model.visible[model.sortKey]
}

func (model Model) sortTitle() string {
	for _, field := range sortFields() {
		if field.key == model.sortKey {
			return strings.ToUpper(field.label)
		}
	}
	return ""
}

func (model Model) sortIndicator() string {
	if model.sortReverse {
		return "▼"
	}
	return "▲"
}

func (model Model) renderRow(file report.File, selected bool) string {
	state := model.rows[file.Path]
	background := screenBackground
	if selected {
		background = selectedBackground
	} else if !state.editedAt.IsZero() {
		if edited := editBackground(state, time.Now(), model.options.TrendWindow); edited != "" {
			background = edited
		}
	}
	separator := lipgloss.NewStyle().Background(background).Render(" ")
	arrow := " "
	if state.rankDelta > 0 {
		arrow = "↑"
	} else if state.rankDelta < 0 {
		arrow = "↓"
	}
	scoreWidth := 7
	if arrow != " " {
		scoreWidth--
	}
	score := styleCell(pad(decimalWithin(file.Score, scoreWidth), scoreWidth, true), scoreColour(file.Score), background)
	if arrow != " " {
		score += styleCell(arrow, directionColour(state.rankDelta), background)
	}
	parts := []string{score}
	activeColumns := model.activeColumns()
	for _, column := range activeColumns {
		value, exists, contribution := metric(file, column.key)
		text := "-"
		if exists {
			if column.key == "god" {
				text = report.OneDecimal(value)
			} else {
				text = report.DisplayNumber(value)
			}
		}
		_ = contribution
		colour := metricColour(column.key, value, exists)
		parts = append(parts, styleCell(pad(text, column.width, column.right), colour, background))
	}
	if len(activeColumns) > 0 {
		parts[0] += separator
		if activeColumns[len(activeColumns)-1].key == "god" {
			parts[len(parts)-1] += separator
		}
	}
	prefix := strings.Join(parts, separator) + separator
	pathWidth := max(0, model.width-lipgloss.Width(prefix))
	line := prefix + renderPath(file.Path, pathWidth, background)
	if remaining := model.width - lipgloss.Width(line); remaining > 0 {
		line += lipgloss.NewStyle().Background(background).Render(strings.Repeat(" ", remaining))
	}
	return line
}

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
		warm = value > 0
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
	componentID := map[string]string{"cog": "cognitive_complexity", "npath": "npath_complexity", "cyclo": "cyclomatic_method_complexity", "deep": "deeply_nested_if", "god": "god_class"}[key]
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

func (model Model) overlay(base, modal string) string {
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < model.height {
		baseLines = append(baseLines, strings.Repeat(" ", model.width))
	}
	modalLines := strings.Split(modal, "\n")
	modalWidth := lipgloss.Width(modal)
	modalHeight := len(modalLines)
	left := max(0, (model.width-modalWidth)/2)
	top := max(0, (model.height-modalHeight)/2)
	for index, modalLine := range modalLines {
		row := top + index
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLine := baseLines[row]
		if lipgloss.Width(baseLine) < model.width {
			baseLine = padANSI(baseLine, model.width)
		}
		modalLine = padANSI(modalLine, modalWidth)
		rightEdge := min(model.width, left+modalWidth)
		baseLines[row] = ansi.Cut(baseLine, 0, left) +
			ansi.Cut(modalLine, 0, rightEdge-left) +
			ansi.Cut(baseLine, rightEdge, model.width)
	}
	return strings.Join(baseLines[:model.height], "\n")
}

func (model Model) selectedFile() (report.File, bool) {
	files := model.displayFiles()
	if model.cursor < 0 || model.cursor >= len(files) {
		return report.File{}, false
	}
	return files[model.cursor], true
}

func (model Model) detailView() string {
	file, ok := model.selectedFile()
	if !ok {
		return ""
	}
	outerWidth, outerHeight, titleHeight, bodyHeight, contentWidth := model.detailDimensions()
	innerWidth := max(1, outerWidth-2)
	lines := model.detailContent(file, contentWidth)
	maximumOffset := max(0, len(lines)-bodyHeight)
	offset := min(maximumOffset, max(0, model.detailOffset))

	titleBackground := lipgloss.Color("#0b1e2d")
	bodyBackground := lipgloss.Color("#091723")
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#68f6c8")).Background(titleBackground)
	bodyStyle := lipgloss.NewStyle().Foreground(colourText).Background(bodyBackground)
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
		trackColour := lipgloss.Color("#31536b")
		if len(lines) > bodyHeight {
			track = "│"
			if row >= thumbStart && row < thumbStart+thumbSize {
				track = "█"
			}
		}
		inner = append(inner,
			bodyStyle.Render("  ")+
				content+
				lipgloss.NewStyle().Foreground(trackColour).Background(bodyBackground).Render(track)+
				bodyStyle.Render(" "),
		)
	}
	dialog := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#31506a")).Render(strings.Join(inner, "\n"))
	return dialog
}

type detailLine struct {
	text   string
	colour lipgloss.Color
	bold   bool
}

func (model Model) detailContent(file report.File, width int) []string {
	logical := []detailLine{
		{file.Path, lipgloss.Color("#d8e7f0"), true},
		{fmt.Sprintf("%s  ·  rank %d  ·  score %.1f", file.Language, file.Rank, file.Score), lipgloss.Color("#7893a7"), false},
		{"", colourText, false},
		{"METRIC SUMMARY", lipgloss.Color("#68f6c8"), true},
	}
	labels := []struct{ id, label string }{
		{"cognitive_complexity", "Cognitive complexity"},
		{"npath_complexity", "NPath complexity"},
		{"cyclomatic_method_complexity", "Cyclomatic method complexity"},
		{"cyclomatic_class_complexity", "Cyclomatic class complexity"},
		{"deeply_nested_if", "Deeply nested if findings"},
		{"god_class", "God class score"},
	}
	for _, item := range labels {
		component, exists := file.Components[item.id]
		if !exists {
			logical = append(logical, detailLine{fmt.Sprintf("%-24s unavailable", item.label), lipgloss.Color("#91aabd"), false})
			continue
		}
		values := make([]float64, 0, len(component.Subjects))
		for _, subject := range component.Subjects {
			values = append(values, subject.Value)
		}
		maximum, total := 0.0, 0.0
		for _, value := range values {
			maximum = math.Max(maximum, value)
			total += value
		}
		if item.id == "god_class" {
			logical = append(logical, detailLine{fmt.Sprintf("%-24s %.1f across %d types", item.label, component.Contribution, component.Observations), lipgloss.Color("#91aabd"), false})
		} else if item.id == "deeply_nested_if" {
			logical = append(logical, detailLine{fmt.Sprintf("%-24s %s findings", item.label, report.DisplayNumber(total)), lipgloss.Color("#91aabd"), false})
		} else {
			unit := "routines"
			if item.id == "cyclomatic_class_complexity" {
				unit = "types"
			}
			logical = append(logical, detailLine{fmt.Sprintf("%-24s maximum %s across %d %s", item.label, report.DisplayNumber(maximum), component.Observations, unit), lipgloss.Color("#91aabd"), false})
		}
	}
	logical = append(logical, detailLine{"", colourText, false}, detailLine{"COMPONENTS", lipgloss.Color("#68f6c8"), true})
	componentIDs := make([]string, 0, len(file.Components))
	for id := range file.Components {
		componentIDs = append(componentIDs, id)
	}
	sort.Strings(componentIDs)
	for _, id := range componentIDs {
		component := file.Components[id]
		logical = append(logical, detailLine{fmt.Sprintf("%s  contribution %.1f  ·  observations %d", id, component.Contribution, component.Observations), lipgloss.Color("#b9cad8"), true})
		for _, subject := range component.Subjects {
			logical = append(logical, detailLine{fmt.Sprintf("  • %s = %s  (+%s)", subject.Subject, report.DisplayNumber(subject.Value), report.DisplayNumber(subject.Contribution)), lipgloss.Color("#7893a7"), false})
		}
	}
	result := []string{}
	for _, line := range logical {
		wrapped := []string{""}
		if line.text != "" {
			wrapped = strings.Split(ansi.Hardwrap(line.text, max(1, width), false), "\n")
		}
		for _, part := range wrapped {
			style := lipgloss.NewStyle().Foreground(line.colour).Background(lipgloss.Color("#091723")).Bold(line.bold)
			result = append(result, style.Render(padANSI(clip(part, width), width)))
		}
	}
	return result
}

func (model Model) detailDimensions() (outerWidth, outerHeight, titleHeight, bodyHeight, contentWidth int) {
	outerWidth = min(model.width, max(4, int(math.Round(float64(model.width)*0.92))))
	outerHeight = min(model.height, max(4, int(math.Round(float64(model.height)*0.88))))
	innerWidth := max(1, outerWidth-2)
	innerHeight := max(2, outerHeight-2)
	titleHeight = min(3, max(1, innerHeight-1))
	bodyHeight = max(1, innerHeight-titleHeight)
	contentWidth = max(1, innerWidth-4)
	return
}

func (model Model) detailBodyHeight() int {
	_, _, _, bodyHeight, _ := model.detailDimensions()
	return bodyHeight
}

func (model Model) detailMaxOffset() int {
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

func (model Model) columnsView() string {
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(colourBlue).Render("Columns"), ""}
	for index, column := range columnNames() {
		cursor := "  "
		if index == model.columnCursor {
			cursor = "› "
		}
		mark := " "
		if model.visible[column.key] {
			mark = "✓"
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", cursor, mark, column.title))
	}
	lines = append(lines, "", "space toggle   Enter/Esc close")
	return lipgloss.NewStyle().Width(38).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(colourBlue).Background(lipgloss.Color("#0d1d29")).Foreground(colourText).Render(strings.Join(lines, "\n"))
}

func (model Model) sortView() string {
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(colourBlue).Render("SORT RESULTS"), ""}
	for index, item := range sortFields() {
		cursor := "  "
		if index == model.sortCursor {
			cursor = "› "
		}
		direction := "ascending"
		if model.pendingSort {
			direction = "descending"
		}
		line := fmt.Sprintf("%s%-12s", cursor, item.label)
		if index == model.sortCursor {
			line += lipgloss.NewStyle().Foreground(colourGreen).Render(direction)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "←/→ direction   Enter apply   Esc cancel")
	return lipgloss.NewStyle().Width(46).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(colourBlue).Background(lipgloss.Color("#0d1d29")).Foreground(colourText).Render(strings.Join(lines, "\n"))
}
