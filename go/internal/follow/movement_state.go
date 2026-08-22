package follow

import (
	"time"

	"github.com/blater/slopwatch/internal/report"
)

func (model *Model) mergeRowState(file report.File, state rowState, result analysisResult, oldScores map[string]float64, oldRanks map[string]int, now time.Time, baseline bool) rowState {
	previousScore, existed := oldScores[file.Path]
	analyzed := result.full || contains(result.replace, file.Path)
	scoreChanged := existed && analyzed && file.Score != previousScore
	if !baseline {
		state = updateNewFileState(state, file, existed, analyzed, now)
	}
	if scoreChanged {
		state.editedAt = now
		state.direction = compareScore(file.Score, previousScore)
	}
	state = appendRankPoint(state, file.Rank, oldRanks[file.Path], now)
	state.ranks = pruneRanks(state.ranks, now, model.options.TrendWindow)
	if scoreChanged {
		state.scoreChangedAt = now
		state.movementDelta = rankMovement(state.ranks, file.Rank)
	}
	return state
}

func updateNewFileState(state rowState, file report.File, existed, analyzed bool, now time.Time) rowState {
	if !existed && analyzed {
		state.newFileAt = now
		state.newFileRank = file.Rank
		state.newFileMoved = false
	}
	if !state.newFileAt.IsZero() && file.Rank != state.newFileRank {
		state.newFileMoved = true
	}
	return state
}

func appendRankPoint(state rowState, rank, oldRank int, now time.Time) rowState {
	if len(state.ranks) == 0 && oldRank != 0 {
		state.ranks = append(state.ranks, rankPoint{at: now, rank: oldRank})
	}
	if len(state.ranks) == 0 || state.ranks[len(state.ranks)-1].rank != rank {
		state.ranks = append(state.ranks, rankPoint{at: now, rank: rank})
	}
	return state
}

func pruneRanks(ranks []rankPoint, now time.Time, window time.Duration) []rankPoint {
	cutoff := now.Add(-window)
	first := 0
	for first+1 < len(ranks) && ranks[first].at.Before(cutoff) {
		first++
	}
	return ranks[first:]
}

func rankMovement(ranks []rankPoint, currentRank int) int {
	if len(ranks) <= 1 {
		return 0
	}
	return ranks[0].rank - currentRank
}
