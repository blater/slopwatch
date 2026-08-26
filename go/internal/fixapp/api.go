// Package fixapp coordinates concurrent remediation jobs behind the sole API
// used by the TUI. Concrete providers, Git, analysis caches, and preference
// files remain behind injected ports.
package fixapp

import (
	"context"
	"errors"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/validation"
)

var (
	ErrClosed           = errors.New("fix service closed")
	ErrJobNotFound      = errors.New("fix job not found")
	ErrTargetReserved   = errors.New("fix target is reserved")
	ErrStaleRevision    = errors.New("stale job revision")
	ErrActionNotAllowed = errors.New("job cannot be canceled in its current state")
)

type GlobalRevision uint64

type PrepareRequest struct {
	Workspace fix.WorkspaceIdentity
	Targets   []fix.RepoPath
	Overrides appconfig.SessionOverrides
	Delivery  *PrepareDelivery
}

// PrepareDelivery carries the exact dialog selection into authoritative
// preflight. It is deliberately separate from persisted preferences: a
// readiness recheck must validate the session's edited tuple, not the saved
// default that happened to seed the form.
type PrepareDelivery struct {
	Mode   fix.DeliveryMode
	Branch string
}

type FixDraft struct {
	ID                        fix.DraftID
	Revision                  uint64
	Workspace                 fix.WorkspaceIdentity
	Targets                   []fix.RepoPath
	Baseline                  fixanalysis.BaselineSnapshot
	Preferences               appconfig.Resolved
	Profile                   agent.Profile
	Probe                     agent.ProbeResult
	Model                     agent.ModelID
	Effort                    agent.EffortID
	Delegation                agent.DelegationMode
	TargetScore               float64
	Focus                     []fix.MetricGoal
	ChangeScope               string
	AllowedPaths              []fix.RepoPath
	ValidationPlanID          string
	ValidationReadiness       validation.Readiness
	ValidationReadinessByPlan map[string]validation.Readiness
	DeliveryMode              fix.DeliveryMode
	DeliveryTarget            delivery.PreflightResult
	BranchName                string
	Instructions              agent.InstructionDocument
	Preflight                 candidate.PreflightResult
}

type SubmitRequest struct {
	Draft FixDraft
}

// DraftEdits is the provider-neutral mutable part of a prepared draft. The
// service layer reapplies these values to the scoring and instruction
// contracts together so callers cannot create a split-brain draft.
type DraftEdits struct {
	TargetScore      float64
	Focus            []fix.MetricGoal
	ChangeScope      string
	ValidationPlanID string
	DeliveryMode     fix.DeliveryMode
	BranchName       string
}

type JobFilter struct {
	IncludeFinished bool
	ActiveOnly      bool
}

type JobListSnapshot struct {
	Revision GlobalRevision
	Jobs     []fix.JobPresentation
}

type CommandReceipt struct {
	RequestID fix.CommandID
	JobID     fix.JobID
	Accepted  bool
	Duplicate bool
	Revision  uint64
	Message   string
}

type DiffRequest struct {
	Offset int
	Limit  int
}

type DiffPage struct {
	Files       []candidate.DiffFile
	Offset      int
	NextOffset  int
	Complete    bool
	Fingerprint string
}

type LogCursor int

type LogEntry struct {
	At            time.Time
	Kind          agent.EventKind
	Summary       string
	ActorID       string
	ParentActorID string
	Usage         *agent.Usage
}

type LogPage struct {
	Entries   []LogEntry
	Next      LogCursor
	Complete  bool
	Truncated bool
}

// RuntimeLimits are the live scheduler/retention settings that may be updated
// after preferences are saved. Reducing a limit never cancels running work;
// it gates subsequent scheduling/admission and trims retained transcripts.
type RuntimeLimits struct {
	MaxAgents          int
	MaxVerifiers       int
	MaxRetainedJobs    int
	MaxTranscriptBytes int64
}

func RuntimeLimitsFromConcurrency(value appconfig.Concurrency) RuntimeLimits {
	return RuntimeLimits{MaxAgents: value.MaxAgents, MaxVerifiers: value.MaxVerifiers, MaxRetainedJobs: value.MaxRetainedJobs, MaxTranscriptBytes: value.MaxTranscriptBytes}
}

type Subscription interface {
	Wait(context.Context, GlobalRevision) (GlobalRevision, error)
	Close() error
}

type Service interface {
	Prepare(context.Context, PrepareRequest) (FixDraft, error)
	Submit(context.Context, SubmitRequest) (fix.JobID, error)
	Jobs(JobFilter) JobListSnapshot
	Job(fix.JobID) (fix.JobPresentation, bool)
	Subscribe() Subscription
	Execute(context.Context, fix.JobCommand) (CommandReceipt, error)
	CandidateFile(context.Context, fix.JobID, fix.RepoPath) (candidate.File, error)
	Diff(context.Context, fix.JobID, DiffRequest) (DiffPage, error)
	Transcript(context.Context, fix.JobID, LogCursor, int) (LogPage, error)
	Reconfigure(context.Context, RuntimeLimits) error
	Shutdown(context.Context) error
}
