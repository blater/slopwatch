// Package appconfig is the typed preferences boundary used by application
// services and the TUI. Consumers never receive a file path or TOML document.
package appconfig

import (
	"context"
	"errors"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/validation"
)

type Revision uint64
type Scope string

const (
	ScopeUser       Scope = "user"
	ScopeRepository Scope = "repository"
)

type Origin string

const (
	OriginBuiltIn    Origin = "built_in"
	OriginUser       Origin = "user"
	OriginRepository Origin = "repository"
	OriginCLI        Origin = "cli"
	OriginSession    Origin = "session"
)

var ErrRevisionConflict = errors.New("preferences revision conflict")

type SessionOverrides struct {
	TargetScore *float64
	Profile     *agent.ProfileID
	Model       *agent.ModelID
	Effort      *agent.EffortID
	TrendWindow *time.Duration
}

type FixDefaults struct {
	TargetScore    float64
	Focus          []fix.MetricID
	ChangeScope    string
	Profile        agent.ProfileID
	Model          agent.ModelID
	Effort         agent.EffortID
	Delegation     agent.DelegationMode
	PromptTemplate string
	BranchTemplate string
	ValidationPlan string
}

type Concurrency struct {
	MaxAgents                int
	MaxVerifiers             int
	MaxRetainedJobs          int
	MaxTranscriptBytes       int64
	MaxActorsPerJob          int
	MaxCandidatePreviewBytes int64
	MaxCandidatePreviewLines int
}

// ValidationWorkspace contains operational ceilings used both when copying a
// candidate into confinement and when fingerprinting it before/after checks.
// Keeping them in application configuration makes one visible policy govern
// both sides of that boundary.
type ValidationWorkspace struct {
	MaxFiles                    int64
	MaxDirectories              int64
	MaxPathBytes                int64
	MaxFileBytes                int64
	MaxTotalBytes               int64
	ContainerPIDs               int
	ContainerMemoryBytes        int64
	ContainerCPUMillis          int64
	ContainerTemporaryBytes     int64
	ContainerWorkspaceBytes     int64
	ContainerNofileLimit        int64
	ContainerGeneratedFileBytes int64
	ContainerStopTimeout        time.Duration
	ContainerControlTimeout     time.Duration
	ContainerSentinelTimeout    time.Duration
	ContainerCrashProbeTimeout  time.Duration
}

type Delivery struct {
	DefaultMode              fix.DeliveryMode
	Remote                   string
	BaseBranch               string
	BranchTemplate           string
	Publisher                string
	DraftPullRequests        bool
	RequireValidation        bool
	CommandOutputBytes       int64
	CommitPolicy             string
	CommitTitleTemplate      string
	CommitBodyTemplate       string
	PullRequestTitleTemplate string
	PullRequestBodyTemplate  string
	CleanupPolicy            string
}

type Resolved struct {
	SchemaVersion       int
	Revision            Revision
	Origins             map[string]Origin
	Fix                 FixDefaults
	Concurrency         Concurrency
	Profiles            []agent.Profile
	Validation          []validation.Plan
	ValidationWorkspace ValidationWorkspace
	Delivery            Delivery
	TrendWindow         time.Duration
}

type Patch struct {
	Fix                 *FixDefaults
	Concurrency         *Concurrency
	Profiles            *[]agent.Profile
	Validation          *[]validation.Plan
	ValidationWorkspace *ValidationWorkspace
	Delivery            *Delivery
	TrendWindow         *time.Duration
}

type Saved struct {
	Revision Revision
	Resolved Resolved
}

type Editable struct {
	Resolved    Resolved
	Diagnostics []string
}
type Editor interface {
	LoadEditable(context.Context, fix.WorkspaceIdentity) (Editable, error)
}

type Resolver interface {
	Resolve(context.Context, fix.WorkspaceIdentity, SessionOverrides) (Resolved, error)
}

type Store interface {
	Save(context.Context, fix.WorkspaceIdentity, Scope, Patch, Revision) (Saved, error)
}
