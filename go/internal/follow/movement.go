package follow

import (
	"time"
)

func mergeRows(model *Model, result analysisResult, oldScores map[string]float64, oldRanks map[string]int, now time.Time, baseline bool) {
	if result.previous != nil {
		oldScores, oldRanks = map[string]float64{}, map[string]int{}
		for _, file := range result.previous {
			oldScores[file.Path], oldRanks[file.Path] = file.Score, file.Rank
		}
	}
	newRows := make(map[string]rowState, len(model.files.Document.Files))
	for _, file := range model.files.Document.Files {
		if result.progress {
			newRows[file.Path] = model.files.Rows[file.Path]
			continue
		}
		newRows[file.Path] = mergeRowState(file, model.files.Rows[file.Path], result, oldScores, oldRanks, now, baseline, model.options.TrendWindow)
	}
	model.files.Rows = newRows
}
