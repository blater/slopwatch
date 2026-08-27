package agent

import (
	"time"

	"github.com/blater/slopmochi/internal/fix"
)

type EventKind string

const (
	EventStarted         EventKind = "started"
	EventActivity        EventKind = "activity"
	EventCommandStarted  EventKind = "command_started"
	EventCommandFinished EventKind = "command_finished"
	EventFileChanged     EventKind = "file_changed"
	EventUsage           EventKind = "usage"
	EventWarning         EventKind = "warning"
	EventRuntimeMessage  EventKind = "runtime_message"
)

type Usage struct {
	InputTokens     int64
	CachedTokens    int64
	OutputTokens    int64
	ReasoningTokens int64
	Cumulative      bool
}

type Event struct {
	JobID         fix.JobID
	AttemptID     fix.AttemptID
	Sequence      uint64
	At            time.Time
	Kind          EventKind
	Summary       string
	CommandID     string
	ActorID       string
	ParentActorID string
	Path          fix.RepoPath
	Usage         *Usage
}

type EventSink interface {
	Emit(Event) error
}

type EventSinkFunc func(Event) error

func (function EventSinkFunc) Emit(event Event) error { return function(event) }
