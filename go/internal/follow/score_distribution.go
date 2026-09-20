package follow

import (
	"math"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

const (
	scoreDistributionBuckets = 21 // 0..4 through 95..99, plus the 100+ overflow bucket
	scoreDistributionLevels  = 8
)

// scoreDistribution keeps the aggregate independent of table order, limit,
// and search state. The fixed-size array also makes Model value copies safe.
type scoreDistribution struct {
	bins        [scoreDistributionBuckets]int
	currentBins [scoreDistributionBuckets]int
	unavailable int
	total       int
	ready       bool
}

type scoreDistributionColumn struct {
	count   int
	current bool
	bucket  int
}

// ANSI 256 colours are ordered from green through yellow and orange to red.
// Repeated end colours keep high-score buckets legible when the range is hot.
var scoreDistributionPalette = [...]uint{
	46, 46, 82, 118, 154, 190, 226, 226, 226, 226, 226,
	220, 214, 208, 208, 208, 208, 202, 196, 196, 196,
}

func scoreDistributionBucket(score float64) int {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return -1
	}
	if score < 0 {
		score = 0
	}
	if score >= 100 {
		return scoreDistributionBuckets - 1
	}
	return min(scoreDistributionBuckets-1, int(score/5))
}

func scoreDistributionSample(file report.File) (int, bool) {
	if len(file.PendingComponents) > 0 || metricFailed(file, "score") {
		return -1, false
	}
	bucket := scoreDistributionBucket(file.Score)
	return bucket, bucket >= 0
}

func buildScoreDistribution(files []report.File) scoreDistribution {
	distribution := scoreDistribution{ready: true}
	for _, file := range files {
		distribution.total++
		if bucket, available := scoreDistributionSample(file); available {
			distribution.bins[bucket]++
			if scoreDistributionCurrent(file) {
				distribution.currentBins[bucket]++
			}
		} else {
			distribution.unavailable++
		}
	}
	return distribution
}

func scoreDistributionCurrent(file report.File) bool {
	return file.Freshness == "" || file.Freshness == report.FreshnessCurrent
}

func (state *FilesState) rebuildScoreDistribution() {
	state.ScoreDistribution = buildScoreDistribution(state.Document.Files)
}

func (state *FilesState) ensureScoreDistribution() {
	if !state.ScoreDistribution.ready {
		state.rebuildScoreDistribution()
	}
}

func removeScoreDistributionSample(distribution *scoreDistribution, file report.File) {
	distribution.total = max(0, distribution.total-1)
	if bucket, available := scoreDistributionSample(file); available {
		distribution.bins[bucket] = max(0, distribution.bins[bucket]-1)
		if scoreDistributionCurrent(file) {
			distribution.currentBins[bucket] = max(0, distribution.currentBins[bucket]-1)
		}
	} else {
		distribution.unavailable = max(0, distribution.unavailable-1)
	}
}

func addScoreDistributionSample(distribution *scoreDistribution, file report.File) {
	distribution.total++
	if bucket, available := scoreDistributionSample(file); available {
		distribution.bins[bucket]++
		if scoreDistributionCurrent(file) {
			distribution.currentBins[bucket]++
		}
	} else {
		distribution.unavailable++
	}
}

func updateScoreDistributionFreshness(distribution *scoreDistribution, before, after report.File) {
	if !distribution.ready || scoreDistributionCurrent(before) == scoreDistributionCurrent(after) {
		return
	}
	bucket, available := scoreDistributionSample(before)
	if !available {
		return
	}
	if scoreDistributionCurrent(before) {
		distribution.currentBins[bucket] = max(0, distribution.currentBins[bucket]-1)
	}
	if scoreDistributionCurrent(after) {
		distribution.currentBins[bucket]++
	}
}

// replaceScoreDistribution updates only paths returned by an incremental
// analysis. A full document load or reweight calls rebuildScoreDistribution.
// current contains the projected result files, so this does not scan the
// complete document on a refresh.
func (state *FilesState) replaceScoreDistribution(previous map[string]report.File, current []report.File, affected map[string]bool) {
	state.ensureScoreDistribution()
	for path := range affected {
		if file, exists := previous[path]; exists {
			removeScoreDistributionSample(&state.ScoreDistribution, file)
		}
	}
	for _, file := range current {
		path := filepath.ToSlash(file.Path)
		if affected[path] {
			addScoreDistributionSample(&state.ScoreDistribution, file)
		}
	}
}

func scoreDistributionColour(bucket int, current bool, count int) lipgloss.TerminalColor {
	if count == 0 {
		return style.TextMuted
	}
	if !current {
		return lipgloss.ANSIColor(245)
	}
	bucket = min(scoreDistributionBuckets-1, max(0, bucket))
	return lipgloss.ANSIColor(scoreDistributionPalette[bucket])
}

func scoreDistributionBlock(level int) string {
	if level <= 0 {
		return " "
	}
	if level > 8 {
		return "█"
	}
	return string([]rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}[level-1])
}

func scoreDistributionColumns(distribution scoreDistribution, width int) []scoreDistributionColumn {
	if width <= 0 {
		return nil
	}
	columns := make([]scoreDistributionColumn, width)
	for column := range columns {
		start, end := 0, 0
		if width < scoreDistributionBuckets {
			if column == width-1 {
				start, end = scoreDistributionBuckets-1, scoreDistributionBuckets
			} else {
				normalColumns := width - 1
				start = column * (scoreDistributionBuckets - 1) / normalColumns
				end = (column + 1) * (scoreDistributionBuckets - 1) / normalColumns
			}
		} else {
			start = column * scoreDistributionBuckets / width
			end = (column + 1) * scoreDistributionBuckets / width
			if end <= start {
				end = min(scoreDistributionBuckets, start+1)
			}
		}
		for bucket := start; bucket < end; bucket++ {
			columns[column].count += distribution.bins[bucket]
			columns[column].current = columns[column].current || distribution.currentBins[bucket] == distribution.bins[bucket]
		}
		if end > start {
			columns[column].bucket = start
		}
		for bucket := start; bucket < end; bucket++ {
			if distribution.bins[bucket] > 0 && distribution.currentBins[bucket] != distribution.bins[bucket] {
				columns[column].current = false
				break
			}
		}
	}
	return columns
}

func scoreDistributionAxis(width int) string {
	if width <= 0 {
		return ""
	}
	axis := []rune(strings.Repeat(" ", width))
	labels := []string{"⁰", "²⁵", "⁵⁰", "⁷⁵", "¹⁰⁰⁺"}
	barWidth := max(1, width-3)
	positions := []int{0, 5 * barWidth / 20, 10 * barWidth / 20, 15 * barWidth / 20, width - lipgloss.Width(labels[4])}
	placed := [][2]int{}
	for _, index := range []int{0, 4, 2, 1, 3} {
		label := labels[index]
		labelWidth := lipgloss.Width(label)
		start := positions[index]
		if start < 0 || start+labelWidth > width {
			continue
		}
		overlaps := false
		for _, interval := range placed {
			if start <= interval[1]+1 && start+labelWidth-1 >= interval[0]-1 {
				overlaps = true
				break
			}
		}
		if overlaps {
			continue
		}
		copy(axis[start:], []rune(label))
		placed = append(placed, [2]int{start, start + labelWidth - 1})
	}
	return string(axis)
}

func renderScoreDistribution(model Model, width int) (string, string) {
	distribution := model.files.ScoreDistribution
	if !distribution.ready || width < 6 {
		return "", ""
	}
	// The graph has no text prefix: its fixed position directly after the
	// logo makes the score range legible without consuming chart cells.
	prefix := ""
	barWidth := width - 3
	if barWidth < 6 {
		barWidth = width
	}
	columns := scoreDistributionColumns(distribution, barWidth)
	maximum := 0
	for _, column := range columns {
		maximum = max(maximum, column.count)
	}
	if barWidth < 6 {
		return "", ""
	}
	heights := make([]int, len(columns))
	if maximum > 0 {
		for index, column := range columns {
			if column.count > 0 {
				heights[index] = max(1, (column.count*scoreDistributionLevels+maximum-1)/maximum)
			}
		}
	}
	bar := func() string {
		parts := make([]string, 0, len(columns))
		for index, column := range columns {
			level := min(scoreDistributionLevels, max(0, heights[index]))
			parts = append(parts, lipgloss.NewStyle().Foreground(scoreDistributionColour(column.bucket, column.current, column.count)).Background(style.SurfaceTop).Render(scoreDistributionBlock(level)))
		}
		return strings.Join(parts, "")
	}
	label := lipgloss.NewStyle().Foreground(style.TextMuted).Background(style.SurfaceTop)
	bars := bar()
	axis := scoreDistributionAxis(barWidth + min(3, max(0, width-lipgloss.Width(prefix)-barWidth)))
	fullTop := label.Render(prefix) + bars + label.Render(strings.Repeat(" ", max(0, lipgloss.Width(axis)-barWidth)))
	fullBottom := label.Render(strings.Repeat(" ", lipgloss.Width(prefix))) + label.Render(axis)
	if lipgloss.Width(fullBottom) <= width {
		return fullTop, fullBottom
	}
	return "", ""
}

func scoreDistributionBlank(width int) (string, string) {
	if width < 6 {
		return "", ""
	}
	background := lipgloss.NewStyle().Foreground(style.TextMuted).Background(style.SurfaceTop)
	return background.Render(strings.Repeat(" ", width)), background.Render(strings.Repeat(" ", width))
}
