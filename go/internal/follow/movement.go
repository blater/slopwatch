package follow

import (
	"time"
)

func mergeRows(model *Model, result analysisResult, oldScores map[string]float64, oldRanks map[string]int, now time.Time, baseline bool) {
	newRows := make(map[string]rowState, len(model.document.Files))
	for _, file := range model.document.Files {
		newRows[file.Path] = model.mergeRowState(file, model.rows[file.Path], result, oldScores, oldRanks, now, baseline)
	}
	model.rows = newRows
}
