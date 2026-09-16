package follow

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/fix"
)

type jobMonitorKeyAction int

const (
	jobMonitorKeyNone jobMonitorKeyAction = iota
	jobMonitorKeyClose
	jobMonitorKeyNext
	jobMonitorKeyLog
	jobMonitorKeyDiff
	jobMonitorKeyCancel
)

type jobMonitorKeyResult struct {
	action jobMonitorKeyAction
	jobID  fix.JobID
	path   fix.RepoPath
}

func (state *jobMonitorState) apply(message jobMonitorMsg, width, height int, fullScreen bool) bool {
	state.loading = false
	state.refreshing = false
	if !message.found {
		state.errorText = "Job is no longer available"
	} else if message.err != nil {
		state.errorText = message.err.Error()
	} else {
		state.errorText = ""
	}
	if message.found {
		state.job = message.job
	}
	maximum := jobMonitorMaxOffset(*state, width, height, fullScreen)
	state.offset = min(state.offset, maximum)
	if state.pending {
		state.pending = false
		return true
	}
	return false
}

func (state *jobMonitorState) handleKey(key tea.KeyMsg, height, maximum int, jobs []fix.JobPresentation) jobMonitorKeyResult {
	switch key.String() {
	case "esc", "q":
		return jobMonitorKeyResult{action: jobMonitorKeyClose}
	case "up", "k":
		state.offset = max(0, state.offset-1)
	case "down", "j":
		state.offset = min(maximum, state.offset+1)
	case "pgup", "ctrl+b":
		state.offset = max(0, state.offset-max(1, height-6))
	case "pgdown", "ctrl+f":
		state.offset = min(maximum, state.offset+max(1, height-6))
	case "[", "]":
		if len(jobs) == 0 {
			return jobMonitorKeyResult{}
		}
		index := 0
		for candidate := range jobs {
			if jobs[candidate].ID == state.jobID {
				index = candidate
			}
		}
		delta := 1
		if key.String() == "[" {
			delta = -1
		}
		index = fixCycleIndex(index, delta, len(jobs))
		return jobMonitorKeyResult{action: jobMonitorKeyNext, jobID: jobs[index].ID}
	case "l":
		return jobMonitorKeyResult{action: jobMonitorKeyLog, jobID: state.jobID}
	case "d":
		return jobMonitorKeyResult{action: jobMonitorKeyDiff, jobID: state.jobID, path: state.focusPath}
	case "C":
		return jobMonitorKeyResult{action: jobMonitorKeyCancel, jobID: state.jobID}
	}
	return jobMonitorKeyResult{}
}

type jobReaderKeyAction int

const (
	jobReaderKeyNone jobReaderKeyAction = iota
	jobReaderKeyClose
	jobReaderKeyRefresh
	jobReaderKeyReopen
)

func (state *jobReaderState) apply(message jobReaderMsg, width, height int, fullScreen bool) bool {
	state.loading = false
	state.refreshing = false
	if message.err != nil {
		state.errorText = message.err.Error()
	} else {
		state.errorText = ""
		if message.increment {
			state.lines = append(state.lines, message.lines...)
		} else {
			state.lines = append([]string(nil), message.lines...)
		}
		state.logCursor = message.logCursor
		state.truncated = message.truncated
	}
	state.clamp(width, height, fullScreen)
	if state.pending {
		state.pending = false
		return true
	}
	return false
}

func (state *jobReaderState) handleKey(key tea.KeyMsg, width, height int, fullScreen bool) jobReaderKeyAction {
	page := state.pageSize(width, height, fullScreen)
	maximum := state.maxOffset(page)
	switch key.String() {
	case "esc", "q":
		*state = jobReaderState{}
		return jobReaderKeyClose
	case "up", "k":
		state.offset = max(0, state.offset-1)
		state.follow = false
	case "down", "j":
		state.offset = min(maximum, state.offset+1)
		state.follow = state.offset == maximum
	case "pgup", "ctrl+b":
		state.offset = max(0, state.offset-page)
		state.follow = false
	case "pgdown", "ctrl+f":
		state.offset = min(maximum, state.offset+page)
		state.follow = state.offset == maximum
	case "home", "g":
		state.offset = 0
		state.follow = false
	case "end", "G":
		state.offset = maximum
		state.follow = state.kind == OverlayJobLog
	case "left", "h":
		state.horizontal = max(0, state.horizontal-pathScrollStep)
	case "right", "l":
		state.horizontal = min(state.maxHorizontalOffset(width, height, fullScreen), state.horizontal+pathScrollStep)
	case "r":
		if state.kind == OverlayJobLog {
			return jobReaderKeyRefresh
		}
		return jobReaderKeyReopen
	}
	return jobReaderKeyNone
}
