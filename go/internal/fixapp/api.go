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
)

var (
	ErrClosed           = errors.New("fix service closed")
	ErrJobNotFound      = errors.New("fix job not found")
	ErrTargetReserved   = errors.New("fix target is reserved")
	ErrActionNotAllowed = errors.New("job cannot be canceled in its current state")
)

type LoadRequest struct {
	Workspace fix.WorkspaceIdentity
	Targets   []fix.RepoPath
	Overrides appconfig.SessionOverrides
	Delivery  *LoadDelivery
}

type LoadDelivery struct {
	Plan   fix.DeliveryPlan
	Branch string
}

type FixInput struct {
	Workspace      fix.WorkspaceIdentity
	Targets        []fix.RepoPath
	Baseline       fixanalysis.BaselineSnapshot
	Preferences    appconfig.Resolved
	Profile        agent.Profile
	Probe          agent.ProbeResult
	Model          agent.ModelID
	Effort         agent.EffortID
	TargetScore    float64
	Focus          []fix.MetricGoal
	ChangeScope    string
	AllowedPaths   []fix.RepoPath
	DeliveryPlan   fix.DeliveryPlan
	DeliveryTarget delivery.PreflightResult
	BranchName     string
	Instructions   agent.InstructionDocument
	PlannedPaths   []fix.RepoPath
}

type FormValues struct {
	TargetScore  float64
	Focus        []fix.MetricGoal
	ChangeScope  string
	DeliveryPlan fix.DeliveryPlan
	BranchName   string
}

type JobFilter struct {
	IncludeFinished bool
	ActiveOnly      bool
}

type JobListSnapshot struct {
	Jobs []fix.JobPresentation
}

type CommandReceipt struct {
	RequestID fix.CommandID
	JobID     fix.JobID
	Accepted  bool
	Duplicate bool
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
	Text          string
	ActorID       string
	ParentActorID string
	Usage         *agent.Usage
}

type LogPage struct {
	Entries  []LogEntry
	Next     LogCursor
	Complete bool
}

// RuntimeLimits are the live scheduler settings that may be updated after
// preferences are saved. Reducing a limit never cancels running work.
type RuntimeLimits struct {
	MaxAgents    int
	MaxVerifiers int
}

func RuntimeLimitsFromConcurrency(value appconfig.Concurrency) RuntimeLimits {
	return RuntimeLimits{MaxAgents: value.MaxAgents, MaxVerifiers: value.MaxVerifiers}
}

type Subscription interface {
	Wait(context.Context) error
	Close() error
}

type Service interface {
	LoadFix(context.Context, LoadRequest) (FixInput, error)
	Run(context.Context, FixInput) (fix.JobID, error)
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
