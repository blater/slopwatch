package follow

import (
	"time"
)

func mergeRows(model *Model, result analysisResult, oldScores map[string]float64, oldRanks map[string]int, now time.Time, baseline bool) {
	newRows := make(map[string]rowState, len(model.files.Document.Files))
	for _, file := range model.files.Document.Files {
		newRows[file.Path] = mergeRowState(file, model.files.Rows[file.Path], result, oldScores, oldRanks, now, baseline, model.options.TrendWindow)
	}
	model.files.Rows = newRows
}
